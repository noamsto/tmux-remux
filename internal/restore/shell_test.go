package restore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/noamsto/tmux-state/internal/restore"
)

type stubRunner struct {
	out string
	err error
}

func (s stubRunner) Run(_ context.Context, _ []string) (string, error) {
	return s.out, s.err
}

func TestDefaultShellPrefersTmuxOption(t *testing.T) {
	got, isBash := restore.DefaultShell(context.Background(), stubRunner{out: "/usr/bin/zsh\n"}, "")
	if got != "/usr/bin/zsh" {
		t.Errorf("path = %q, want /usr/bin/zsh", got)
	}
	if isBash {
		t.Error("isBash should be false for zsh")
	}
}

func TestDefaultShellDetectsBashByBasename(t *testing.T) {
	_, isBash := restore.DefaultShell(context.Background(), stubRunner{out: "/usr/bin/bash"}, "")
	if !isBash {
		t.Error("isBash should be true for bash")
	}
}

func TestDefaultShellFallsBackToShellEnv(t *testing.T) {
	got, _ := restore.DefaultShell(context.Background(), stubRunner{out: ""}, "/bin/fish")
	if got != "/bin/fish" {
		t.Errorf("path = %q, want /bin/fish (from $SHELL fallback)", got)
	}
}

func TestDefaultShellFallsBackToShWhenAllEmpty(t *testing.T) {
	got, _ := restore.DefaultShell(context.Background(), stubRunner{out: ""}, "")
	if got != "/bin/sh" {
		t.Errorf("path = %q, want /bin/sh", got)
	}
}

func TestDefaultShellSurvivesTmuxError(t *testing.T) {
	got, _ := restore.DefaultShell(context.Background(), stubRunner{err: errors.New("no server")}, "/bin/zsh")
	if got != "/bin/zsh" {
		t.Errorf("path = %q, want /bin/zsh (fallback after tmux error)", got)
	}
}
