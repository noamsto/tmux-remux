package restore_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/restore"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

var defaultOpts = restore.BuildOptions{
	Self:         "/usr/bin/tmux-state",
	DefaultShell: "/bin/zsh",
	AllowList:    []string{"nvim"},
}

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
	plan := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	want := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: "nvim"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b", StartupCommand: ""},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPlanWithScrollbackProducesCatThenExec(t *testing.T) {
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
	plan := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	wantStartup := `'/usr/bin/tmux-state' cat-scrollback deadbeef; exec nvim`
	for _, a := range plan {
		if cw, ok := a.(restore.CreateWindow); ok {
			if cw.StartupCommand != wantStartup {
				t.Errorf("CreateWindow.StartupCommand = %q, want %q", cw.StartupCommand, wantStartup)
			}
			return
		}
	}
	t.Fatal("CreateWindow not found in plan")
}

func TestBuildPlanScrollbackWithoutAllowedCommandUsesShell(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{
					{Index: 1, Cwd: "/a", Command: "bash", ChildCount: 2, ScrollbackSHA: "abc"},
				},
			}},
		}},
	}
	plan := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	wantStartup := `'/usr/bin/tmux-state' cat-scrollback abc; exec /bin/zsh`
	for _, a := range plan {
		if cw, ok := a.(restore.CreateWindow); ok {
			if cw.StartupCommand != wantStartup {
				t.Errorf("CreateWindow.StartupCommand = %q, want %q", cw.StartupCommand, wantStartup)
			}
			return
		}
	}
	t.Fatal("CreateWindow not found in plan")
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
	plan := restore.BuildPlan(m, f, nil, restore.BuildOptions{})
	for _, a := range plan {
		if sp, ok := a.(restore.SplitPane); ok && sp.Cwd == "/b" {
			t.Error("idle-shell pane should be filtered out")
		}
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
	plan := restore.BuildPlan(m, f, map[string]bool{"s1": true}, restore.BuildOptions{})
	for _, a := range plan {
		if cs, ok := a.(restore.CreateSession); ok && cs.Name == "s1" {
			t.Error("running session should be deduped")
		}
	}
}
