package anim

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// FrameResult 单帧推进结果。
type FrameResult struct {
	EntityID    string
	State       string       // 本帧结束时的当前状态
	Samples     []ClipSample // 归一化混合输出，权重和为 1
	Diagnostics []Diagnostic // 本帧产生的全部诊断，可定位到具体绑定
}

// Runtime 持有当前生效的图定义，支持热更新与多实体并发推进。
// 热更新通过原子指针整体替换：推进中的实体在帧开始时读取一次指针，
// 本帧内始终看到同一个完整定义，不会读到半更新状态。
type Runtime struct {
	graph atomic.Pointer[Graph]
}

// NewRuntime 以初始图定义创建运行时；def 校验失败时返回错误。
func NewRuntime(def GraphDef) (*Runtime, error) {
	g, err := Compile(def)
	if err != nil {
		return nil, err
	}
	r := &Runtime{}
	r.graph.Store(g)
	return r, nil
}

// Update 热更新图定义。先完整编译校验，成功后原子替换；
// 校验失败时保留上一个可用定义继续运行，并返回错误。
func (r *Runtime) Update(def GraphDef) error {
	g, err := Compile(def)
	if err != nil {
		return err
	}
	r.graph.Store(g)
	return nil
}

// Graph 返回当前生效的图定义（快照）。
func (r *Runtime) Graph() *Graph { return r.graph.Load() }

// transitionState 进行中的交叉淡入。
type transitionState struct {
	from    string
	elapsed float64
	total   float64
}

// Entity 一个推进中的实体。参数与内部状态完全私有，
// 不同实体之间互不串扰；同一实体的 Advance 由内部互斥锁串行化。
type Entity struct {
	id string
	rt *Runtime

	mu     sync.Mutex
	params *Params

	state     string
	stateTime float64
	trans     *transitionState // 非 nil 表示正在过渡
}

// Spawn 在运行时中创建实体，从入口状态开始。
func (r *Runtime) Spawn(id string) *Entity {
	g := r.graph.Load()
	return &Entity{id: id, rt: r, params: NewParams(), state: g.entry}
}

// ID 返回实体标识。
func (e *Entity) ID() string { return e.id }

// SetParams 整体替换实体参数（拷贝快照，调用方之后修改不影响实体）。
func (e *Entity) SetParams(p *Params) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.params = p.clone()
}

func (p *Params) clone() *Params {
	if p == nil {
		return NewParams()
	}
	q := &Params{}
	if len(p.floats) > 0 {
		q.floats = make(map[string]float64, len(p.floats))
		for k, v := range p.floats {
			q.floats[k] = v
		}
	}
	if len(p.bools) > 0 {
		q.bools = make(map[string]bool, len(p.bools))
		for k, v := range p.bools {
			q.bools[k] = v
		}
	}
	if len(p.ints) > 0 {
		q.ints = make(map[string]int64, len(p.ints))
		for k, v := range p.ints {
			q.ints[k] = v
		}
	}
	return q
}

// State 返回实体当前状态 ID。
func (e *Entity) State() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state
}

