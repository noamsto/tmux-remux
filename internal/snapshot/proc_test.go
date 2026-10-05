package snapshot_test

import (
	"errors"
	"os"
	"testing"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

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

func TestProcInfoForSelf(t *testing.T) {
	pid := os.Getpid()
	p, err := snapshot.ProcInfo(pid)
	if err != nil {
		t.Fatal(err)
	}
	if p.PPID != os.Getppid() {
		t.Errorf("PPID = %d, want %d", p.PPID, os.Getppid())
	}
	if p.Start == 0 {
		t.Error("Start = 0, want a start time")
	}
	if p.Comm == "" {
		t.Error("Comm is empty")
	}
	again, err := snapshot.ProcInfo(pid)
	if err != nil {
		t.Fatal(err)
	}
	if again.Start != p.Start {
		t.Errorf("Start changed between calls: %d then %d", p.Start, again.Start)
	}
}

func TestProcInfoForBogusPIDIsNoProcess(t *testing.T) {
	_, err := snapshot.ProcInfo(2147483646)
	if !errors.Is(err, snapshot.ErrNoProcess) {
		t.Errorf("err = %v, want ErrNoProcess", err)
	}
}
