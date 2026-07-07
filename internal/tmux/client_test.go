package tmux_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/tmux"
)

func TestRunReturnsStdoutTrimmed(t *testing.T) {
	c := tmux.NewClient("echo")
	out, err := c.Run(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimRight(out, "\n"); got != "hello" {
		t.Errorf("Run = %q, want \"hello\"", got)
	}
}

func TestRunReturnsErrorOnNonZero(t *testing.T) {
	c := tmux.NewClient("false")
	_, err := c.Run(context.Background(), nil)
	if err == nil {
		t.Error("expected error for non-zero exit")
	}
}

// TestRunMapsNoServerStderrToSentinel uses a tiny fake tmux script that
// writes a "no server running" message and exits non-zero, the way real tmux
// behaves when its socket file is gone. Run must surface ErrNoServer so
// callers can branch on errors.Is without parsing stderr themselves.
func TestRunMapsNoServerStderrToSentinel(t *testing.T) {
	fake := writeFakeTmux(t, `>&2 echo "no server running on /tmp/tmux-1000/default"; exit 1`)
	c := tmux.NewClient(fake)
	_, err := c.Run(context.Background(), []string{"list-sessions"})
	if !errors.Is(err, tmux.ErrNoServer) {
		t.Errorf("got %v, want ErrNoServer", err)
	}
}

// TestRunMapsErrorConnectingToSentinel covers the second phrasing tmux emits
// when the socket file does not exist at all (vs. exists-but-dead-server).
func TestRunMapsErrorConnectingToSentinel(t *testing.T) {
	fake := writeFakeTmux(t, `>&2 echo "error connecting to /tmp/tmux-1000/default (No such file or directory)"; exit 1`)
	c := tmux.NewClient(fake)
	_, err := c.Run(context.Background(), []string{"list-sessions"})
	if !errors.Is(err, tmux.ErrNoServer) {
		t.Errorf("got %v, want ErrNoServer", err)
	}
}

// TestRunPropagatesOtherStderr ensures real tmux errors (bad flag, unknown
// command, etc.) are NOT swallowed as ErrNoServer.
func TestRunPropagatesOtherStderr(t *testing.T) {
	fake := writeFakeTmux(t, `>&2 echo "unknown command: bogus"; exit 1`)
	c := tmux.NewClient(fake)
	_, err := c.Run(context.Background(), []string{"bogus"})
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, tmux.ErrNoServer) {
		t.Errorf("got ErrNoServer, want generic error (stderr was %q)", err)
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error %v does not include stderr", err)
	}
}

// TestServerStartTimeParsesSecondsToMillis verifies #{start_time} (epoch
// seconds) is converted to the millisecond scale used by events.ts.
func TestServerStartTimeParsesSecondsToMillis(t *testing.T) {
	fake := writeFakeTmux(t, `echo 1780811351`)
	c := tmux.NewClient(fake)
	got, err := c.ServerStartTime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != 1780811351000 {
		t.Errorf("ServerStartTime = %d, want 1780811351000", got)
	}
}

func TestServerStartTimeRejectsGarbage(t *testing.T) {
	fake := writeFakeTmux(t, `echo not-a-number`)
	c := tmux.NewClient(fake)
	if _, err := c.ServerStartTime(context.Background()); err == nil {
		t.Error("expected parse error for non-numeric start_time")
	}
}

// writeFakeTmux drops a tiny bash script in t.TempDir whose body is `body`
// and returns its path so tests can use it as the Client binary.
func writeFakeTmux(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-tmux")
	script := "#!/usr/bin/env bash\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
