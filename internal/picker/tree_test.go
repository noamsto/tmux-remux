package picker_test

import (
	"os"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/filter"
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

func TestBuildTree_PaneLabel_HomeRelativeCwd(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no HOME")
	}
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Name:  "w",
				Panes: []snapshot.Pane{{Cwd: home + "/work", Command: "fish"}},
			}},
		}},
	}
	root := picker.BuildTree(m)
	got := root.Children[0].Children[0].Children[0].Label
	if !strings.Contains(got, "~/work") {
		t.Errorf("pane label = %q, want it to contain ~/work", got)
	}
}

func TestFilterDecorate_NoToggles_AllKept(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Name: "w",
				Panes: []snapshot.Pane{
					{Index: 0, Command: "fish"},
					{Index: 1, Command: "nvim", ChildCount: 1},
				},
			}},
		}},
	}
	root := picker.BuildTree(m)
	counts := picker.FilterDecorate(root, filter.Filter{}, nil)

	if counts.KeptPanes != 2 || counts.SkippedPanes != 0 {
		t.Errorf("counts = %+v, want kept=2 skipped=0", counts)
	}
	for _, sess := range root.Children {
		if sess.Skipped {
			t.Errorf("session %q skipped with default filter", sess.Label)
		}
		for _, w := range sess.Children {
			if w.Skipped {
				t.Errorf("window %q skipped with default filter", w.Label)
			}
			for _, p := range w.Children {
				if p.Skipped {
					t.Errorf("pane %q skipped with default filter", p.Label)
				}
			}
		}
	}
}

func TestFilterDecorate_Table(t *testing.T) {
	idleShell := snapshot.Pane{Index: 0, Command: "fish", ChildCount: 0}
	busyShell := snapshot.Pane{Index: 0, Command: "fish", ChildCount: 2}
	nvim := snapshot.Pane{Index: 0, Command: "nvim", ChildCount: 0}

	mk := func(panes ...snapshot.Pane) snapshot.Manifest {
		return snapshot.Manifest{
			Sessions: []snapshot.Session{{
				Name:    "s",
				Windows: []snapshot.Window{{Name: "w", Panes: panes}},
			}},
		}
	}

	tests := []struct {
		name             string
		manifest         snapshot.Manifest
		filter           filter.Filter
		running          map[string]bool
		wantKeptPanes    int
		wantSkippedPanes int
	}{
		{
			name:             "no toggles → all kept",
			manifest:         mk(idleShell, nvim),
			filter:           filter.Filter{},
			wantKeptPanes:    2,
			wantSkippedPanes: 0,
		},
		{
			name:             "skip idle shells drops the idle fish pane",
			manifest:         mk(idleShell, nvim),
			filter:           filter.Filter{SkipIdleShells: true},
			wantKeptPanes:    1,
			wantSkippedPanes: 1,
		},
		{
			name:             "skip idle shells: busy fish (children>0) stays",
			manifest:         mk(busyShell, nvim),
			filter:           filter.Filter{SkipIdleShells: true},
			wantKeptPanes:    2,
			wantSkippedPanes: 0,
		},
		{
			name:             "dedup running drops the whole session",
			manifest:         mk(nvim),
			filter:           filter.Filter{DedupRunningServer: true},
			running:          map[string]bool{"s": true},
			wantKeptPanes:    0,
			wantSkippedPanes: 1,
		},
		{
			name:             "dedup running with bool off but matching name → kept (toggle is the gate)",
			manifest:         mk(nvim),
			filter:           filter.Filter{},
			running:          map[string]bool{"s": true},
			wantKeptPanes:    1,
			wantSkippedPanes: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := picker.BuildTree(tc.manifest)
			c := picker.FilterDecorate(root, tc.filter, tc.running)
			if c.KeptPanes != tc.wantKeptPanes || c.SkippedPanes != tc.wantSkippedPanes {
				t.Errorf("counts = kept=%d skipped=%d, want kept=%d skipped=%d",
					c.KeptPanes, c.SkippedPanes, tc.wantKeptPanes, tc.wantSkippedPanes)
			}
		})
	}
}
