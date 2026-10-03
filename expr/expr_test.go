package expr

import "testing"

type lookup map[string]float64

func (l lookup) GetParam(name string) (float64, bool) {
	v, ok := l[name]
	return v, ok
}

func TestParseAndEval(t *testing.T) {
	cases := []struct {
		src  string
		vals lookup
		want float64
	}{
		{"1 + 2", nil, 3},
		{"2 * 3 + 4", nil, 10},
		{"2 + 3 * 4", nil, 14},
		{"(2 + 3) * 4", nil, 20},
		{"10 / 4", nil, 2.5},
		{"speed > 1", lookup{"speed": 2}, 1},
		{"speed > 1", lookup{"speed": 0}, 0},
		{"speed >= 1", lookup{"speed": 1}, 1},
		{"speed == 1", lookup{"speed": 1}, 1},
		{"speed != 1", lookup{"speed": 1}, 0},
		{"a && b", lookup{"a": 1, "b": 0}, 0},
		{"a && b", lookup{"a": 1, "b": 1}, 1},
		{"a || b", lookup{"a": 0, "b": 1}, 1},
		{"!a", lookup{"a": 0}, 1},
		{"-x", lookup{"x": 3}, -3},
		{"speed > 1 && grounded == 1", lookup{"speed": 5, "grounded": 1}, 1},
		{"speed > 10 || grounded == 0", lookup{"speed": 5, "grounded": 0}, 1},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			n, err := Parse(c.src)
			if err != nil {
				t.Fatal(err)
			}
			f, err := Compile(n)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := f(c.vals)
			if !ok {
				t.Fatal("evaluation failed")
			}
			if got != c.want {
				t.Fatalf("got %g, want %g", got, c.want)
			}
		})
	}
}

func TestMissingParam(t *testing.T) {
	n, err := Parse("speed > 1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := Compile(n)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f(lookup{}); ok {
		t.Fatal("expected missing param to report not-ok")
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{"", "1 +", ">", "()", "1 2"} {
		if _, err := Parse(src); err == nil {
			t.Fatalf("expected parse error for %q", src)
		}
	}
}
