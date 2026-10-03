package runtime

import (
	"math/rand"
	"testing"
)

// TestRecovery: an entity restored from a committed snapshot reproduces the
// output of an uninterrupted run when re-supplied with the same parameter
// sequence.
func TestRecovery(t *testing.T) {
	eng, err := New(testGraph(), 17)
	if err != nil {
		t.Fatal(err)
	}

	// Reference run: 20 frames.
	ref := NewEntity("e", 1)
	refPoses := scenario(t, eng, ref, 20)

	// Run 8 frames, snapshot, serialize, restore, then run 12 more frames.
	en := NewEntity("e", 1)
	rng := rand.New(rand.NewSource(1234))
	for f := 0; f < 8; f++ {
		en.SetParam("weight0", rng.Float64())
		en.SetParam("weight1", rng.Float64())
		if rng.Float64() < 0.3 {
			en.SetParam("speed", 0)
		} else {
			en.SetParam("speed", 5)
		}
		en.SetBool("grounded", rng.Float64() < 0.5)
		en.Advance(eng, 1.0/60.0)
	}

	snap := en.Snapshot()
	b, err := MarshalSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := UnmarshalSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	en2, err := RestoreEntity(restored)
	if err != nil {
		t.Fatal(err)
	}
	if en2.State() != snap.State || en2.Frame() != snap.Frame {
		t.Fatalf("restored state=%q frame=%d, want %q/%d", en2.State(), en2.Frame(), snap.State, snap.Frame)
	}

	for f := 8; f < 20; f++ {
		en2.SetParam("weight0", rng.Float64())
		en2.SetParam("weight1", rng.Float64())
		if rng.Float64() < 0.3 {
			en2.SetParam("speed", 0)
		} else {
			en2.SetParam("speed", 5)
		}
		en2.SetBool("grounded", rng.Float64() < 0.5)
		r := en2.Advance(eng, 1.0/60.0)
		if !floatsClose(r.Pose, refPoses[f]) {
			t.Fatalf("frame %d after recovery: pose %v != ref %v", f, r.Pose, refPoses[f])
		}
	}
}

// TestRecoveryMidTransition: snapshotting in the middle of a crossfade
// restores the crossfade position.
func TestRecoveryMidTransition(t *testing.T) {
	eng, err := New(testGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)
	en.Advance(eng, 0.1) // crossfade started, t=0
	en.Advance(eng, 0.1) // t=0.4
	if !en.InTransition() {
		t.Fatal("precondition: entity should be mid-transition")
	}
	snap := en.Snapshot()
	restored, err := RestoreEntity(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.InTransition() {
		t.Fatal("restored entity should be mid-transition")
	}
	if restored.TransitionProgress() != en.TransitionProgress() {
		t.Fatalf("progress %g != %g", restored.TransitionProgress(), en.TransitionProgress())
	}
	// Continue both and compare.
	en.SetParam("speed", 5)
	restored.SetParam("speed", 5)
	r1 := en.Advance(eng, 0.1)
	r2 := restored.Advance(eng, 0.1)
	if !floatsClose(r1.Pose, r2.Pose) {
		t.Fatalf("post-recovery poses differ: %v vs %v", r1.Pose, r2.Pose)
	}
}
