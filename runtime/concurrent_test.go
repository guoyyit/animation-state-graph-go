package runtime

import (
	"fmt"
	"math/rand"
	"testing"
)

// scenario runs a fixed parameter sequence for an entity and returns the
// per-frame poses.
func scenario(t *testing.T, eng *Engine, en *Entity, frames int) [][]float64 {
	t.Helper()
	poses := make([][]float64, frames)
	rng := rand.New(rand.NewSource(1234))
	for i := 0; i < frames; i++ {
		en.SetParam("weight0", rng.Float64())
		en.SetParam("weight1", rng.Float64())
		if rng.Float64() < 0.3 {
			en.SetParam("speed", 0)
		} else {
			en.SetParam("speed", 5)
		}
		en.SetBool("grounded", rng.Float64() < 0.5)
		r := en.Advance(eng, 1.0/60.0)
		poses[i] = r.Pose
	}
	return poses
}

// TestDeterminismAcrossConcurrency: the same scenario produces identical
// poses regardless of worker count or run count.
func TestDeterminismAcrossConcurrency(t *testing.T) {
	eng, err := New(testGraph(), 99)
	if err != nil {
		t.Fatal(err)
	}
	const n = 40
	const frames = 30

	// Reference run: single entity, fixed parameter sequence.
	refEnt := NewEntity("ref", 1)
	ref := scenario(t, eng, refEnt, frames)

	// For each worker count, advance n entities frame by frame with the same
	// parameter sequence and compare entity 0 against the reference.
	for _, workers := range []int{1, 2, 4, 8, 16} {
		ents := make([]*Entity, n)
		for i := range ents {
			ents[i] = NewEntity(fmt.Sprintf("e%d", i), int64(i)+1)
		}
		rngs := make([]*rand.Rand, n)
		for i := range rngs {
			rngs[i] = rand.New(rand.NewSource(1234))
		}
		got := make([][]float64, frames)
		for f := 0; f < frames; f++ {
			for i, en := range ents {
				rng := rngs[i]
				en.SetParam("weight0", rng.Float64())
				en.SetParam("weight1", rng.Float64())
				if rng.Float64() < 0.3 {
					en.SetParam("speed", 0)
				} else {
					en.SetParam("speed", 5)
				}
				en.SetBool("grounded", rng.Float64() < 0.5)
			}
			results := eng.AdvanceAll(1.0 / 60.0, ents, workers)
			got[f] = results[0].Pose
		}
		for f := range ref {
			if !floatsClose(got[f], ref[f]) {
				t.Fatalf("workers=%d frame %d: pose %v != ref %v", workers, f, got[f], ref[f])
			}
		}
	}
}

// TestAdvanceAllOrdering: results are returned in input order and all
// entities advance independently.
func TestAdvanceAllOrdering(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ents := make([]*Entity, 8)
	for i := range ents {
		ents[i] = NewEntity(fmt.Sprintf("e%d", i), int64(i))
		ents[i].SetParam("weight0", 1)
		ents[i].SetParam("weight1", 0)
		ents[i].SetParam("speed", 0)
	}
	results := eng.AdvanceAll(1.0/60.0, ents, 4)
	for i, r := range results {
		if r.EntityID != fmt.Sprintf("e%d", i) {
			t.Fatalf("result %d has entity id %q", i, r.EntityID)
		}
		if r.Frame != 1 {
			t.Fatalf("entity %d frame = %d, want 1", i, r.Frame)
		}
	}
}

// TestPanicRecovery: a misbehaving entity (nil engine path is impossible,
// but a corrupted pose channel) does not take down other entities.
func TestPanicRecovery(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	good := NewEntity("good", 1)
	good.SetParam("weight0", 1)
	good.SetParam("weight1", 0)
	good.SetParam("speed", 0)

	// An entity with a manually broken state reference triggers the
	// state-not-found fallback rather than a panic.
	bad := NewEntity("bad", 2)
	bad.state = "does_not_exist"
	bad.SetParam("weight0", 1)
	bad.SetParam("weight1", 0)
	bad.SetParam("speed", 0)

	results := eng.AdvanceAll(1.0/60.0, []*Entity{bad, good}, 2)
	if results[0].EntityID != "bad" {
		t.Fatalf("result order broken")
	}
	if len(results[0].Diagnostics) == 0 {
		t.Fatalf("expected diagnostics for bad entity")
	}
	if results[1].State != "idle" {
		t.Fatalf("good entity affected by bad entity: state=%q", results[1].State)
	}
}
