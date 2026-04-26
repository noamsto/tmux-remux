package restore_test

import (
	"context"
	"fmt"
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

type sbReader struct{ data map[string][]byte }

func (s *sbReader) Get(_ context.Context, sha string) ([]byte, error) {
	v, ok := s.data[sha]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func TestApplyPastesScrollback(t *testing.T) {
	rt := &recordingTmux{}
	sb := &sbReader{data: map[string][]byte{"abc": []byte("history\n")}}
	plan := []restore.Action{
		restore.RestoreScrollback{Pane: "s1:1.1", SHA: "abc"},
	}
	if err := restore.ApplyWithScrollback(context.Background(), rt, sb, plan); err != nil {
		t.Fatal(err)
	}
	if len(rt.calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %v", len(rt.calls), rt.calls)
	}
}