// Advance 以实体当前参数推进 dt 秒。
//
// 单帧语义（确定性保证）：
//  1. 帧开始时读取一次图指针，本帧使用同一图快照。
//  2. 只评估当前状态的出边（O(出边数)），按编译期排定的
//     (Priority, 定义顺序) 取第一个条件满足者，裁决结果确定。
//  3. 每帧最多发生一次转移：自我转移只重置状态计时，
//     环状转移在后续帧逐跳推进，均不会死循环或同帧重复进入。
//  4. 过渡期间输出为源/目标两个混合节点按进度的加权插值，
//     两侧各自归一化后再插值，总权重恒为 1，无跳变。
func (e *Entity) Advance(dt float64) FrameResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := e.rt.graph.Load()
	var diags []Diagnostic

	st, ok := g.states[e.state]
	if !ok {
		// 热更新后当前状态已不存在：重置到入口状态（文档化降级策略）。
		diags = append(diags, Diagnostic{
			Severity: SevWarn, Code: "state.missing", Binding: "entity/" + e.id,
			Param: "", Message: fmt.Sprintf("状态 %q 不在图版本 %q 中，重置到入口 %q", e.state, g.version, g.entry),
		})
		e.state = g.entry
		e.stateTime = 0
		e.trans = nil
		st = g.states[e.state]
	}

	// 1. 转移判定（每帧最多一次）
	if e.trans == nil {
		for i := range st.trans {
			ct := &st.trans[i]
			if ct.cond == nil || ct.cond.eval(e.params, fmt.Sprintf("state/%s/trans[%d]", st.id, ct.defOrder), &diags) {
				e.beginTransition(g, ct)
				break
			}
		}
	}

	// 2. 推进计时
	e.stateTime += dt
	var samples []ClipSample
	if e.trans != nil {
		e.trans.elapsed += dt
		alpha := 1.0
		if e.trans.total > 0 {
			alpha = e.trans.elapsed / e.trans.total
			if alpha > 1 {
				alpha = 1
			}
		}
		from := g.states[e.trans.from]
		to := g.states[e.state]
		samples = crossfade(from.blend.eval(e.params, &diags), to.blend.eval(e.params, &diags), alpha)
		if e.trans.elapsed >= e.trans.total {
			e.trans = nil
		}
	} else {
		samples = st.blend.eval(e.params, &diags)
	}

	return FrameResult{EntityID: e.id, State: e.state, Samples: samples, Diagnostics: diags}
}

// beginTransition 开始一次转移。自我转移与瞬时转移（Duration<=0）立即完成。
func (e *Entity) beginTransition(g *Graph, ct *compiledTransition) {
	from := e.state
	e.state = ct.to
	e.stateTime = 0
	if ct.to == from || ct.duration <= 0 {
		e.trans = nil // 自我转移/瞬时转移：不进入过渡态，避免重复进入
		return
	}
	e.trans = &transitionState{from: from, total: ct.duration}
}

// crossfade 合并两个归一化混合输出：w = (1-alpha)*from + alpha*to。
// 相同 ClipID 合并；输出顺序为首次出现顺序（确定性）。
func crossfade(from, to []ClipSample, alpha float64) []ClipSample {
	type acc struct {
		idx int
		w   float64
	}
	byID := make(map[string]acc, len(from)+len(to))
	var order []string
	add := func(s ClipSample, scale float64) {
		if a, ok := byID[s.ClipID]; ok {
			a.w += s.Weight * scale
			byID[s.ClipID] = a
		} else {
			byID[s.ClipID] = acc{idx: len(order), w: s.Weight * scale}
			order = append(order, s.ClipID)
		}
	}
	for _, s := range from {
		add(s, 1-alpha)
	}
	for _, s := range to {
		add(s, alpha)
	}
	out := make([]ClipSample, 0, len(order))
	for _, id := range order {
		w := byID[id].w
		if w > 0 {
			out = append(out, ClipSample{ClipID: id, Weight: w})
		}
	}
	if len(out) == 0 {
		return out
	}
	// 归一化，消除浮点误差导致的总和偏移
	sum := 0.0
	for _, s := range out {
		sum += s.Weight
	}
	if sum > 0 && sum != 1 {
		for i := range out {
			out[i].Weight /= sum
		}
	}
	return out
}

// AdvanceAll 并发推进一批实体，每个实体一个 goroutine。
// 单个实体的 panic 被隔离为诊断，不影响其他实体；
// 慢实体只占用自己的 goroutine，不阻塞他人。
// 返回结果的顺序与输入实体顺序一致（确定性，与并发度无关）。
func (r *Runtime) AdvanceAll(entities []*Entity, dt float64) []FrameResult {
	results := make([]FrameResult, len(entities))
	var wg sync.WaitGroup
	for i, e := range entities {
		wg.Add(1)
		go func(i int, e *Entity) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					results[i] = FrameResult{
						EntityID: e.id,
						Diagnostics: []Diagnostic{{
							Severity: SevError, Code: "entity.panic",
							Binding: "entity/" + e.id,
							Message: fmt.Sprintf("推进 panic 已隔离: %v", rec),
						}},
					}
				}
			}()
			results[i] = e.Advance(dt)
		}(i, e)
	}
	wg.Wait()
	return results
}
