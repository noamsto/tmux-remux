package restore_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/noamsto/tmux-state/internal/restore"
)

type recordingTmux struct {
	calls [][]string
}

func (r *recordingTmux) Run(_ context.Context, args []string) (string, error) {
	r.calls = append(r.calls, args)
	return "", nil
}

func TestApplyEmitsTmuxCallsWithoutStartup(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a"},
		restore.SplitPane{Target: "s1:1", Cwd: "/b"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	if err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-session", "-d", "-s", "s1", "-c", "/a"},
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a"},
		{"split-window", "-t", "s1:1", "-c", "/b"},
		{"select-layout", "-t", "s1:1", "L"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyAppendsStartupCommandWhenPresent(t *testing.T) {
	rt := &recordingTmux{}
	startup := `'/usr/bin/tmux-state' cat-scrollback abc; exec /bin/zsh`
	plan := []restore.Action{
		restore.CreateWindow{Session: "s1", Index: 1, Name: "main", Cwd: "/a", StartupCommand: startup},
		restore.SplitPane{Target: "s1:1", Cwd: "/b", StartupCommand: "htop"},
	}
	if err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"new-window", "-t", "s1:1", "-n", "main", "-c", "/a", startup},
		{"split-window", "-t", "s1:1", "-c", "/b", "htop"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyEmitsSetOptionPerWindowOptionSorted(t *testing.T) {
	rt := &recordingTmux{}
	plan := []restore.Action{
		restore.SetWindowOptions{Window: "s1:1", Options: map[string]string{
			"@pr_number": "42",
			"@branch":    "feat/x",
			"@issue_id":  "ENG-1",
		}},
	}
	if err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"set-option", "-t", "s1:1", "-w", "@branch", "feat/x"},
		{"set-option", "-t", "s1:1", "-w", "@issue_id", "ENG-1"},
		{"set-option", "-t", "s1:1", "-w", "@pr_number", "42"},
	}
	if diff := cmp.Diff(want, rt.calls); diff != "" {
		t.Errorf("calls mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyContinuesPastIndividualFailures(t *testing.T) {
	calls := 0
	failOn := 1
	rt := failingTmux{
		runFn: func(_ []string) (string, error) {
			calls++
			if calls == failOn+1 {
				return "", context.Canceled
			}
			return "", nil
		},
	}
	plan := []restore.Action{
		restore.CreateSession{Name: "s1", Cwd: "/a"},
		restore.CreateWindow{Session: "s1", Index: 1, Cwd: "/a"},
		restore.SetLayout{Window: "s1:1", Layout: "L"},
	}
	if err := restore.Apply(context.Background(), rt, plan); err != nil {
		t.Fatalf("Apply should swallow per-action errors, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 attempted calls (best-effort), got %d", calls)
	}
}

type failingTmux struct {
	runFn func(args []string) (string, error)
}

func (f failingTmux) Run(_ context.Context, args []string) (string, error) {
	return f.runFn(args)
}
