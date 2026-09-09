// Package testutil provides helpers for end-to-end tests against a real tmux server.
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Server represents a tmux server isolated on its own socket.
type Server struct {
	Socket string
	t      *testing.T
}

// StartServer spawns a tmux server on a unique socket of its own. Sessions
// created by tests are isolated from any user session. The server is killed on
// test cleanup.
//
// The socket lives under /tmp rather than in t.TempDir() or under TMPDIR: a
// unix socket path is capped near 108 bytes, t.TempDir() spends 50 of them on
// the test's name, and TMPDIR is long on both a nix shell and macOS. Over the
// cap the server never binds and every test using it fails as "error
// connecting to ...: File name too long", which says nothing about what broke.
func StartServer(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "remux")
	if err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "sock")
	s := &Server{Socket: socket, t: t}
	t.Cleanup(s.Stop)
	if err := s.tmux("new-session", "-d", "-s", "init", "/bin/sh"); err != nil {
		t.Fatalf("start server: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := exec.Command("test", "-S", socket).Output(); err == nil { //nolint:gosec // socket path is test-controlled
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	return s
}

// Stop kills the tmux server. Safe to call multiple times.
func (s *Server) Stop() {
	_ = s.tmux("kill-server")
}

// Tmux runs `tmux -f /dev/null -u -S <socket> args...` and returns the combined
// output and any error. `-f /dev/null` skips user config (avoids stray restore
// plugins like continuum); `-u` forces UTF-8.
func (s *Server) Tmux(args ...string) (string, error) {
	full := append([]string{"-f", "/dev/null", "-u", "-S", s.Socket}, args...)
	cmd := exec.Command("tmux", full...) //nolint:gosec // socket and args are project-controlled
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (s *Server) tmux(args ...string) error {
	out, err := s.Tmux(args...)
	if err != nil {
		return fmt.Errorf("tmux %v: %w (%s)", args, err, out)
	}
	return nil
}
