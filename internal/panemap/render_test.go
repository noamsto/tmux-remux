package panemap

import (
	"strings"
	"testing"
)

// Real layout strings so parse and render are exercised together, the way the
// picker uses them. The checksum before the first comma is ignored, so "x" is a
// fine stand-in.
const (
	layoutStack     = "x,80x24,0,0[80x11,0,0,0,80x12,0,12,1]"                                 // two panes, one above the other
	layoutCols      = "x,100x20,0,0{69x20,0,0,0,30x20,70,0,1}"                                // two panes side by side, 70/30
	layoutDemo      = "1cb4,145x36,0,0[145x18,0,0,0,145x17,0,19{72x17,0,19,1,72x17,73,19,2}]" // full-width top, two below
	layoutStaggered = "x,80x24,0,0{40x24,0,0[40x11,0,0,0,40x12,0,12,1],39x24,41,0[39x9,41,0,2,39x14,41,10,3]}"
	// A one-row pane (index 1) wedged between two others in the left column,
	// with the right column split at a height that lands on pane 1's own edges.
	// This is the geometry the old coordinate-merge erased; the tree keeps it.
	layoutWedged = "x,80x24,0,0{39x24,0,0[39x10,0,0,0,39x1,0,11,1,39x11,0,13,2],40x24,40,0[40x11,40,0,3,40x12,40,12,4]}"
)

func mustParse(t *testing.T, layout string) Grid {
	t.Helper()
	g, err := Parse(layout)
	if err != nil {
		t.Fatalf("Parse(%q): %v", layout, err)
	}
	return g
}

