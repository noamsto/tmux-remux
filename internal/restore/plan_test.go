package restore_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/restore"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

func TestBuildPlanForFreshServer(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Name: "main", Layout: "L",
				Panes: []snapshot.Pane{
					{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1},
					{Index: 2, Cwd: "/b", Command: "bash", ChildCount: 2},
				},
			}},
		}},
	}
	plan := restore.BuildPlan(m, filter.Filter{}, nil, []string{"nvim"})
	want := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
		restore.RelaunchCommand{Pane: "s1:1.1", Command: "nvim"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPlanFiltersIdleShellPanes(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Name: "main", Layout: "L",
				Panes: []snapshot.Pane{
					{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1},
					{Index: 2, Cwd: "/b", Command: "bash", ChildCount: 0},
				},
			}},
		}},
	}
	f := filter.Filter{SkipIdleShells: true}
	plan := restore.BuildPlan(m, f, nil, nil)
	for _, a := range plan {
		if sp, ok := a.(restore.SplitPane); ok && sp.Cwd == "/b" {
			t.Error("idle-shell pane should be filtered out")
		}
	}
}

func TestBuildPlanScrollbackGating(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Name: "main", Layout: "L",
				Panes: []snapshot.Pane{
					{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1, ScrollbackSHA: "deadbeef"},
				},
			}},
		}},
	}
	off := restore.BuildPlan(m, filter.Filter{}, nil, nil)
	for _, a := range off {
		if _, ok := a.(restore.RestoreScrollback); ok {
			t.Errorf("scrollback restore must not be emitted when Filter.RestoreScrollback is false")
		}
	}
	on := restore.BuildPlan(m, filter.Filter{RestoreScrollback: true}, nil, nil)
	var found bool
	for _, a := range on {
		if _, ok := a.(restore.RestoreScrollback); ok {
			found = true
			break
		}
	}
	if !found {
		t.Error("scrollback restore must be emitted when Filter.RestoreScrollback is true")
	}
}

func TestBuildPlanFiltersDeduplicatedSessions(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{
			{Name: "s1", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim"}}}}},
			{Name: "s2", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1, Cwd: "/c", Command: "nvim"}}}}},
		},
	}
	f := filter.Filter{DedupRunningServer: true}
	plan := restore.BuildPlan(m, f, map[string]bool{"s1": true}, nil)
	for _, a := range plan {
		if cs, ok := a.(restore.CreateSession); ok && cs.Name == "s1" {
			t.Error("running session should be deduped")
		}
	}
}
