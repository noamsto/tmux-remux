package restore_test

import (
	"testing"
	"time"

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
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
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
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
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
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
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
	plan, _ := restore.BuildPlan(m, f, nil, restore.BuildOptions{})
	for _, a := range plan {
		if sp, ok := a.(restore.SplitPane); ok && sp.Cwd == "/b" {
			t.Error("idle-shell pane should be filtered out")
		}
	}
}

func TestBuildPlanSkipsRunningSessions(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{
			{Name: "s1", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim"}}}}},
			{Name: "s2", Windows: []snapshot.Window{{Index: 1, Panes: []snapshot.Pane{{Index: 1, Cwd: "/c", Command: "nvim"}}}}},
		},
	}
	f := filter.Filter{SkipRunningSessions: true}
	plan, _ := restore.BuildPlan(m, f, map[string]bool{"s1": true}, restore.BuildOptions{})
	for _, a := range plan {
		if cs, ok := a.(restore.CreateSession); ok && cs.Name == "s1" {
			t.Error("running session should be skipped")
		}
	}
}

func TestBuildPlanEmitsWindowOptionsForFirstAndLaterWindows(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{
				{
					Index: 1, Name: "main", Layout: "L",
					Options: map[string]string{"@branch": "feat/x"},
					Panes:   []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1}},
				},
				{
					Index: 2, Name: "build", Layout: "L",
					Options: map[string]string{"@branch": "feat/y", "@pr_number": "42"},
					Panes:   []snapshot.Pane{{Index: 1, Cwd: "/b", Command: "nvim", ChildCount: 1}},
				},
			},
		}},
	}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	want := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: "nvim"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
		restore.SetWindowOptions{Window: "s1:1", Options: map[string]string{"@branch": "feat/x"}},
		restore.CreateWindow{Session: "s1", Index: 2, Name: "build", Cwd: "/b", StartupCommand: "nvim"},
		restore.SetLayout{Window: "s1:2", Layout: "L"},
		restore.SetWindowOptions{Window: "s1:2", Options: map[string]string{"@branch": "feat/y", "@pr_number": "42"}},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPlanOmitsWindowOptionsWhenEmpty(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1}},
			}},
		}},
	}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	for _, a := range plan {
		if _, ok := a.(restore.SetWindowOptions); ok {
			t.Error("no SetWindowOptions action should be emitted for a window with no options")
		}
	}
}

func TestBuildPlanStatsCountsKeptAndSkipped(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{
			{Name: "kept", LastAttached: now.Unix() - 60, Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1}},
			}}},
			{Name: "running", LastAttached: now.Unix() - 60, Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/b", Command: "nvim", ChildCount: 1}},
			}}},
			{Name: "idle", LastAttached: now.Unix() - 60, Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/c", Command: "fish", ChildCount: 0}},
			}}},
			{Name: "stale", LastAttached: now.Unix() - 7200, Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/d", Command: "nvim", ChildCount: 1}},
			}}},
		},
	}
	f := filter.Filter{
		Now:                 now,
		MaxSessionAge:       time.Hour,
		SkipIdleShells:      true,
		SkipIdleWindows:     true,
		SkipRunningSessions: true,
	}
	running := map[string]bool{"running": true}

	_, stats := restore.BuildPlan(m, f, running, defaultOpts)
	want := restore.PlanStats{
		SessionsKept:           1,
		SessionsSkippedRunning: 1,
		SessionsSkippedStale:   1,
		SessionsSkippedIdle:    1,
		WindowsSkippedIdle:     1,
	}
	if diff := cmp.Diff(want, stats); diff != "" {
		t.Errorf("stats mismatch (-want +got):\n%s", diff)
	}
}
