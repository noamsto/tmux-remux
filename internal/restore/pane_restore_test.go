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

	plan := restore.BuildPaneRestore(lost, win, "s1", true, defaultOpts)
	want := []restore.Action{
		restore.SplitPane{Target: "@7", Cwd: "/b", StartupCommand: ""},
		restore.SetLayout{Window: "@7", Layout: "LAY"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPaneRestoreTargetsSessionIndexWithoutWindowID(t *testing.T) {
	// Exercises the exported API's id-less target fallback directly. Live undo
	// never reaches it (windowLive can't match an empty id), but BuildPaneRestore
	// shouldn't depend on that caller invariant.
	win := paneRestoreWindow()
	win.ID = "" // legacy snapshot without window ids
	lost := win.Panes[0]

	plan := restore.BuildPaneRestore(lost, win, "s1", true, defaultOpts)
	want := []restore.Action{
		restore.SplitPane{Target: "s1:2", Cwd: "/a", StartupCommand: "nvim; exec /bin/zsh"},
		restore.SetLayout{Window: "s1:2", Layout: "LAY"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}

func TestBuildPaneRestoreRecreatesGoneWindow(t *testing.T) {
	win := paneRestoreWindow()
	lost := win.Panes[1]

	plan := restore.BuildPaneRestore(lost, win, "s1", false, defaultOpts)
	want := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 2, Name: "w", Cwd: "/a", StartupCommand: "nvim; exec /bin/zsh"},
		restore.SplitPane{Target: "s1:2", Cwd: "/b", StartupCommand: ""},
		restore.SetLayout{Window: "s1:2", Layout: "LAY"},
	}
	if diff := cmp.Diff(want, plan); diff != "" {
		t.Errorf("plan mismatch (-want +got):\n%s", diff)
	}
}
