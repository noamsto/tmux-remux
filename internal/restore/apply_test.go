package restore_test

import (
	"context"
	"testing"

	"github.com/noamsto/tmux-state/internal/restore"
)

type recordingTmux struct {
	calls [][]string
}

func (r *recordingTmux) Run(_ context.Context, args []string) (string, error) {
	r.calls = append(r.calls, args)
	return "", nil
}

func TestApplyEmitsCorrectTmuxCalls(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
		restore.RelaunchCommand{Pane: "s1:1.1", Command: "nvim", Args: []string{"file.go"}},
	}
	if err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	wantArgs0 := []string{"new-session", "-d", "-s", "s1", "-c", "/a"}
	if !equalArgs(rt.calls[0], wantArgs0) {
		t.Errorf("call 0: %v, want %v", rt.calls[0], wantArgs0)
	}
	wantArgs1 := []string{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a"}
	if !equalArgs(rt.calls[1], wantArgs1) {
		t.Errorf("call 1: %v, want %v", rt.calls[1], wantArgs1)
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
