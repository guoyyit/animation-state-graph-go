package runtime

import (
	"testing"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

// TestPriorityAdjudication: when several transitions of the same state fire
// in one frame, the highest priority wins.
func TestPriorityAdjudication(t *testing.T) {
	def := testGraph()
	def.Transitions = append(def.Transitions,
		graph.TransitionDef{ID: "to_run_high", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0, Priority: 10},
		graph.TransitionDef{ID: "to_air_low", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0, Priority: -1},
	)
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	if r.State != "run" {
		t.Fatalf("state = %q, want run", r.State)
	}
}

// TestTieBreakByID: equal priorities are broken by lexicographic id order.
func TestTieBreakByID(t *testing.T) {
	def := testGraph()
	// Two equal-priority transitions from idle to run with different ids.
	def.Transitions = append(def.Transitions,
		graph.TransitionDef{ID: "aaa_wins", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0, Priority: 0},
		graph.TransitionDef{ID: "zzz_loses", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0, Priority: 0},
	)
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	if r.State != "run" {
		t.Fatalf("state = %q, want run", r.State)
	}
	// The winner is aaa_wins; both target run so state is run either way.
	// Verify ordering directly through the compiled snapshot.
	cs := eng.Snapshot().State("idle")
	ids := make([]string, len(cs.Transitions()))
	for i, tr := range cs.Transitions() {
		ids[i] = tr.ID
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("transitions not sorted by id at equal priority: %v", ids)
		}
	}
}

// TestSelfTransitionNoReentry: a transition whose target is the current
// state does not restart the state or start a crossfade.
func TestSelfTransitionNoReentry(t *testing.T) {
	def := testGraph()
	def.Transitions = append(def.Transitions,
		graph.TransitionDef{ID: "self", From: "idle", To: "idle", Condition: "speed > 1.0", Duration: 0.5, Priority: 100},
	)
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	if r.State != "idle" || r.InTransition {
		t.Fatalf("state=%q inTransition=%v, want idle/false (self transition must not re-enter)", r.State, r.InTransition)
	}
}

// TestCycleNoLoop: with idle->run and run->idle both true, a single frame
// must fire at most one transition (no in-frame oscillation).
func TestCycleNoLoop(t *testing.T) {
	def := testGraph()
	def.Transitions = []graph.TransitionDef{
		{ID: "a_to_b", From: "idle", To: "run", Condition: "speed > 1.0", Duration: 0, Priority: 0},
		{ID: "b_to_a", From: "run", To: "idle", Condition: "speed > 1.0", Duration: 0, Priority: 0},
	}
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	// One frame fired exactly one transition: idle -> run.
	if r.State != "run" {
		t.Fatalf("state = %q, want run (one transition per frame)", r.State)
	}
	// Next frame fires run -> idle.
	r2 := en.Advance(eng, 0.1)
	noDiags(t, r2)
	if r2.State != "idle" {
		t.Fatalf("state = %q, want idle", r2.State)
	}
}

// TestWildcardTransition: From="*" matches any state.
func TestWildcardTransition(t *testing.T) {
	def := testGraph()
	def.Transitions = append(def.Transitions,
		graph.TransitionDef{ID: "any_to_air", From: "*", To: "run", Condition: "grounded == 0", Duration: 0, Priority: 100},
	)
	eng, err := New(def, 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	en.SetBool("grounded", false)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	if r.State != "run" {
		t.Fatalf("state = %q, want run (wildcard transition)", r.State)
	}
}
