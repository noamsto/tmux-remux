package snapshot_test

import (
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
