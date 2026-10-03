package graph

import "testing"

func strptr(s string) *string { return &s }

func validDef() *GraphDef {
	return &GraphDef{
		Name: "t",
		Params: []ParamDef{
			{Name: "p", Type: ParamFloat, Default: 0},
		},
		Clips: []ClipDef{
			{ID: "c", Channels: []float64{1}},
		},
		States: []StateDef{
			{ID: "s", Root: BlendNodeDef{ID: "b", Children: []BlendChildDef{
				{Clip: strptr("c"), Weight: BindingDef{Param: "p"}},
			}}},
		},
		Transitions: []TransitionDef{
			{ID: "t", From: "s", To: "s", Condition: "p > 0", Duration: 0},
		},
		InitialState: "s",
	}
}

func TestCompileValid(t *testing.T) {
	g, err := Compile(validDef())
	if err != nil {
		t.Fatal(err)
	}
	if g.InitialState() != "s" {
		t.Fatalf("initial = %q", g.InitialState())
	}
	if g.ChannelCount() != 1 {
		t.Fatalf("channels = %d", g.ChannelCount())
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*GraphDef)
	}{
		{"duplicate param", func(d *GraphDef) { d.Params = append(d.Params, d.Params[0]) }},
		{"duplicate clip", func(d *GraphDef) { d.Clips = append(d.Clips, d.Clips[0]) }},
		{"duplicate state", func(d *GraphDef) { d.States = append(d.States, d.States[0]) }},
		{"duplicate transition", func(d *GraphDef) { d.Transitions = append(d.Transitions, d.Transitions[0]) }},
		{"unknown clip", func(d *GraphDef) { d.States[0].Root.Children[0].Clip = strptr("nope") }},
		{"unknown binding param", func(d *GraphDef) { d.States[0].Root.Children[0].Weight.Param = "nope" }},
		{"unknown condition param", func(d *GraphDef) { d.Transitions[0].Condition = "nope > 0" }},
		{"unknown transition target", func(d *GraphDef) { d.Transitions[0].To = "nope" }},
		{"unknown transition source", func(d *GraphDef) { d.Transitions[0].From = "nope" }},
		{"bad condition", func(d *GraphDef) { d.Transitions[0].Condition = "p >" }},
		{"negative duration", func(d *GraphDef) { d.Transitions[0].Duration = -1 }},
		{"empty condition", func(d *GraphDef) { d.Transitions[0].Condition = "" }},
		{"clip channel mismatch", func(d *GraphDef) { d.Clips = append(d.Clips, ClipDef{ID: "c2", Channels: []float64{1, 2}}) }},
		{"no states", func(d *GraphDef) { d.States = nil }},
		{"initial not found", func(d *GraphDef) { d.InitialState = "nope" }},
		{"param min>max", func(d *GraphDef) { d.Params[0].HasMin, d.Params[0].HasMax, d.Params[0].Min, d.Params[0].Max = true, true, 2, 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			def := validDef()
			c.mod(def)
			if _, err := Compile(def); err == nil {
				t.Fatalf("expected compile error for %s", c.name)
			}
		})
	}
}

func TestCompileNil(t *testing.T) {
	if _, err := Compile(nil); err == nil {
		t.Fatal("expected error for nil definition")
	}
}
