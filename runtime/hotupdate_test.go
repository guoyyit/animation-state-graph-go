package runtime

import (
	"sync"
	"testing"

	"github.com/guoyyit/animation-state-graph-go/graph"
)

// TestHotUpdate: a valid update swaps the snapshot; subsequent frames use the
// new graph.
func TestHotUpdate(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r1 := en.Advance(eng, 0.1)
	if !floatsClose(r1.Pose, []float64{1, 0, 0}) {
		t.Fatalf("pre-update pose = %v", r1.Pose)
	}

	// New graph: idle clip channels changed.
	def2 := testGraph()
	def2.Clips[0].Channels = []float64{0.5, 0.5, 0}
	if err := eng.Update(def2); err != nil {
		t.Fatalf("valid update failed: %v", err)
	}
	r2 := en.Advance(eng, 0.1)
	if !floatsClose(r2.Pose, []float64{0.5, 0.5, 0}) {
		t.Fatalf("post-update pose = %v, want new idle pose", r2.Pose)
	}
}

// TestInvalidUpdateKeepsPrevious: a failed validation leaves the previous
// snapshot active.
func TestInvalidUpdateKeepsPrevious(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	bad := testGraph()
	bad.States[0].Root.Children[0].Weight.Param = "does_not_exist"
	if err := eng.Update(bad); err == nil {
		t.Fatal("expected validation error for invalid update")
	}
	// Previous snapshot still active.
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 0)
	r := en.Advance(eng, 0.1)
	noDiags(t, r)
	if !floatsClose(r.Pose, []float64{1, 0, 0}) {
		t.Fatalf("pose = %v, want old graph pose", r.Pose)
	}
}

// TestUpdateRemovesCurrentState: an entity whose state was removed by the
// update falls back to the new initial state without panicking.
func TestUpdateRemovesCurrentState(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	en.Advance(eng, 0.1) // start crossfade
	en.Advance(eng, 0.6) // complete crossfade into run
	if en.State() != "run" {
		t.Fatalf("precondition: state = %q", en.State())
	}

	def2 := testGraph()
	// Remove the run state.
	var states []graph.StateDef
	for _, s := range def2.States {
		if s.ID != "run" {
			states = append(states, s)
		}
	}
	def2.States = states
	var trans []graph.TransitionDef
	for _, t := range def2.Transitions {
		if t.From != "run" && t.To != "run" {
			trans = append(trans, t)
		}
	}
	def2.Transitions = trans
	if err := eng.Update(def2); err != nil {
		t.Fatal(err)
	}
	r := en.Advance(eng, 0.1)
	if r.State != "idle" {
		t.Fatalf("state = %q, want fallback to idle", r.State)
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Kind == DiagStateNotFound {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected state_not_found diagnostic, got %v", r.Diagnostics)
	}
}

// TestConcurrentUpdateAndAdvance: hammering updates and advances concurrently
// must not panic; every pose must come from one complete snapshot.
func TestConcurrentUpdateAndAdvance(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = eng.Update(testGraph())
				}
			}
		}()
	}
	// Each advance goroutine owns its own entities (an entity must never be
	// advanced by two workers concurrently).
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			myEnts := make([]*Entity, 20)
			for i := range myEnts {
				myEnts[i] = NewEntity("e", int64(i))
				myEnts[i].SetParam("weight0", 1)
				myEnts[i].SetParam("weight1", 0)
				myEnts[i].SetParam("speed", 0)
			}
			for i := 0; i < 200; i++ {
				results := eng.AdvanceAll(1.0/60.0, myEnts, 4)
				for _, r := range results {
					if len(r.Pose) != 3 {
						t.Errorf("pose has %d channels", len(r.Pose))
					}
				}
			}
		}()
	}
	close(stop)
	wg.Wait()
}
