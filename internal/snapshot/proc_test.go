package snapshot_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

// reapedPID starts and waits out a trivial child process, returning its pid
// after it has already exited and been reaped — a pid that is guaranteed gone
// without relying on a fixed magic number.
func reapedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run true: %v", err)
	}
	return cmd.Process.Pid
}

func TestParentPIDSelf(t *testing.T) {
	got, err := snapshot.ParentPID(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if want := os.Getppid(); got != want {
		t.Errorf("ParentPID(self) = %d, want %d", got, want)
	}
}

func TestParentPIDGoneProcess(t *testing.T) {
	pid := reapedPID(t)
	if _, err := snapshot.ParentPID(pid); err == nil {
		t.Errorf("ParentPID(%d) = nil error, want error for a reaped pid", pid)
	}
}

func TestChildCountForSelfIsAtLeastZero(t *testing.T) {
	pid := os.Getpid()
	n, err := snapshot.ChildCount(pid)
	if err != nil {
		t.Fatal(err)
	}
	if n < 0 {
		t.Errorf("ChildCount = %d, want >= 0", n)
	}
}

func TestChildCountForBogusPIDIsZero(t *testing.T) {
	n, err := snapshot.ChildCount(2147483646)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("missing PID should return 0, got %d", n)
	}
}
