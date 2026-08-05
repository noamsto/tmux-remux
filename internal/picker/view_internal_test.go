package picker

import (
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// TestRenderList_NeverOverflowsFrame guards the list-pane box math: a lipgloss
// frame pads short content but does NOT clip overflow, so a wrapped row or
// footer pushes the border past the requested height and desyncs the sibling
// panes. Rendered output must be exactly width×height for every size — even a
// narrow pane with a long label and a hidden-count footer.
func TestRenderList_NeverOverflowsFrame(t *testing.T) {
	applyTheme(NewTheme())
	long := "reviewtest-session-with-a-fairly-long-path/main 🧠 (2p)"
	m := PickerModel{
		mode:         ModeClose,
		dimOlderThan: 24 * time.Hour,
		events: []store.Event{
			{ID: 1, Ts: time.Now().UnixMilli(), Kind: "window-unlinked"},
			{ID: 2, Ts: time.Now().UnixMilli(), Kind: "pane-died"},
		},
		closeContexts: map[int64]CloseContext{
			1: {Label: long, SubManifest: oneSession()},
			2: {Label: "pane: fish in mono/2", SubManifest: oneSession()},
		},
		hiddenCount: 14,
	}
	for _, w := range []int{32, 40, 80, 120} {
		for _, h := range []int{3, 4, 6, 10} {
			out := renderList(m, w, h)
			if got := lipgloss.Height(out); got != h {
				t.Errorf("renderList(w=%d,h=%d): height=%d, want %d\n%s", w, h, got, h, out)
			}
			if got := lipgloss.Width(out); got != w {
				t.Errorf("renderList(w=%d,h=%d): width=%d, want %d", w, h, got, w)
			}
		}
	}
}

func oneSession() snapshot.Manifest {
	return snapshot.Manifest{Sessions: []snapshot.Session{{Name: "s"}}}
}

// closeTreeFixture builds the tree used by the rendering tests: mono (current)
// with window 2 closed and a pane inside it, plus a gone lazytmux session.
func closeTreeFixture() *CloseNode {
	evs := []store.Event{
		{ID: 1, Ts: 300, Kind: "window-unlinked"},
		{ID: 2, Ts: 200, Kind: "pane-died"},
		{ID: 3, Ts: 100, Kind: "window-unlinked"},
	}
	one := snapshot.Manifest{Sessions: []snapshot.Session{{Name: "mono"}}}
	ctxs := map[int64]CloseContext{
		1: {Label: "w", Placement: ClosePlacement{Session: "mono", WindowIndex: 2, WindowName: "main", Scope: "window", PaneCount: 1}, SubManifest: one},
		2: {Label: "pane: nvim", Placement: ClosePlacement{Session: "mono", WindowIndex: 2, WindowName: "main", Scope: "pane"}, SubManifest: one},
		3: {Label: "w", Placement: ClosePlacement{Session: "lazytmux", WindowIndex: 3, WindowName: "docs", Scope: "window", PaneCount: 1}, SubManifest: one},
	}
	return BuildCloseTree(evs, ctxs, "mono", map[string]bool{"mono": true})
}

func TestCloseGuidePrefixes(t *testing.T) {
	root := closeTreeFixture()
	// Expand the other-sessions group so its subtree is visible.
	for _, g := range root.Children {
		g.Expanded = true
		for _, c := range g.Children {
			c.Expanded = true
		}
	}
	want := map[string]string{
		"this session · mono": "",
		"2: main (1p)":        "└─ ",
		"pane: nvim":          "   └─ ",
		"other sessions":      "",
		"lazytmux":            "└─ ",
		"3: docs (1p)":        "   └─ ",
	}
	for _, n := range FlattenClose(root) {
		exp, ok := want[n.Label]
		if !ok {
			t.Errorf("unexpected row %q", n.Label)
			continue
		}
		if got := closeGuidePrefix(n); got != exp {
			t.Errorf("prefix for %q = %q, want %q", n.Label, got, exp)
		}
	}
}

// The guide prefix is part of the row, so it must be inside the truncation
// budget: a deep row with a long label must not widen the frame.
func TestRenderCloseTree_NeverOverflowsFrame(t *testing.T) {
	applyTheme(NewTheme())
	root := closeTreeFixture()
	for _, g := range root.Children {
		g.Expanded = true
		for _, c := range g.Children {
			c.Expanded = true
			c.Label = "a-really-long-window-name-that-will-not-fit-in-any-narrow-pane (3p)"
		}
	}
	m := PickerModel{mode: ModeClose, closeTree: root}
	for _, size := range []struct{ w, h int }{{32, 8}, {40, 6}, {80, 12}, {28, 5}} {
		m.width, m.height = size.w, size.h
		out := renderCloseTree(m, size.w, size.h)
		if got := lipgloss.Width(out); got != size.w {
			t.Errorf("width %d height %d: rendered width %d, want %d", size.w, size.h, got, size.w)
		}
		if got := lipgloss.Height(out); got != size.h {
			t.Errorf("width %d height %d: rendered height %d, want %d", size.w, size.h, got, size.h)
		}
	}
}
