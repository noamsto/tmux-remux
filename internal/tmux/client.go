// Package tmux wraps shelling out to the tmux CLI.
package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

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

// Run executes the binary with the given args and returns stdout. Stderr
// is included in the error message on non-zero exit.
func (c *Client) Run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, c.binary, args...) //nolint:gosec // binary and args are trusted callers
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %v: %w (stderr: %s)", c.binary, args, err, stderr.String())
	}
	return stdout.String(), nil
}

const (
	sessionFormat = "#{session_name}" + FieldSep + "#{session_last_attached}"
	windowFormat  = "#{session_name}" + FieldSep + "#{window_index}" + FieldSep + "#{window_name}" + FieldSep + "#{window_layout}"
	paneFormat    = "#{session_name}" + FieldSep + "#{window_index}" + FieldSep + "#{pane_index}" + FieldSep + "#{pane_current_path}" + FieldSep + "#{pane_current_command}" + FieldSep + "#{pane_pid}" + FieldSep + "#{pane_last_used}"
)

// ListSessions runs `tmux list-sessions -F …` and parses the result.
// Returns nil (no error) when no sessions exist.
func (c *Client) ListSessions(ctx context.Context) ([]SessionRow, error) {
	out, err := c.Run(ctx, []string{"list-sessions", "-F", sessionFormat})
	if err != nil {
		// no sessions = exit 1; treat as empty
		return nil, nil //nolint:nilerr // tmux returns non-zero when no sessions exist; that is not an error for us
	}
	return ParseSessions(out)
}

// ListWindows runs `tmux list-windows -a -F …` and parses the result.
func (c *Client) ListWindows(ctx context.Context) ([]WindowRow, error) {
	out, err := c.Run(ctx, []string{"list-windows", "-a", "-F", windowFormat})
	if err != nil {
		return nil, nil //nolint:nilerr // tmux returns non-zero when no windows exist; that is not an error for us
	}
	return ParseWindows(out)
}

// ListPanes runs `tmux list-panes -a -F …` and parses the result.
func (c *Client) ListPanes(ctx context.Context) ([]PaneRow, error) {
	out, err := c.Run(ctx, []string{"list-panes", "-a", "-F", paneFormat})
	if err != nil {
		return nil, nil //nolint:nilerr // tmux returns non-zero when no panes exist; that is not an error for us
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
