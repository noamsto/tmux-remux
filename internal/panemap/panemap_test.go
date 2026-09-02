package panemap

import (
	"errors"
	"testing"
)

func TestParse_NestedLayout(t *testing.T) {
	// Captured from a real snapshot: full-width pane on top, two side by side
	// underneath.
	const layout = "1cb4,145x36,0,0[145x18,0,0,0,145x17,0,19{72x17,0,19,1,72x17,73,19,2}]"

	g, err := Parse(layout)
	if err != nil {
		t.Fatalf("Parse returned %v", err)
	}
	if g.W != 145 || g.H != 36 {
		t.Errorf("window = %dx%d, want 145x36", g.W, g.H)
	}
	want := []Rect{
		{Index: 0, W: 145, H: 18, X: 0, Y: 0},
		{Index: 1, W: 72, H: 17, X: 0, Y: 19},
		{Index: 2, W: 72, H: 17, X: 73, Y: 19},
	}
	if len(g.Panes) != len(want) {
		t.Fatalf("got %d panes, want %d: %+v", len(g.Panes), len(want), g.Panes)
	}
	for i := range want {
		if g.Panes[i] != want[i] {
			t.Errorf("pane %d = %+v, want %+v", i, g.Panes[i], want[i])
		}
	}
}

func TestParse_SinglePane(t *testing.T) {
	// A one-pane window: the window tuple carries the pane index directly.
	g, err := Parse("9f58,80x24,0,0,0")
	if err != nil {
		t.Fatalf("Parse returned %v", err)
	}
	if g.W != 80 || g.H != 24 {
		t.Errorf("window = %dx%d, want 80x24", g.W, g.H)
	}
	if len(g.Panes) != 1 || g.Panes[0] != (Rect{Index: 0, W: 80, H: 24, X: 0, Y: 0}) {
		t.Errorf("panes = %+v, want one 80x24 pane at 0,0", g.Panes)
	}
}

func TestParse_Columns(t *testing.T) {
	// A pure side-by-side split: two panes, no nesting.
	g, err := Parse("abcd,80x24,0,0{40x24,0,0,0,39x24,41,0,1}")
	if err != nil {
		t.Fatalf("Parse returned %v", err)
	}
	want := []Rect{
		{Index: 0, W: 40, H: 24, X: 0, Y: 0},
		{Index: 1, W: 39, H: 24, X: 41, Y: 0},
	}
	if len(g.Panes) != len(want) {
		t.Fatalf("got %d panes, want %d: %+v", len(g.Panes), len(want), g.Panes)
	}
	for i := range want {
		if g.Panes[i] != want[i] {
			t.Errorf("pane %d = %+v, want %+v", i, g.Panes[i], want[i])
		}
	}
}

func TestParse_Rejects(t *testing.T) {
	inputs := []string{
		"",                                       // empty
		"nonsense",                               // no comma
		"1cb4",                                   // checksum only
		"1cb4,notxgeometry,0,0",                  // window size not a number
		"1cb4,80x24,0,0[80x24,0,0,0",             // unbalanced bracket
		"1cb4,80x24,0,0{80x24,0,0,0]",            // mismatched bracket
		"1cb4,80x24,0,0,0trailing",               // trailing garbage after a full parse
		"1cb4,80x24,0,0[80x11,0,0,0,80x12,0,12]", // container child missing its pane index
	}
	for _, in := range inputs {
		if _, err := Parse(in); !errors.Is(err, ErrNoLayout) {
			t.Errorf("Parse(%q) error = %v, want ErrNoLayout", in, err)
		}
	}
}
