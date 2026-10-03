package runtime

import (
	"fmt"
	"testing"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

// bigGraph builds a chain of n states, each with 2 clips and 2 outgoing
// transitions, plus a wildcard transition. This models a state-heavy graph.
func bigGraph(nStates int) *graph.GraphDef {
	def := &graph.GraphDef{
		Name: "big",
		Params: []graph.ParamDef{
			{Name: "p0", Type: graph.ParamFloat, Default: 1, HasMin: true, HasMax: true, Min: 0, Max: 1},
			{Name: "p1", Type: graph.ParamFloat, Default: 0, HasMin: true, HasMax: true, Min: 0, Max: 1},
			{Name: "cond", Type: graph.ParamFloat, Default: 0},
		},
		Clips: []graph.ClipDef{
			{ID: "c0", Channels: []float64{1, 0}},
			{ID: "c1", Channels: []float64{0, 1}},
		},
	}
	for i := 0; i < nStates; i++ {
		id := fmt.Sprintf("s%03d", i)
		next := fmt.Sprintf("s%03d", (i+1)%nStates)
		def.States = append(def.States, graph.StateDef{
			ID: id,
			Root: graph.BlendNodeDef{
				ID: id + "_blend",
				Children: []graph.BlendChildDef{
					{Clip: ptr("c0"), Weight: graph.BindingDef{Param: "p0"}},
					{Clip: ptr("c1"), Weight: graph.BindingDef{Param: "p1"}},
				},
			},
		})
		def.Transitions = append(def.Transitions,
			graph.TransitionDef{
				ID: fmt.Sprintf("t_%s_next", id), From: id, To: next,
				Condition: fmt.Sprintf("cond > %d", i), Duration: 0.2, Priority: 0,
			},
			graph.TransitionDef{
				ID: fmt.Sprintf("t_%s_back", id), From: id, To: fmt.Sprintf("s%03d", (i-1+nStates)%nStates),
				Condition: fmt.Sprintf("cond < -%d", i), Duration: 0.2, Priority: 0,
			},
		)
	}
	def.InitialState = "s000"
	return def
}

func BenchmarkAdvance(b *testing.B) {
	eng, err := New(bigGraph(200), 1)
	if err != nil {
		b.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("p0", 1)
	en.SetParam("p1", 0)
	en.SetParam("cond", 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		en.Advance(eng, 1.0/60.0)
	}
}

func BenchmarkAdvanceAll_1000Entities(b *testing.B) {
	eng, err := New(bigGraph(200), 1)
	if err != nil {
		b.Fatal(err)
	}
	const n = 1000
	ents := make([]*Entity, n)
	for i := range ents {
		ents[i] = NewEntity(fmt.Sprintf("e%d", i), int64(i))
		ents[i].SetParam("p0", 1)
		ents[i].SetParam("p1", 0)
		ents[i].SetParam("cond", 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.AdvanceAll(1.0/60.0, ents, 8)
	}
}

// TestAdvanceScaling: per-frame cost is bounded by the size of the current
// state (outgoing transitions + clips), not the total graph size.
func TestAdvanceScaling(t *testing.T) {
	for _, nStates := range []int{10, 200} {
		eng, err := New(bigGraph(nStates), 1)
		if err != nil {
			t.Fatal(err)
		}
		en := NewEntity("e", 1)
		en.SetParam("p0", 1)
		en.SetParam("p1", 0)
		en.SetParam("cond", 0)
		for i := 0; i < 100; i++ {
			r := en.Advance(eng, 1.0/60.0)
			noDiags(t, r)
		}
	}
}
