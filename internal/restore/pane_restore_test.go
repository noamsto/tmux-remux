package restore_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-remux/internal/restore"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

func paneRestoreWindow() snapshot.Window {
	return snapshot.Window{
		Index: 2, Name: "w", Layout: "LAY", ID: "@7",
		Panes: []snapshot.Pane{
			{Index: 1, Cwd: "/a", Command: "nvim"},
			{Index: 2, Cwd: "/b", Command: "bash"},
		},
	}
}

func TestBuildPaneRestoreSplitsIntoLiveWindow(t *testing.T) {
	win := paneRestoreWindow()
	lost := win.Panes[1] // the bash pane that died

	plan := restore.BuildPaneRestore(lost, win, "s1", "@7", defaultOpts)
	want := []restore.Action{
		restore.SplitPane{Target: "@7", Cwd: "/b", StartupCommand: ""},
		restore.SetLayout{Window: "@7", Layout: "LAY"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPaneRestoreRecreatesGoneWindow(t *testing.T) {
	win := paneRestoreWindow()
	lost := win.Panes[1]

	plan := restore.BuildPaneRestore(lost, win, "s1", "", defaultOpts)
	want := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 2, Name: "w", Cwd: "/a", StartupCommand: "nvim; exec /bin/zsh", NewSession: true},
		restore.SplitPane{Target: "s1:2", Cwd: "/b", StartupCommand: ""},
		restore.SetLayout{Window: "s1:2", Layout: "LAY"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

// The caller resolves the live window, so BuildPaneRestore splits into exactly
// the target it is handed — including a window id that differs from the
// snapshot's, which is what a re-created window has.
func TestBuildPaneRestoreSplitsIntoResolvedTarget(t *testing.T) {
	lost := snapshot.Pane{Index: 2, Cwd: "/b"}
	win := snapshot.Window{Index: 3, Name: "docs", Layout: "L", ID: "@9"}

	plan := restore.BuildPaneRestore(lost, win, "mono", "@42", restore.BuildOptions{})

	want := []restore.Action{
		restore.SplitPane{Target: "@42", Cwd: "/b"},
		restore.SetLayout{Window: "@42", Layout: "L"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPaneRestoreRecreatesWindowWhenNoLiveTarget(t *testing.T) {
	win := snapshot.Window{Index: 3, Name: "docs", Layout: "L", ID: "@9",
		Panes: []snapshot.Pane{{Index: 1, Cwd: "/a"}, {Index: 2, Cwd: "/b"}}}
	lost := win.Panes[1]

	plan := restore.BuildPaneRestore(lost, win, "mono", "", restore.BuildOptions{})

	if len(plan) == 0 {
		t.Fatal("expected a window-recreating plan, got none")
	}
	cw, ok := plan[0].(restore.CreateWindow)
	if !ok {
		t.Fatalf("plan[0] = %T, want restore.CreateWindow", plan[0])
	}
	if cw.Session != "mono" || cw.Index != 3 {
		t.Errorf("CreateWindow = %+v, want session mono index 3", cw)
	}

	// The recreated window must bring the lost pane back too, not just its
	// surviving sibling — BuildPlan emits it as a SplitPane onto the new window.
	want := restore.SplitPane{Target: "mono:3", Cwd: lost.Cwd}
	found := false
	for _, a := range plan {
		if sp, ok := a.(restore.SplitPane); ok && sp == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plan = %+v, want it to contain the lost pane's restore action %+v", plan, want)
	}
}
