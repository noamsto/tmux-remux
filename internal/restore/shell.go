package restore

import (
	"context"
	"path/filepath"
	"strings"
)

// DefaultShell resolves the user's preferred login shell for restored panes.
//
// Resolution order:
//  1. tmux's own default-shell option (`tmux show-option -gqv default-shell`)
//  2. shellEnv (caller passes os.Getenv("SHELL"))
//  3. /bin/sh
//
// Returns the resolved path and whether its basename is "bash" (so callers
// can prepend -l to the relaunch arg list, matching tmux-resurrect behavior).
func DefaultShell(ctx context.Context, t Runner, shellEnv string) (string, bool) {
	path := ""
	if out, err := t.Run(ctx, []string{"show-option", "-gqv", "default-shell"}); err == nil {
		path = strings.TrimSpace(out)
	}
	if path == "" {
		path = strings.TrimSpace(shellEnv)
	}
	if path == "" {
		path = "/bin/sh"
	}
	return path, filepath.Base(path) == "bash"
}
