package picker_test

import (
	"testing"

	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

func TestBuildTree_TwoSessionsTwoWindowsTwoPanes(t *testing.T) {
	m := snapshot.Manifest{
		V:    1,
		Host: "h",
		Sessions: []snapshot.Session{
			{
				Name: "lazytmux",
				Windows: []snapshot.Window{
					{
						Index: 0, Name: "claude",
						Panes: []snapshot.Pane{
							{Index: 0, Cwd: "/home/u/lazytmux", Command: "zsh"},
						},
					},
				},
			},
			{
				Name: "nix-config",
				Windows: []snapshot.Window{
					{
						Index: 0, Name: "shell",
						Panes: []snapshot.Pane{
							{Index: 0, Cwd: "/home/u/nix-config", Command: "fish"},
						},
					},
				},
			},
		},
	}

	root := picker.BuildTree(m)
	if root == nil {
		t.Fatal("BuildTree returned nil")
	}
	if got := len(root.Children); got != 2 {
		t.Fatalf("root.Children = %d, want 2", got)
	}

	s0 := root.Children[0]
	if s0.Kind != picker.NodeSession || s0.Label != "lazytmux (1w)" {
		t.Errorf("session 0: kind=%v label=%q", s0.Kind, s0.Label)
	}
	if !s0.Expanded {
		t.Error("session 0: want Expanded=true by default")
	}

	w := s0.Children[0]
	if w.Kind != picker.NodeWindow || w.Label != "0: claude (1p)" {
		t.Errorf("window: kind=%v label=%q", w.Kind, w.Label)
	}
	if !w.Expanded {
		t.Error("window: want Expanded=true by default")
	}

	p := w.Children[0]
	if p.Kind != picker.NodePane {
		t.Errorf("pane: kind=%v", p.Kind)
	}
	if p.Expanded {
		t.Error("pane: want Expanded=false by default")
	}
	// Pane label format: "zsh    ~/lazytmux" (HOME-relative via shellexpand).
	// Exact home-relative formatting is asserted in a focused test below.
}
