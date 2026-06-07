package restore

import (
	"context"
	"fmt"
	"sort"
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
		case SplitPane:
			args = []string{"split-window", "-t", v.Target, "-c", v.Cwd}
			if v.StartupCommand != "" {
				args = append(args, v.StartupCommand)
			}
		case SetLayout:
			args = []string{"select-layout", "-t", v.Window, v.Layout}
		case SetWindowOptions:
			// One set-option per entry, in sorted key order for determinism.
			names := make([]string, 0, len(v.Options))
			for name := range v.Options {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				_, _ = t.Run(ctx, []string{"set-option", "-t", v.Window, "-w", name, v.Options[name]})
			}
			continue
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
