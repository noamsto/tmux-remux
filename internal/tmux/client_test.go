package tmux_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/tmux-remux/internal/tmux"
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

// TestRunSynthesizesTmuxEnvWhenUnset verifies that when the parent has no
// TMUX env, Run sets one before exec. This is what prevents tmux 3.x from
// rewriting the \x1f field separator to "_" in -F output (see client.go
// comment on Run).
func TestRunSynthesizesTmuxEnvWhenUnset(t *testing.T) {
	parentTmux, hadParentTmux := os.LookupEnv("TMUX")
	t.Cleanup(func() {
		if hadParentTmux {
			_ = os.Setenv("TMUX", parentTmux)
		} else {
			_ = os.Unsetenv("TMUX")
		}
	})
	if err := os.Unsetenv("TMUX"); err != nil {
		t.Fatal(err)
	}

	fake := writeFakeTmux(t, `printf 'TMUX=%s\n' "$TMUX"`)
	c := tmux.NewClient(fake)
	out, err := c.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out)
	if !strings.HasPrefix(got, "TMUX=") || got == "TMUX=" {
		t.Fatalf("expected non-empty TMUX in subprocess env, got %q", got)
	}
	wantSuffix := fmt.Sprintf("/tmux-%d/default,0,0", os.Getuid())
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("got %q, want suffix %q", got, wantSuffix)
	}
}

// TestRunPreservesParentTmuxEnv confirms we don't clobber TMUX when the
// caller already sits inside a tmux client (hooks, keybindings).
func TestRunPreservesParentTmuxEnv(t *testing.T) {
	const sentinel = "/some/socket,1,2"
	t.Setenv("TMUX", sentinel)

	fake := writeFakeTmux(t, `printf '%s' "$TMUX"`)
	c := tmux.NewClient(fake)
	out, err := c.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != sentinel {
		t.Errorf("TMUX = %q, want %q (Run must not overwrite an existing value)", out, sentinel)
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

func TestWindowFormatWithDecoration(t *testing.T) {
	c := tmux.NewClient("tmux", "@crew_name", "@crew_color")
	got := c.WindowFormat()
	if !strings.HasSuffix(got, tmux.FieldSep+"#{@crew_name}"+tmux.FieldSep+"#{@crew_color}") {
		t.Errorf("format missing decoration fields: %q", got)
	}
}

func TestWindowFormatNoDecoration(t *testing.T) {
	c := tmux.NewClient("tmux")
	if strings.Contains(c.WindowFormat(), "#{@") {
		t.Errorf("unexpected decoration field in %q", c.WindowFormat())
	}
}

func TestServerStartTimeRejectsGarbage(t *testing.T) {
	fake := writeFakeTmux(t, `echo not-a-number`)
	c := tmux.NewClient(fake)
	if _, err := c.ServerStartTime(context.Background()); err == nil {
		t.Error("expected parse error for non-numeric start_time")
	}
}

func TestSetPaneOptionIssuesQuietPaneScopedArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := writeFakeTmux(t, fmt.Sprintf(`printf '%%s\n' "$@" > %s`, argsFile))
	c := tmux.NewClient(fake)

	if err := c.SetPaneOption(context.Background(), "%3", "@remux_relaunch", "claude --resume abc-123"); err != nil {
		t.Fatalf("SetPaneOption: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "set-option\n-pq\n-t\n%3\n@remux_relaunch\nclaude --resume abc-123\n"
	if string(got) != want {
		t.Errorf("tmux args =\n%q\nwant\n%q", got, want)
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
