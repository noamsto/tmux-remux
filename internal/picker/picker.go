// Package picker provides an fzf-based event picker.
package picker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/noamsto/tmux-state/internal/store"
)

// Item is one selectable row in the picker.
type Item struct {
	Key     string
	Display string
}

// Pick spawns the binary (typically "fzf") with stdin = rows joined by \n,
// and returns the selected key. Empty key = no selection (user cancelled).
func Pick(ctx context.Context, binary string, rows []Item) (string, error) {
	var input bytes.Buffer
	for _, r := range rows {
		fmt.Fprintf(&input, "%s\t%s\n", r.Key, r.Display)
	}

	cmd := exec.CommandContext(ctx, binary, "--with-nth", "2..", "--delimiter", "\t") //nolint:gosec // binary is project-controlled (defaults to fzf)
	cmd.Stdin = &input
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 130 {
			return "", nil
		}
		return "", fmt.Errorf("fzf: %w", err)
	}
	line := strings.TrimRight(out.String(), "\n")
	if line == "" {
		return "", nil
	}
	parts := strings.SplitN(line, "\t", 2)
	return parts[0], nil
}

// FormatRow renders an event as a tab-delimited fzf row: <id>\t<human-ts>  <kind>  <reason>
func FormatRow(ev store.Event) string {
	t := time.UnixMilli(ev.Ts).Format("2006-01-02 15:04:05")
	return fmt.Sprintf("%d\t%s  %-15s  %s", ev.ID, t, ev.Kind, ev.Reason)
}
