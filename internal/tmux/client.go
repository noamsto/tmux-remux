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
