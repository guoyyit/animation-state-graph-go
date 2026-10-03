package anim

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
)

// testDef 构造一个基础测试图：
//
//	参数: speed(Float [0,10]), run(Bool)
//	混合: locomotion = idle*const(1-speed/10) 不适用 → 用参数驱动:
//	      walk 权重=speed, run 权重=10-speed（通过两个 blend 片段参数绑定）
//	状态: idle -> moving (speed>1, 过渡0.5s), moving -> idle (speed<=1)
func testDef() GraphDef {
	return GraphDef{
		Version: "v1",
		Params: []ParamDef{
			{Name: "speed", Type: ParamFloat, Min: 0, Max: 10},
			{Name: "wrun", Type: ParamFloat, Min: 0, Max: 10},
		},
		Blends: []BlendNodeDef{
			{ID: "idleBlend", Clips: []ClipRef{{ClipID: "idle", ConstWeight: 1}}},
			{ID: "moveBlend", Clips: []ClipRef{
				{ClipID: "walk", Param: "speed"},
				{ClipID: "run", Param: "wrun"},
			}},
		},
		States: []StateDef{
			{ID: "idle", BlendID: "idleBlend", Transitions: []TransitionDef{
				{To: "moving", Condition: ExprDef{Param: "speed", Op: ">", Value: 1}, Priority: 0, Duration: 0.5},
			}},
			{ID: "moving", BlendID: "moveBlend", Transitions: []TransitionDef{
				{To: "idle", Condition: ExprDef{Param: "speed", Op: "<=", Value: 1}, Priority: 0, Duration: 0.25},
			}},
		},
		Entry: "idle",
	}
}

func sumWeights(samples []ClipSample) float64 {
	s := 0.0
	for _, c := range samples {
		s += c.Weight
	}
	return s
}

// 1. 混合权重归一化与过渡平滑性
func TestBlendNormalizationAndSmoothTransition(t *testing.T) {
	rt, err := NewRuntime(testDef())
	if err != nil {
		t.Fatal(err)
	}
	e := rt.Spawn("e1")
	e.SetParams(NewParams().SetFloat("speed", 4).SetFloat("wrun", 6))

	// 第 1 帧：speed>1 触发 idle->moving，进入 0.5s 过渡
	prev := map[string]float64{}
	for frame := 0; frame < 12; frame++ {
		res := e.Advance(0.1)
		if got := sumWeights(res.Samples); math.Abs(got-1) > 1e-9 {
			t.Fatalf("frame %d: 权重和=%v, 期望 1", frame, got)
		}
		cur := map[string]float64{}
		for _, s := range res.Samples {
			cur[s.ClipID] = s.Weight
		}
		// 过渡中任意片段权重变化必须连续（单帧变化不超过 dt/duration + 余量）
		if frame > 0 {
			for id, w := range cur {
				if d := math.Abs(w - prev[id]); d > 0.21 {
					t.Fatalf("frame %d: 片段 %s 权重跳变 %v", frame, id, d)
				}
			}
		}
		prev = cur
	}
	// 过渡结束后应完全处于 moving：walk=0.4, run=0.6
	res := e.Advance(0.1)
	w := map[string]float64{}
	for _, s := range res.Samples {
		w[s.ClipID] = s.Weight
	}
	if math.Abs(w["walk"]-0.4) > 1e-9 || math.Abs(w["run"]-0.6) > 1e-9 {
		t.Fatalf("归一化权重错误: %v", w)
	}
	if res.State != "moving" {
		t.Fatalf("状态应为 moving, 实际 %s", res.State)
	}
}

// 2. 同帧多转移满足时的确定性裁决：Priority 小者胜，同级按定义顺序
func TestTransitionArbitrationDeterministic(t *testing.T) {
	def := GraphDef{
		Version: "v1",
		Params:  []ParamDef{{Name: "x", Type: ParamFloat}},
		Blends: []BlendNodeDef{
			{ID: "bA", Clips: []ClipRef{{ClipID: "a", ConstWeight: 1}}},
			{ID: "bB", Clips: []ClipRef{{ClipID: "b", ConstWeight: 1}}},
			{ID: "bC", Clips: []ClipRef{{ClipID: "c", ConstWeight: 1}}},
		},
		States: []StateDef{
			{ID: "s", BlendID: "bA", Transitions: []TransitionDef{
				{To: "t2", Condition: ExprDef{Param: "x", Op: ">", Value: 0}, Priority: 5},
				{To: "t1", Condition: ExprDef{Param: "x", Op: ">", Value: 0}, Priority: 1},
			}},
			{ID: "t1", BlendID: "bB"},
			{ID: "t2", BlendID: "bC"},
		},
		Entry: "s",
	}
	rt, err := NewRuntime(def)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		e := rt.Spawn(fmt.Sprintf("e%d", i))
		e.SetParams(NewParams().SetFloat("x", 1))
		res := e.Advance(0.016)
		if res.State != "t1" {
			t.Fatalf("run %d: 期望裁决到 t1, 实际 %s", i, res.State)
		}
	}
}

