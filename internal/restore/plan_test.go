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

func TestBuildPlanRelaunchOverrideBypassesAllowList(t *testing.T) {
	// A pane whose command is not allow-listed still relaunches via its
	// @ts_relaunch override, emitted verbatim after the scrollback step.
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{{
			Name: "s1",
			Windows: []snapshot.Window{{
				Index: 1, Name: "main", Layout: "L",
				Panes: []snapshot.Pane{
					{Index: 1, Cwd: "/a", Command: "claude", ChildCount: 1, ScrollbackSHA: "deadbeef", Relaunch: "claude --resume abc-123"},
				},
			}},
		}},
	}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, defaultOpts)
	wantStartup := `'/usr/bin/tmux-state' cat-scrollback deadbeef; exec claude --resume abc-123`
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

func TestBuildPlanEmitsSortedSetOptions(t *testing.T) {
	m := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Name: "s", Windows: []snapshot.Window{{
			Index: 1, Layout: "L",
			Decoration: map[string]string{"@crew_color": "colour141", "@crew_name": "dispatcher"},
			Panes:      []snapshot.Pane{{Index: 0, Cwd: "/tmp", Command: "bash"}},
		}},
	}}}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{DefaultShell: "/bin/sh"})

	var sets []restore.SetOption
	var setIdx, layoutIdx int
	for i, a := range plan {
		if so, ok := a.(restore.SetOption); ok {
			sets = append(sets, so)
			setIdx = i
		}
		if _, ok := a.(restore.SetLayout); ok {
			layoutIdx = i
		}
	}
	if len(sets) != 2 {
		t.Fatalf("got %d SetOption, want 2", len(sets))
	}
	// sorted by name: @crew_color before @crew_name
	if sets[0].Name != "@crew_color" || sets[1].Name != "@crew_name" {
		t.Errorf("SetOption not sorted by name: %+v", sets)
	}
	if sets[0].Target != "s:1" || sets[0].Value != "colour141" || sets[0].Pane {
		t.Errorf("unexpected SetOption: %+v", sets[0])
	}
	if setIdx > layoutIdx {
		t.Error("SetOption emitted after SetLayout; want before")
	}
}

func TestBuildPlanNoDecorationNoSetOption(t *testing.T) {
	m := snapshot.Manifest{V: 1, Sessions: []snapshot.Session{{
		Name: "s", Windows: []snapshot.Window{{
			Index: 1, Layout: "L",
			Panes: []snapshot.Pane{{Index: 0, Cwd: "/tmp", Command: "bash"}},
		}},
	}}}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, restore.BuildOptions{DefaultShell: "/bin/sh"})
	for _, a := range plan {
		if _, ok := a.(restore.SetOption); ok {
			t.Error("unexpected SetOption for window with no decoration")
		}
	}
}
