package runtime

import (
	"testing"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

func ptr(s string) *string { return &s }

// testGraph builds a two-state graph used across tests.
//
//	idle: idle clip (weight0) + run clip (weight1)
//	run:  run clip (weight0) + air clip (weight1)
//
// Transitions idle<->run are driven by "speed" with a 0.5s crossfade.
func testGraph() *graph.GraphDef {
	return &graph.GraphDef{
		Name: "test",
		Params: []graph.ParamDef{
			{Name: "speed", Type: graph.ParamFloat, Default: 0, HasMin: true, HasMax: true, Min: 0, Max: 10},
			{Name: "grounded", Type: graph.ParamBool, Default: 1},
			{Name: "weight0", Type: graph.ParamFloat, Default: 1, HasMin: true, HasMax: true, Min: 0, Max: 1},
			{Name: "weight1", Type: graph.ParamFloat, Default: 0, HasMin: true, HasMax: true, Min: 0, Max: 1},
		},
		Clips: []graph.ClipDef{
			{ID: "idle", Channels: []float64{1, 0, 0}},
			{ID: "run", Channels: []float64{0, 1, 0}},
			{ID: "air", Channels: []float64{0, 0, 1}},
		},
		States: []graph.StateDef{
			{
				ID: "idle",
				Root: graph.BlendNodeDef{
					ID: "idle_blend",
					Children: []graph.BlendChildDef{
						{Clip: ptr("idle"), Weight: graph.BindingDef{Param: "weight0"}},
						{Clip: ptr("run"), Weight: graph.BindingDef{Param: "weight1"}},
					},
				},
			},
			{
				ID: "run",
				Root: graph.BlendNodeDef{
					ID: "run_blend",
					Children: []graph.BlendChildDef{
						{Clip: ptr("run"), Weight: graph.BindingDef{Param: "weight0"}},
						{Clip: ptr("air"), Weight: graph.BindingDef{Param: "weight1"}},
					},
				},
			},
		},
		Transitions: []graph.TransitionDef{
			{ID: "to_run", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0.5, Priority: 0},
			{ID: "to_idle", From: "run", To: "idle", Condition: "speed <= 1.0", Duration: 0.5, Priority: 0},
		},
		InitialState: "idle",
	}
}

// noDiags fails the test if the result carries any diagnostic.
func noDiags(t *testing.T, r AdvanceResult) {
	t.Helper()
	if len(r.Diagnostics) != 0 {
		t.Fatalf("entity %s frame %d: unexpected diagnostics: %v", r.EntityID, r.Frame, r.Diagnostics)
	}
}

func diagKinds(diags []Diagnostic) map[DiagKind]int {
	m := make(map[DiagKind]int)
	for _, d := range diags {
		m[d.Kind]++
	}
	return m
}
