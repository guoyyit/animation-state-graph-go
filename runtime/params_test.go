package runtime

import (
	"strings"
	"testing"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

// TestMissingParam: a weight binding whose parameter was never provided
// falls back to the default and emits a locatable diagnostic.
func TestMissingParam(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	// weight0 intentionally not set; weight1 set.
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	kinds := diagKinds(r.Diagnostics)
	if kinds[DiagMissingParam] == 0 {
		t.Fatalf("expected missing_param diagnostic, got %v", r.Diagnostics)
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Kind == DiagMissingParam && d.Param == "weight0" {
			found = true
			if !strings.Contains(d.Binding, "root.children[0].weight") {
				t.Fatalf("diagnostic binding %q does not locate the binding", d.Binding)
			}
			if d.Degraded != 1 {
				t.Fatalf("degraded = %g, want default 1", d.Degraded)
			}
		}
	}
	if !found {
		t.Fatalf("no missing_param diagnostic for weight0: %v", r.Diagnostics)
	}
	// Default weight0=1, weight1=0 -> idle pose.
	if !floatsClose(r.Pose, []float64{1, 0, 0}) {
		t.Fatalf("pose = %v, want idle pose", r.Pose)
	}
}

// TestMissingConditionParam: a transition condition referencing a missing
// parameter does not fire and reports the transition.
func TestMissingConditionParam(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	// speed intentionally not set.
	r := en.Advance(eng, 0.1)
	for _, d := range r.Diagnostics {
		if d.Kind == DiagMissingParam && d.Param == "speed" {
			if !strings.Contains(d.Binding, "transition.to_run.condition") {
				t.Fatalf("diagnostic binding %q does not locate the transition", d.Binding)
			}
			return
		}
	}
	t.Fatalf("expected missing_param diagnostic for speed, got %v", r.Diagnostics)
}

// TestTypeMismatch: a bool parameter given a non-0/1 value degrades to the
// default and reports the binding.
func TestTypeMismatch(t *testing.T) {
	def := testGraph()
	// Add a bool parameter binding to a clip weight.
	def.Params = append(def.Params, graph.ParamDef{Name: "enable", Type: graph.ParamBool, Default: 0})
	def.States[0].Root.Children[0].Weight = graph.BindingDef{Param: "enable"}
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("enable", 5) // not a valid bool
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	found := false
	for _, d := range r.Diagnostics {
		if d.Kind == DiagTypeMismatch && d.Param == "enable" {
			found = true
			if d.Degraded != 0 {
				t.Fatalf("degraded = %g, want default 0", d.Degraded)
			}
		}
	}
	if !found {
		t.Fatalf("expected type_mismatch diagnostic, got %v", r.Diagnostics)
	}
}

// TestOutOfRange: an out-of-range parameter is clamped and reported.
func TestOutOfRange(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1.5) // above max 1
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	found := false
	for _, d := range r.Diagnostics {
		if d.Kind == DiagOutOfRange && d.Param == "weight0" {
			found = true
			if d.Degraded != 1 {
				t.Fatalf("degraded = %g, want clamped 1", d.Degraded)
			}
		}
	}
	if !found {
		t.Fatalf("expected out_of_range diagnostic, got %v", r.Diagnostics)
	}
	if !floatsClose(r.Pose, []float64{1, 0, 0}) {
		t.Fatalf("pose = %v, want idle pose", r.Pose)
	}
}

// TestDegenerateWeights: when all weights sum to zero, the configured policy
// is applied and reported.
func TestDegenerateWeights(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 0)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	kinds := diagKinds(r.Diagnostics)
	if kinds[DiagDegenerateWeights] == 0 {
		t.Fatalf("expected degenerate_weights diagnostic, got %v", r.Diagnostics)
	}
	// Default policy: zero pose.
	if !floatsClose(r.Pose, []float64{0, 0, 0}) {
		t.Fatalf("pose = %v, want zero pose", r.Pose)
	}

	def2 := testGraph()
	def2.ZeroWeight = graph.EqualWeights
	eng2, err := New(def2, 2)
	if err != nil {
		t.Fatal(err)
	}
	en2 := NewEntity("e2", 2)
	en2.SetParam("weight0", 0)
	en2.SetParam("weight1", 0)
	en2.SetParam("speed", 0)
	r2 := en2.Advance(eng2, 0.1)
	if !floatsClose(r2.Pose, []float64{0.5, 0.5, 0}) {
		t.Fatalf("pose = %v, want equal-weight blend", r2.Pose)
	}
}

// TestIntTypeMismatch: an int parameter given a fractional value degrades.
func TestIntTypeMismatch(t *testing.T) {
	def := testGraph()
	def.Params = append(def.Params, graph.ParamDef{Name: "count", Type: graph.ParamInt, Default: 0})
	def.States[0].Root.Children[0].Weight = graph.BindingDef{Param: "count"}
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("count", 1.5)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	found := false
	for _, d := range r.Diagnostics {
		if d.Kind == DiagTypeMismatch && d.Param == "count" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected type_mismatch diagnostic for int param, got %v", r.Diagnostics)
	}
}
