package anim

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sort"
)

// snapshotData 序列化载荷。map 键在编码前排序，保证字节级可复现。
type snapshotData struct {
	Version   string
	State     string
	StateTime float64
	HasTrans  bool
	TransFrom string
	TransElap float64
	TransTot  float64
	FloatKeys []string
	FloatVals []float64
	BoolKeys  []string
	BoolVals  []bool
	IntKeys   []string
	IntVals   []int64
}

// Snapshot 实体可持久化的状态快照。
type Snapshot struct {
	EntityID string
	Data     []byte
}

// Save 导出实体当前状态。相同内部状态必产生相同字节序列。
func (e *Entity) Save() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	g := e.rt.graph.Load()
	d := snapshotData{
		Version:   g.version,
		State:     e.state,
		StateTime: e.stateTime,
	}
	if e.trans != nil {
		d.HasTrans = true
		d.TransFrom = e.trans.from
		d.TransElap = e.trans.elapsed
		d.TransTot = e.trans.total
	}
	if e.params != nil {
		d.FloatKeys = sortedKeysF(e.params.floats)
		for _, k := range d.FloatKeys {
			d.FloatVals = append(d.FloatVals, e.params.floats[k])
		}
		d.BoolKeys = sortedKeysB(e.params.bools)
		for _, k := range d.BoolKeys {
			d.BoolVals = append(d.BoolVals, e.params.bools[k])
		}
		d.IntKeys = sortedKeysI(e.params.ints)
		for _, k := range d.IntKeys {
			d.IntVals = append(d.IntVals, e.params.ints[k])
		}
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(d); err != nil {
		panic(err) // 仅当内存耗尽等极端情况
	}
	return Snapshot{EntityID: e.id, Data: buf.Bytes()}
}

// Restore 从快照恢复实体。
// 图版本必须与快照一致，且快照中的状态仍然存在，否则返回错误，
// 避免恢复到不可复现的位置。
func (r *Runtime) Restore(s Snapshot) (*Entity, error) {
	var d snapshotData
	if err := gob.NewDecoder(bytes.NewReader(s.Data)).Decode(&d); err != nil {
		return nil, fmt.Errorf("快照解码失败: %w", err)
	}
	g := r.graph.Load()
	if d.Version != g.version {
		return nil, fmt.Errorf("快照图版本 %q 与当前版本 %q 不一致", d.Version, g.version)
	}
	if _, ok := g.states[d.State]; !ok {
		return nil, fmt.Errorf("快照状态 %q 在当前图中不存在", d.State)
	}
	e := &Entity{
		id: s.EntityID, rt: r,
		params: NewParams(), state: d.State, stateTime: d.StateTime,
	}
	for i, k := range d.FloatKeys {
		e.params.SetFloat(k, d.FloatVals[i])
	}
	for i, k := range d.BoolKeys {
		e.params.SetBool(k, d.BoolVals[i])
	}
	for i, k := range d.IntKeys {
		e.params.SetInt(k, d.IntVals[i])
	}
	if d.HasTrans {
		if _, ok := g.states[d.TransFrom]; !ok {
			return nil, fmt.Errorf("快照过渡源状态 %q 在当前图中不存在", d.TransFrom)
		}
		e.trans = &transitionState{from: d.TransFrom, elapsed: d.TransElap, total: d.TransTot}
	}
	return e, nil
}

func sortedKeysF(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysB(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysI(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