// 2b. 自我转移不死循环：每帧最多一次转移
func TestSelfTransitionNoLoop(t *testing.T) {
	def := GraphDef{
		Version: "v1",
		Params:  []ParamDef{{Name: "x", Type: ParamFloat}},
		Blends:  []BlendNodeDef{{ID: "b", Clips: []ClipRef{{ClipID: "c", ConstWeight: 1}}}},
		States: []StateDef{
			{ID: "s", BlendID: "b", Transitions: []TransitionDef{
				{To: "s", Condition: ExprDef{Param: "x", Op: ">", Value: 0}},
			}},
		},
		Entry: "s",
	}
	rt, err := NewRuntime(def)
	if err != nil {
		t.Fatal(err)
	}
	e := rt.Spawn("e")
	e.SetParams(NewParams().SetFloat("x", 1))
	for i := 0; i < 100; i++ { // 若死循环此处会挂起
		res := e.Advance(0.016)
		if res.State != "s" {
			t.Fatalf("自我转移后应仍在 s, 实际 %s", res.State)
		}
	}
}

// 3. 参数异常：缺失/类型不匹配/越界产生可定位诊断，且按策略降级
func TestParamAnomalies(t *testing.T) {
	rt, err := NewRuntime(testDef())
	if err != nil {
		t.Fatal(err)
	}
	e := rt.Spawn("e1")

	// 缺失 speed：转移条件不成立（不转移），并产生诊断
	e.SetParams(NewParams())
	res := e.Advance(0.016)
	if res.State != "idle" {
		t.Fatalf("参数缺失时不应转移, 实际 %s", res.State)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "param.missing" && d.Param == "speed" && d.Binding != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 param.missing 诊断: %v", res.Diagnostics)
	}

	// 类型不匹配：speed 传成 Bool
	e.SetParams(NewParams().SetBool("speed", true))
	res = e.Advance(0.016)
	found = false
	for _, d := range res.Diagnostics {
		if d.Code == "param.type" && d.Param == "speed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 param.type 诊断: %v", res.Diagnostics)
	}

	// 越界：speed=99 超出 [0,10]，应钳制并告警
	e.SetParams(NewParams().SetFloat("speed", 99).SetFloat("wrun", 1))
	res = e.Advance(0.016)
	found = false
	for _, d := range res.Diagnostics {
		if d.Code == "param.range" && d.Param == "speed" && d.Severity == SevWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 param.range 诊断: %v", res.Diagnostics)
	}
	if res.State != "moving" {
		t.Fatalf("钳制后 speed=10>1 应转移到 moving, 实际 %s", res.State)
	}
}

// 4. 并发推进：多实体互不串扰，不同并发度下结果一致
func TestConcurrentAdvanceDeterministic(t *testing.T) {
	const nEntities, nFrames = 64, 50
	run := func(concurrency int) [][]FrameResult {
		rt, err := NewRuntime(testDef())
		if err != nil {
			t.Fatal(err)
		}
		entities := make([]*Entity, nEntities)
		rng := rand.New(rand.NewSource(42)) // 固定种子
		params := make([][]*Params, nEntities)
		for i := range entities {
			entities[i] = rt.Spawn(fmt.Sprintf("e%d", i))
			params[i] = make([]*Params, nFrames)
			for f := 0; f < nFrames; f++ {
				params[i][f] = NewParams().
					SetFloat("speed", rng.Float64()*10).
					SetFloat("wrun", rng.Float64()*10)
			}
		}
		out := make([][]FrameResult, nEntities)
		for i := range out {
			out[i] = make([]FrameResult, nFrames)
		}
		var wg sync.WaitGroup
		sem := make(chan struct{}, concurrency)
		for i := 0; i < nEntities; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				for f := 0; f < nFrames; f++ {
					entities[i].SetParams(params[i][f])
					out[i][f] = entities[i].Advance(0.016)
				}
			}(i)
		}
		wg.Wait()
		return out
	}
	base := run(1)
	for _, c := range []int{4, 16, 64} {
		got := run(c)
		for i := range base {
			for f := range base[i] {
				a, b := base[i][f], got[i][f]
				if a.State != b.State || len(a.Samples) != len(b.Samples) {
					t.Fatalf("并发度 %d: 实体 %d 帧 %d 输出不一致", c, i, f)
				}
				for k := range a.Samples {
					if a.Samples[k] != b.Samples[k] {
						t.Fatalf("并发度 %d: 实体 %d 帧 %d 片段 %d 不一致: %v vs %v",
							c, i, f, k, a.Samples[k], b.Samples[k])
					}
				}
			}
		}
	}
}

