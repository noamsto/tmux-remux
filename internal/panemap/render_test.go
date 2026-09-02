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
func TestRender_StackedPanesShareDivider_Small(t *testing.T) {
	g := Grid{W: 80, H: 24, Panes: []Rect{
		{Index: 0, W: 80, H: 11, X: 0, Y: 0},
		{Index: 1, W: 80, H: 12, X: 0, Y: 12},
	}}
	got := render(g, 10, 8, nil, nil)
	lines := strings.Split(got, "\n")
	if len(lines) != 8 {
		t.Fatalf("got %d lines, want 8", len(lines))
	}
	// Find the divider row — the one with ├ or ┤. Exactly one such row must
	// exist; if two exist, borders weren't merged.
	dividers := 0
	for i, l := range lines {
		if strings.ContainsRune(l, '├') || strings.ContainsRune(l, '┤') {
			dividers++
			_ = i
		}
	}
	if dividers != 1 {
		t.Errorf("got %d divider rows, want 1:\n%s", dividers, got)
	}
}
