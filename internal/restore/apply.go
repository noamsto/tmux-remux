package restore

import (
	"context"
	"fmt"
	"strconv"
)

// Runner is the subset of tmux.Client used by Apply (lets tests inject a fake).
type Runner interface {
	Run(ctx context.Context, args []string) (string, error)
}

// Apply executes the plan via the Runner. Best-effort: individual failures
// are swallowed so the rest of the plan still runs. RestoreScrollback actions
// are no-ops here — see ApplyWithScrollback.
func Apply(ctx context.Context, t Runner, plan []Action) error {
	for _, a := range plan {
		var args []string
		switch v := a.(type) {
		case CreateSession:
			args = []string{"new-session", "-d", "-s", v.Name, "-c", v.Cwd}
		case CreateWindow:
			args = []string{"new-window", "-t", fmt.Sprintf("%s:%d", v.Session, v.Index), "-n", v.Name, "-c", v.Cwd}
		case SplitPane:
			args = []string{"split-window", "-t", v.Target, "-c", v.Cwd}
		case SetLayout:
			args = []string{"select-layout", "-t", v.Window, v.Layout}
		case RelaunchCommand:
			cmd := v.Command
			for _, a := range v.Args {
				cmd += " " + strconv.Quote(a)
			}
			args = []string{"send-keys", "-t", v.Pane, cmd, "Enter"}
		case RestoreScrollback:
			continue
		default:
			return fmt.Errorf("unknown action: %T", a)
		}
		if _, err := t.Run(ctx, args); err != nil {
			continue
		}
	}
	return nil
}
