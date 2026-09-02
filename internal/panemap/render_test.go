package panemap

import (
	"strings"
	"testing"
)

// One pane filling the window is one box filling the panel.
func TestRender_SinglePane(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{{Index: 0, W: 80, H: 24}}}
	got := render(g, 10, 4, nil, nil)
	want := strings.Join([]string{
		"┌────────┐",
		"│        │",
		"│        │",
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Two panes stacked share one divider row, drawn with ├ ┤ junctions rather
// than two adjacent borders.
func TestRender_StackedPanesShareDivider(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 80, H: 11, X: 0, Y: 0},
		{Index: 1, W: 80, H: 12, X: 0, Y: 12},
	}}
	got := render(g, 10, 5, nil, nil)
	want := strings.Join([]string{
		"┌────────┐",
		"│        │",
		"├────────┤",
		"│        │",
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Proportions survive scaling: a 70/30 vertical split stays 70/30.
func TestRender_KeepsProportions(t *testing.T) {
	g := Grid{W: 100, H: 20, Panes: []Rect{
		{Index: 0, W: 69, H: 20, X: 0, Y: 0},
		{Index: 1, W: 30, H: 20, X: 70, Y: 0},
	}}
	lines := strings.Split(render(g, 21, 3, nil, nil), "\n")
	divider := strings.IndexRune(lines[1], '│')
	second := strings.IndexRune(lines[1][divider+1:], '│')
	if divider != 0 {
		t.Fatalf("left edge at %d, want 0", divider)
	}
	// The middle divider should sit near 70% of 20 interior columns, not 50%.
	if mid := divider + 1 + second; mid < 12 || mid > 16 {
		t.Errorf("middle divider at column %d, want ~14 (70%%)", mid)
	}
}

// Below the minimum the art would be noise, so a one-line summary replaces it.
func TestRender_TooSmallFallsBackToSummary(t *testing.T) {
	g := Grid{W: 145, H: 36, Panes: []Rect{
		{Index: 0, W: 145, H: 18}, {Index: 1, W: 72, H: 17}, {Index: 2, W: 72, H: 17},
	}}
	got := Render(g, minMapWidth-1, minMapHeight, nil, nil)
	if want := "3 panes · 145×36"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Same geometry as TestRender_StackedPanesShareDivider but at h=8, which
// exposes the per-boundary rounding mismatch the deduplicated scaler fixes.
func TestRender_StackedPanesShareDivider_AtH8(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 80, H: 11, X: 0, Y: 0},
		{Index: 1, W: 80, H: 12, X: 0, Y: 12},
	}}
	got := render(g, 10, 8, nil, nil)
	blank := "│        │"
	want := strings.Join([]string{
		"┌────────┐",
		blank, blank, blank,
		"├────────┤",
		blank, blank,
		"└────────┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A 1-column pane sits with its own two edges only 1 apart from its
// neighbours' — merging every gap-1 boundary would erase it.
func TestRender_OneColumnPaneKeepsItsWidth(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 39, H: 24, X: 0, Y: 0},
		{Index: 1, W: 1, H: 24, X: 40, Y: 0},
		{Index: 2, W: 38, H: 24, X: 42, Y: 0},
	}}
	got := render(g, 81, 8, nil, nil)
	var joints []int
	for i, r := range []rune(strings.Split(got, "\n")[0]) {
		if r == '┬' {
			joints = append(joints, i)
		}
	}
	if len(joints) != 2 {
		t.Fatalf("got %d interior dividers at %v, want 2:\n%s", len(joints), joints, got)
	}
	if joints[1]-joints[0] < 2 {
		t.Errorf("dividers at %v abut, so pane 1 has no width:\n%s", joints, got)
	}
}

// Two columns split at different heights chain their divider boundaries — 10-11
// from the left column, 11-12 from the right — so a pairwise merge leaves each
// column drawing its own divider row.
func TestRender_ChainedDividersShareOneRow(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 40, H: 10, X: 0, Y: 0},
		{Index: 1, W: 40, H: 13, X: 0, Y: 11},
		{Index: 2, W: 39, H: 11, X: 41, Y: 0},
		{Index: 3, W: 39, H: 12, X: 41, Y: 12},
	}}
	got := render(g, 41, 8, nil, nil)
	left, right := strings.Repeat("─", 20), strings.Repeat("─", 18)
	blank := "│" + strings.Repeat(" ", 20) + "│" + strings.Repeat(" ", 18) + "│"
	want := strings.Join([]string{
		"┌" + left + "┬" + right + "┐",
		blank, blank, blank,
		"├" + left + "┼" + right + "┤",
		blank, blank,
		"└" + left + "┴" + right + "┘",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Six stacked panes need seven distinct rows, which a 6-row panel cannot give.
// Drawing one of them with no height would lie about the layout, so Render
// falls back to the summary even though the panel clears the size guard.
func TestRender_UndrawableFallsBackToSummary(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 80, H: 3, X: 0, Y: 0},
		{Index: 1, W: 80, H: 3, X: 0, Y: 4},
		{Index: 2, W: 80, H: 3, X: 0, Y: 8},
		{Index: 3, W: 80, H: 3, X: 0, Y: 12},
		{Index: 4, W: 80, H: 3, X: 0, Y: 16},
		{Index: 5, W: 80, H: 4, X: 0, Y: 20},
	}}
	got := Render(g, minMapWidth, minMapHeight, nil, nil)
	if want := summary(g); got != want {
		t.Errorf("got:\n%s\nwant %q", got, want)
	}
}
