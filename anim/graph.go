package anim

import (
	"fmt"
	"sort"
)

// compiledTransition 预编译的转移。
type compiledTransition struct {
	to       string
	toIndex  int
	cond     *compiledExpr // nil 表示无条件
	priority int
	duration float64
	defOrder int // 定义顺序，用于同优先级的确定性裁决
}

// compiledState 预编译的状态。
type compiledState struct {
	id    string
	blend *compiledBlend
	trans []compiledTransition // 已按 (priority, defOrder) 排序
}

// Graph 编译后的不可变状态图，可安全并发读取。
// 所有查找均通过预建索引 O(1) 完成，单帧推进只访问当前状态，
// 复杂度为 O(当前状态出边数 + 当前混合节点片段数)，与图规模无关。
type Graph struct {
	version string
	params  map[string]ParamDef
	blends  map[string]*compiledBlend
	states  map[string]*compiledState
	order   []string // 状态 ID 的稳定顺序（定义顺序）
	entry   string
}

// Version 返回图定义版本。
func (g *Graph) Version() string { return g.version }

// Compile 校验并编译图定义；失败时返回错误，不产生部分可用的图。
func Compile(def GraphDef) (*Graph, error) {
	g := &Graph{
		version: def.Version,
		params:  map[string]ParamDef{},
		blends:  map[string]*compiledBlend{},
		states:  map[string]*compiledState{},
	}
	// 参数表
	for _, pd := range def.Params {
		if pd.Name == "" {
			return nil, fmt.Errorf("参数名不能为空")
		}
		if _, dup := g.params[pd.Name]; dup {
			return nil, fmt.Errorf("参数 %q 重复定义", pd.Name)
		}
		if pd.Type != ParamFloat && pd.Type != ParamBool && pd.Type != ParamInt {
			return nil, fmt.Errorf("参数 %q 类型非法", pd.Name)
		}
		g.params[pd.Name] = pd
	}
	// 混合节点
	for _, bd := range def.Blends {
		if _, dup := g.blends[bd.ID]; dup {
			return nil, fmt.Errorf("混合节点 %q 重复定义", bd.ID)
		}
		b, err := compileBlend(bd, g.params)
		if err != nil {
			return nil, err
		}
		g.blends[bd.ID] = b
	}
	// 状态
	for _, sd := range def.States {
		if sd.ID == "" {
			return nil, fmt.Errorf("状态 ID 不能为空")
		}
		if _, dup := g.states[sd.ID]; dup {
			return nil, fmt.Errorf("状态 %q 重复定义", sd.ID)
		}
		blend, ok := g.blends[sd.BlendID]
		if !ok {
			return nil, fmt.Errorf("状态 %q 引用了未定义的混合节点 %q", sd.ID, sd.BlendID)
		}
		cs := &compiledState{id: sd.ID, blend: blend}
		for i, td := range sd.Transitions {
			if _, ok := g.states[td.To]; !ok {
				// 目标状态可能尚未编译，先记录，二次校验
			}
			if td.Duration < 0 {
				return nil, fmt.Errorf("状态 %q 的转移时长不能为负", sd.ID)
			}
			ct := compiledTransition{
				to: td.To, priority: td.Priority,
				duration: td.Duration, defOrder: i,
			}
			if td.Condition.Param != "" {
				ce, err := compileExpr(td.Condition, g.params)
				if err != nil {
					return nil, fmt.Errorf("状态 %q 的转移条件无效: %w", sd.ID, err)
				}
				ct.cond = ce
			}
			cs.trans = append(cs.trans, ct)
		}
		g.states[sd.ID] = cs
		g.order = append(g.order, sd.ID)
	}
	// 二次校验：转移目标存在
	for _, cs := range g.states {
		for _, ct := range cs.trans {
			if _, ok := g.states[ct.to]; !ok {
				return nil, fmt.Errorf("状态 %q 的转移目标 %q 不存在", cs.id, ct.to)
			}
		}
	}
	// 入口
	if _, ok := g.states[def.Entry]; !ok {
		return nil, fmt.Errorf("入口状态 %q 不存在", def.Entry)
	}
	g.entry = def.Entry
	// 裁决规则：同帧多个转移满足时，按 (Priority 升序, 定义顺序升序) 取第一个；
	// 该排序在编译期完成，推进时顺序扫描首个满足者即可，结果确定。
	for _, cs := range g.states {
		sort.SliceStable(cs.trans, func(i, j int) bool {
			if cs.trans[i].priority != cs.trans[j].priority {
				return cs.trans[i].priority < cs.trans[j].priority
			}
			return cs.trans[i].defOrder < cs.trans[j].defOrder
		})
	}
	// 解析转移目标索引（用于快照兼容校验）
	for _, cs := range g.states {
		for i := range cs.trans {
			cs.trans[i].toIndex = indexOf(g.order, cs.trans[i].to)
		}
	}
	return g, nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
