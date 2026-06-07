// Package tmux wraps shelling out to the tmux CLI.
package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNoServer is returned by Run (and surfaced via ListSessions/Windows/Panes
// as a nil result with no error) when tmux reports that no server is running
// on the target socket. Two phrasings exist in tmux 3.x:
//
//   - "no server running on /path"        socket file exists but no server
//   - "error connecting to /path (...)"  socket file does not exist
//
// Callers that want to handle "no server" as an error rather than empty state
// can match with errors.Is.
var ErrNoServer = errors.New("tmux: no server running")

// Client invokes a tmux-compatible binary. Defaults to the "tmux" command
// when binary is empty.
type Client struct {
	binary string
}

// NewClient returns a Client that invokes binary; if empty, "tmux" is used.
func NewClient(binary string) *Client {
	if binary == "" {
		binary = "tmux"
	}
	return &Client{binary: binary}
}

// Run executes the binary with the given args and returns stdout. Non-zero
// exit with a "no server" stderr is mapped to ErrNoServer; other failures are
// wrapped with stderr.
//
// When TMUX is not set in the calling environment, Run synthesizes a TMUX
// value pointing at the default socket. tmux 3.x rewrites control bytes
// (0x01-0x1f) in -F format output to "_" when invoked outside a client,
// which silently corrupts the \x1f field separator used by ParseSessions and
// friends. Setting TMUX to any non-empty value with a real socket path makes
// tmux preserve those bytes. See parse.go FieldSep.
func (c *Client) Run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, c.binary, args...) //nolint:gosec // binary and args are trusted callers
	cmd.Env = withSynthesizedTmuxEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if isNoServerStderr(msg) {
			return "", ErrNoServer
		}
		return "", fmt.Errorf("%s %v: %w (stderr: %s)", c.binary, args, err, msg)
	}
	return stdout.String(), nil
}

func isNoServerStderr(s string) bool {
	return strings.Contains(s, "no server running on ") ||
		strings.Contains(s, "error connecting to ")
}

// withSynthesizedTmuxEnv returns env unchanged when TMUX is already set,
// otherwise appends a synthesized TMUX=<socket>,0,0 entry. The socket path
// follows tmux's own default-socket logic: $TMUX_TMPDIR/tmux-<UID>/default,
// with /tmp as the TMUX_TMPDIR fallback. The pid/session-id components are
// dummies — tmux only checks that TMUX is non-empty and that the socket path
// resolves to a running server.
func withSynthesizedTmuxEnv(env []string) []string {
	tmpdir := ""
	for _, e := range env {
		if strings.HasPrefix(e, "TMUX=") {
			return env
		}
		if v, ok := strings.CutPrefix(e, "TMUX_TMPDIR="); ok {
			tmpdir = v
		}
	}
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	return append(env, fmt.Sprintf("TMUX=%s/tmux-%d/default,0,0", tmpdir, os.Getuid()))
}

const (
	sessionFormat = "#{session_name}" + FieldSep + "#{session_last_attached}"
	windowFormat  = "#{session_name}" + FieldSep + "#{window_index}" + FieldSep + "#{window_name}" + FieldSep + "#{window_layout}" + FieldSep + "#{window_id}"
	paneFormat    = "#{session_name}" + FieldSep + "#{window_index}" + FieldSep + "#{pane_index}" + FieldSep + "#{pane_current_path}" + FieldSep + "#{pane_current_command}" + FieldSep + "#{pane_pid}" + FieldSep + "#{pane_last_used}" + FieldSep + "#{pane_id}"
)

// ListSessions runs `tmux list-sessions -F …` and parses the result.
// Returns (nil, nil) when no tmux server is running; propagates other errors.
func (c *Client) ListSessions(ctx context.Context) ([]SessionRow, error) {
	out, err := c.Run(ctx, []string{"list-sessions", "-F", sessionFormat})
	if errors.Is(err, ErrNoServer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSessions(out)
}

// ListWindows runs `tmux list-windows -a -F …` and parses the result.
// Returns (nil, nil) when no tmux server is running; propagates other errors.
func (c *Client) ListWindows(ctx context.Context) ([]WindowRow, error) {
	out, err := c.Run(ctx, []string{"list-windows", "-a", "-F", windowFormat})
	if errors.Is(err, ErrNoServer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseWindows(out)
}

// ListPanes runs `tmux list-panes -a -F …` and parses the result.
// Returns (nil, nil) when no tmux server is running; propagates other errors.
func (c *Client) ListPanes(ctx context.Context) ([]PaneRow, error) {
	out, err := c.Run(ctx, []string{"list-panes", "-a", "-F", paneFormat})
	if errors.Is(err, ErrNoServer) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParsePanes(out)
}

// CapturePane returns the scrollback contents of a pane as raw bytes.
// target format: <session>:<window_index>.<pane_index>
func (c *Client) CapturePane(ctx context.Context, target string) ([]byte, error) {
	out, err := c.Run(ctx, []string{"capture-pane", "-pJ", "-t", target, "-S", "-"})
	if err != nil {
		return nil, fmt.Errorf("capture-pane %q: %w", target, err)
	}
	return []byte(out), nil
}
