package restore

import (
	"context"
	"fmt"
)

// Runner is the subset of tmux.Client used by Apply (lets tests inject a fake).
type Runner interface {
	Run(ctx context.Context, args []string) (string, error)
}

// Apply executes the plan via the Runner. Best-effort: individual failures
// are swallowed so the rest of the plan still runs.
//
// CreateSession / CreateWindow / SplitPane each pass StartupCommand as the
// trailing shell-command argument when non-empty (tmux runs it via /bin/sh -c
// for the new pane). When empty, the trailing arg is omitted and tmux uses
// its default-command. Scrollback rendering is the responsibility of the
// startup command itself — see restore.BuildStartupCommand.
func Apply(ctx context.Context, t Runner, plan []Action) error {
	for _, a := range plan {
		var args []string
		switch v := a.(type) {
		case CreateSession:
			args = []string{"new-session", "-d", "-s", v.Name, "-c", v.Cwd}
			if v.StartupCommand != "" {
				args = append(args, v.StartupCommand)
			}
		case CreateWindow:
			args = []string{"new-window", "-t", fmt.Sprintf("%s:%d", v.Session, v.Index), "-n", v.Name, "-c", v.Cwd}
			if v.StartupCommand != "" {
				args = append(args, v.StartupCommand)
			}
			if _, err := t.Run(ctx, args); err != nil {
				continue
			}
			// `new-window -n` disables automatic-rename. For windows that named
			// themselves via automatic-rename-format, re-enable it so the live
			// format takes over instead of pinning the stale stored name.
			if v.AutomaticRename {
				_, _ = t.Run(ctx, []string{"set-window-option", "-t", fmt.Sprintf("%s:%d", v.Session, v.Index), "automatic-rename", "on"})
			}
			continue
		case SplitPane:
			args = []string{"split-window", "-t", v.Target, "-c", v.Cwd}
			if v.StartupCommand != "" {
				args = append(args, v.StartupCommand)
			}
		case SetLayout:
			args = []string{"select-layout", "-t", v.Window, v.Layout}
		default:
			// Unknown action type is a programming error (not a runtime
			// failure), so we abort rather than silently skip — callers
			// are expected to handle all Action variants.
			return fmt.Errorf("unknown action: %T", a)
		}
		if _, err := t.Run(ctx, args); err != nil {
			continue
		}
	}
	return nil
}
