package runtime

import (
	"math"
	"testing"
)

// TestBlendNormalization verifies weights are normalized and the pose is the
// weighted sum of clip poses.
func TestBlendNormalization(t *testing.T) {
	eng, err := New(testGraph(), 42)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 0.75)
	en.SetParam("weight1", 0.25)
	en.SetParam("speed", 0)

	r := en.Advance(eng, 1.0/60.0)
	noDiags(t, r)
	want := []float64{0.75, 0.25, 0}
	if !floatsClose(r.Pose, want) {
		t.Fatalf("pose = %v, want %v", r.Pose, want)
	}

	// Non-normalized raw weights are normalized the same way. Use a graph
	// without schema ranges so the raw values are not clamped.
	def2 := testGraph()
	for i := range def2.Params {
		if def2.Params[i].Name == "weight0" || def2.Params[i].Name == "weight1" {
			def2.Params[i].HasMin = false
			def2.Params[i].HasMax = false
		}
	}
	eng2, err := New(def2, 2)
	if err != nil {
		t.Fatal(err)
	}
	en2 := NewEntity("e2", 2)
	en2.SetParam("weight0", 3)
	en2.SetParam("weight1", 1)
	en2.SetParam("speed", 0)
	r2 := en2.Advance(eng2, 1.0/60.0)
	noDiags(t, r2)
	if !floatsClose(r2.Pose, want) {
		t.Fatalf("pose = %v, want %v (normalization)", r2.Pose, want)
	}
}

// TestCrossfade verifies a transition produces interpretable intermediate
// poses: t=0 equals the outgoing pose, t=1 equals the incoming pose, and the
// pose changes continuously without jumps.
func TestCrossfade(t *testing.T) {
	eng, err := New(testGraph(), 7)
	if err != nil {
		t.Fatal(err)
	}
	en := NewEntity("e", 1)
	en.SetParam("weight0", 1)
	en.SetParam("weight1", 0)
	en.SetParam("speed", 5)

	dt := 0.2
	var poses [][]float64
	for i := 0; i < 4; i++ {
		r := en.Advance(eng, dt)
		noDiags(t, r)
		poses = append(poses, r.Pose)
	}

	// Frame 1: transition just started, t=0 -> idle pose.
	if !floatsClose(poses[0], []float64{1, 0, 0}) {
		t.Fatalf("frame1 (t=0) = %v, want idle pose", poses[0])
	}
	// Frame 2: t=0.4 -> 0.6*idle + 0.4*run.
	if !floatsClose(poses[1], []float64{0.6, 0.4, 0}) {
		t.Fatalf("frame2 (t=0.4) = %v", poses[1])
	}
	// Frame 3: t=0.8 -> 0.2*idle + 0.8*run.
	if !floatsClose(poses[2], []float64{0.2, 0.8, 0}) {
		t.Fatalf("frame3 (t=0.8) = %v", poses[2])
	}
	// Frame 4: transition complete -> run pose.
	if !floatsClose(poses[3], []float64{0, 1, 0}) {
		t.Fatalf("frame4 (t=1) = %v, want run pose", poses[3])
	}

	// Continuity: consecutive poses must not jump (max component delta <=
	// the crossfade step of 0.4).
	for i := 1; i < len(poses); i++ {
		for j := range poses[i] {
			d := math.Abs(poses[i][j] - poses[i-1][j])
			if d > 0.4+1e-9 {
				t.Fatalf("jump between frame %d and %d: delta %g", i-1, i, d)
			}
		}
	}
	if en.State() != "run" {
		t.Fatalf("state = %q, want run", en.State())
	}
}

// TestInstantTransition verifies Duration=0 switches state immediately on
// the frame the condition fires.
func TestInstantTransition(t *testing.T) {
	def := testGraph()
	def.Transitions[0].Duration = 0
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
	if r.State != "run" || r.InTransition {
		t.Fatalf("state=%q inTransition=%v, want run/false", r.State, r.InTransition)
	}
	if !floatsClose(r.Pose, []float64{0, 1, 0}) {
		t.Fatalf("pose = %v, want run pose", r.Pose)
	}
}

func floatsClose(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}