// One pane filling the window is one box filling the panel.
func TestRender_SinglePane(t *testing.T) {
	got := render(mustParse(t, "x,80x24,0,0,0"), 10, 4, nil, nil)
	want := strings.Join([]string{
		"┌────────┐",
		"│ 0      │",
		"│        │",
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Two stacked panes share one divider row, drawn with ├ ┤ junctions rather than
// two abutting borders.
func TestRender_StackedPanesShareDivider(t *testing.T) {
	got := render(mustParse(t, layoutStack), 10, 5, nil, nil)
	want := strings.Join([]string{
		"┌────────┐",
		"│ 0      │",
		"├────────┤",
		"│ 1      │",
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The same layout at h=8 exposed the old per-boundary rounding, which split the
// shared divider across two rows. With the tree there is exactly one.
func TestRender_StackedPanesShareDivider_AtH8(t *testing.T) {
	got := render(mustParse(t, layoutStack), 10, 8, nil, nil)
	want := strings.Join([]string{
		"┌────────┐",
		"│ 0      │",
		"│        │",
		"├────────┤",
		"│ 1      │",
		"│        │",
		"│        │",
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A nested layout — full-width top, two side by side below — draws the T
// junction where the vertical divider meets the horizontal one.
func TestRender_NestedLayout(t *testing.T) {
	got := render(mustParse(t, layoutDemo), 24, 6, nil, nil)
	want := strings.Join([]string{
		"┌──────────────────────┐",
		"│ 0                    │",
		"│                      │",
		"├──────────┬───────────┤",
		"│ 1        │ 2         │",
		"└──────────┴───────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Proportions survive scaling: a 70/30 side-by-side split stays 70/30, not 50/50.
func TestRender_KeepsProportions(t *testing.T) {
	lines := strings.Split(render(mustParse(t, layoutCols), 21, 3, nil, nil), "\n")
	left := strings.IndexRune(lines[1], '│')
	rest := strings.IndexRune(lines[1][left+1:], '│')
	if left != 0 {
		t.Fatalf("left edge at %d, want 0", left)
	}
	if mid := left + 1 + rest; mid < 12 || mid > 16 {
		t.Errorf("middle divider at column %d, want ~14 (70%% of 20)", mid)
	}
}

// Each pane's label is written inside its box.
func TestRender_LabelsPanes(t *testing.T) {
	label := func(i int) string { return []string{"nvim", "agent-work"}[i] }
	got := render(mustParse(t, layoutStack), 30, 7, label, nil)
	if !strings.Contains(got, "nvim") || !strings.Contains(got, "agent-work") {
		t.Errorf("labels missing from:\n%s", got)
	}
}

// A label longer than its box must not spill past the border.
func TestRender_LabelTruncatedToBox(t *testing.T) {
	got := render(mustParse(t, "x,80x24,0,0,0"), 24, 6, func(int) string { return strings.Repeat("x", 100) }, nil)
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) != 24 {
			t.Fatalf("line %q is %d runes, want 24", line, len([]rune(line)))
		}
	}
}

// The marked pane's border is dashed so the map says which one died.
func TestRender_MarkedPaneUsesDashedBorder(t *testing.T) {
	marked := func(i int) bool { return i == 1 }
	got := render(mustParse(t, layoutStack), 30, 7, nil, marked)
	if !strings.ContainsRune(got, '┄') && !strings.ContainsRune(got, '┆') {
		t.Errorf("no dashed border for the marked pane:\n%s", got)
	}
}

// Two columns split at different heights keep each column's divider on its own
// row — the left at row 5 (├), the right at row 4 (┤), never merged onto one.
// Exact art so the junction glyphs where each staggered divider meets the centre
// line are pinned too.
func TestRender_StaggeredColumnsKeepSeparateDividers(t *testing.T) {
	got := render(mustParse(t, layoutStaggered), 30, 12, nil, nil)
	want := strings.Join([]string{
		"┌──────────────┬─────────────┐",
		"│ 0            │ 2           │",
		"│              │             │",
		"│              │             │",
		"│              ├─────────────┤",
		"├──────────────┤ 3           │",
		"│ 1            │             │",
		"│              │             │",
		"│              │             │",
		"│              │             │",
		"│              │             │",
		"└──────────────┴─────────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A one-row pane between two others, in a window whose other column splits at
// that pane's edges, must still draw with an interior. The old merge chained
// the boundaries across columns and gave this pane zero height at every size.
func TestRender_WedgedPaneSurvives(t *testing.T) {
	g := mustParse(t, layoutWedged)
	var boxes []box
	if !layoutNode(g.root, 0, 0, 79, 23, &boxes) {
		t.Fatal("layout unexpectedly undrawable at 80x24")
	}
	var found bool
	for _, b := range boxes {
		if b.idx == 1 {
			found = true
			if b.x1-b.x0 < 2 || b.y1-b.y0 < 2 {
				t.Errorf("pane 1 drawn without interior: %+v", b)
			}
		}
	}
	if !found {
		t.Error("pane 1 was erased from the map")
	}
}

// Below the size guard, Render shows a one-line summary rather than art.
func TestRender_TooSmallFallsBackToSummary(t *testing.T) {
	got := Render(mustParse(t, layoutDemo), minMapWidth-1, minMapHeight, nil, nil)
	if want := "3 panes · 145×36"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// When a pane cannot fit an interior at the given size, the whole map degrades
// to the summary — never to bare abutting borders. Each case is above the size
// guard, so it is the geometry, not the panel, that triggers the fallback.
func TestRender_UndrawableFallsBackToSummary(t *testing.T) {
	cases := []struct {
		name        string
		layout      string
		w, h        int
		wantSummary string
	}{
		{
			name:        "one-row pane too small",
			layout:      layoutWedged,
			w:           40,
			h:           12,
			wantSummary: "5 panes · 80×24",
		},
		{
			name:        "narrow side-by-side panes",
			layout:      "x,200x50,0,0{8x50,0,0,0,8x50,9,0,1,182x50,18,0,2}",
			w:           24,
			h:           8,
			wantSummary: "3 panes · 200×50",
		},
		{
			name:        "six stacked panes below the floor",
			layout:      "x,80x24,0,0[80x3,0,0,0,80x3,0,4,1,80x3,0,8,2,80x3,0,12,3,80x3,0,16,4,80x3,0,20,5]",
			w:           24,
			h:           6,
			wantSummary: "6 panes · 80×24",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := render(mustParse(t, c.layout), c.w, c.h, nil, nil)
			if got != c.wantSummary {
				t.Errorf("got:\n%s\nwant summary %q", got, c.wantSummary)
			}
		})
	}
}

// No drawable layout ever produces two abutting parallel border lines: the
// interior requirement guarantees a cell of space between any two dividers.
func TestRender_NoAbuttingBorders(t *testing.T) {
	layouts := []string{layoutStack, layoutCols, layoutDemo, layoutStaggered, layoutWedged}
	for _, layout := range layouts {
		g := mustParse(t, layout)
		for w := minMapWidth; w <= 90; w += 7 {
			for h := minMapHeight; h <= 24; h += 3 {
				out := render(g, w, h, nil, nil)
				if strings.HasPrefix(out, "─") || !strings.Contains(out, "\n") {
					continue // summary fallback, nothing to check
				}
				assertNoAbutting(t, layout, w, h, out)
			}
		}
	}
}

func assertNoAbutting(t *testing.T, layout string, w, h int, out string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	isBorderRow := func(l string) bool {
		return !strings.ContainsRune(l, ' ') && strings.ContainsRune(l, '─')
	}
	for i := 1; i < len(lines); i++ {
		if isBorderRow(lines[i-1]) && isBorderRow(lines[i]) {
			t.Errorf("%s at %dx%d: abutting border rows %d,%d:\n%s", layout, w, h, i-1, i, out)
			return
		}
	}
	// Abutting vertical borders show as "││" with no interior between two boxes.
	for _, l := range lines {
		if strings.Contains(l, "││") {
			t.Errorf("%s at %dx%d: abutting vertical borders:\n%s", layout, w, h, out)
			return
		}
	}
}

// Rendering is a pure function of the layout and panel size: no map iteration
// order leaks into the output.
func TestRender_Deterministic(t *testing.T) {
	g := mustParse(t, layoutDemo)
	first := render(g, 40, 12, nil, nil)
	for range 30 {
		if got := render(g, 40, 12, nil, nil); got != first {
			t.Fatalf("non-deterministic output:\nfirst:\n%s\nlater:\n%s", first, got)
		}
	}
}