// 5. 热更新：失败保留旧定义；成功后新定义生效
func TestHotUpdate(t *testing.T) {
	rt, err := NewRuntime(testDef())
	if err != nil {
		t.Fatal(err)
	}
	e := rt.Spawn("e1")
	e.SetParams(NewParams().SetFloat("speed", 0.5))
	e.Advance(0.016)

	// 非法定义：引用不存在的混合节点 → 更新失败，旧定义继续工作
	bad := testDef()
	bad.Version = "v2-bad"
	bad.States[0].BlendID = "nonexistent"
	if err := rt.Update(bad); err == nil {
		t.Fatal("非法定义应更新失败")
	}
	if rt.Graph().Version() != "v1" {
		t.Fatalf("更新失败后应保留 v1, 实际 %s", rt.Graph().Version())
	}
	res := e.Advance(0.016)
	if res.State != "idle" || len(res.Samples) == 0 {
		t.Fatalf("旧定义应继续可用: %+v", res)
	}

	// 合法定义：更新成功
	good := testDef()
	good.Version = "v2"
	if err := rt.Update(good); err != nil {
		t.Fatal(err)
	}
	if rt.Graph().Version() != "v2" {
		t.Fatalf("应切换到 v2, 实际 %s", rt.Graph().Version())
	}

	// 新定义删除了实体当前状态 → 重置到入口并给出诊断
	good2 := testDef()
	good2.Version = "v3"
	good2.States = good2.States[:1] // 只剩 idle
	good2.States[0].Transitions = nil
	e.SetParams(NewParams().SetFloat("speed", 5).SetFloat("wrun", 5))
	e.Advance(0.016) // 进入 moving
	if e.State() != "moving" {
		t.Fatalf("前置条件失败: 期望 moving, 实际 %s", e.State())
	}
	if err := rt.Update(good2); err != nil {
		t.Fatal(err)
	}
	res = e.Advance(0.016)
	if res.State != "idle" {
		t.Fatalf("状态被删除后应重置到入口 idle, 实际 %s", res.State)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "state.missing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有 state.missing 诊断: %v", res.Diagnostics)
	}
}

// 6. 重启恢复：快照恢复后继续推进，输出与未重启逐条一致
func TestSaveRestoreReproducible(t *testing.T) {
	def := testDef()
	seq := []*Params{
		NewParams().SetFloat("speed", 0.5).SetFloat("wrun", 0),
		NewParams().SetFloat("speed", 3).SetFloat("wrun", 7),
		NewParams().SetFloat("speed", 4).SetFloat("wrun", 6),
		NewParams().SetFloat("speed", 5).SetFloat("wrun", 5),
		NewParams().SetFloat("speed", 6).SetFloat("wrun", 4),
	}
	// 基准：连续推进 5 帧
	rt1, _ := NewRuntime(def)
	e1 := rt1.Spawn("e")
	var baseline []FrameResult
	for _, p := range seq {
		e1.SetParams(p)
		baseline = append(baseline, e1.Advance(0.1))
	}
	// 模拟重启：推进 2 帧后保存，新建运行时恢复，再推进 3 帧
	rt2, _ := NewRuntime(def)
	e2 := rt2.Spawn("e")
	for _, p := range seq[:2] {
		e2.SetParams(p)
		e2.Advance(0.1)
	}
	snap := e2.Save()
	rt3, _ := NewRuntime(def)
	e3, err := rt3.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	// 快照已包含前 2 帧，恢复后直接从第 3 帧继续
	var restored []FrameResult
	for _, p := range seq[2:] {
		e3.SetParams(p)
		restored = append(restored, e3.Advance(0.1))
	}
	for j := range restored {
		a, b := baseline[2+j], restored[j]
		if a.State != b.State || len(a.Samples) != len(b.Samples) {
			t.Fatalf("帧 %d 恢复后输出不一致", 2+j)
		}
		for k := range a.Samples {
			if a.Samples[k] != b.Samples[k] {
				t.Fatalf("帧 %d 片段 %d 不一致: %v vs %v", 2+j, k, a.Samples[k], b.Samples[k])
			}
		}
	}

	// 版本不一致时拒绝恢复
	rt4, _ := NewRuntime(func() GraphDef { d := def; d.Version = "v9"; return d }())
	if _, err := rt4.Restore(snap); err == nil {
		t.Fatal("版本不一致应拒绝恢复")
	}
}

// 7. 快照字节级可复现
func TestSnapshotDeterministicBytes(t *testing.T) {
	rt, _ := NewRuntime(testDef())
	e := rt.Spawn("e")
	e.SetParams(NewParams().SetFloat("speed", 3).SetFloat("wrun", 7))
	e.Advance(0.1)
	s1 := e.Save()
	s2 := e.Save()
	if string(s1.Data) != string(s2.Data) {
		t.Fatal("相同状态应产生相同快照字节")
	}
}
