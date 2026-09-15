package main

import (
	"fmt"
	"os"
	"testing"
)

// TestServerKeyFollowsTmuxEnv pins the production key derivation: the store
// partition must be the socket this process's tmux calls target. A constant
// here puts a hook's close events and a timer's snapshots in different lanes
// on one server, which is the bug this branch exists to fix.
func TestServerKeyFollowsTmuxEnv(t *testing.T) {
	t.Setenv("TMUX", "/run/user/1000/tmux-1000/default,123,4")
	if got := serverKey(); got != "/run/user/1000/tmux-1000/default" {
		t.Errorf("serverKey() = %q, want the socket from TMUX", got)
	}
}

func TestServerKeyFallsBackToDefaultSocket(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", "/run/user/1000")
	want := fmt.Sprintf("/run/user/1000/tmux-%d/default", os.Getuid())
	if got := serverKey(); got != want {
		t.Errorf("serverKey() = %q, want %q", got, want)
	}
}
