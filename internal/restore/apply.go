package restore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Runner is the subset of tmux.Client used by Apply (lets tests inject a fake).
type Runner interface {
	Run(ctx context.Context, args []string) (string, error)
}

// Apply executes the plan via the Runner. Best-effort: a failed action is
// collected and the rest of the plan still runs. The returned slice holds one
// error per failed action so callers can report a partial restore; only an
// unknown action type — a programming error — aborts with a non-nil error.
//
// CreateWindow / SplitPane each pass StartupCommand as the trailing
// shell-command argument when non-empty (tmux runs it via /bin/sh -c for the
// new pane). When empty, the trailing arg is omitted and tmux uses its
// default-command. Scrollback rendering is the responsibility of the startup
// command itself — see restore.BuildStartupCommand.
func Apply(ctx context.Context, t Runner, plan []Action) ([]error, error) {
	var failed []error
	for _, a := range plan {
		var args []string
		switch v := a.(type) {
		case CreateWindow:
			if err := createWindow(ctx, t, v); err != nil {
				failed = append(failed, err)
			}
			continue
		case SplitPane:
			args = []string{"split-window", "-t", v.Target, "-c", v.Cwd}
			if v.StartupCommand != "" {
				args = append(args, v.StartupCommand)
			}
		case SetLayout:
			args = []string{"select-layout", "-t", v.Window, v.Layout}
		case SetOption:
			cmd := "set-window-option"
			flags := "-q"
			if v.Pane {
				cmd = "set-option"
				flags = "-pq"
			}
			args = []string{cmd, flags, "-t", v.Target, v.Name, v.Value}
		default:
			// Unknown action type is a programming error (not a runtime
			// failure), so we abort rather than silently skip — callers
			// are expected to handle all Action variants.
			return failed, fmt.Errorf("unknown action: %T", a)
		}
		if _, err := t.Run(ctx, args); err != nil {
			failed = append(failed, err)
		}
	}
	return failed, nil
}

// createWindow materializes one window. For the session's first window it
// creates the session too, in a single new-session call: tmux always gives a
// new session a window of its own, so creating the two separately leaves that
// window squatting on base-index and the window-create for that index fails
// with "index in use", losing its startup command.
//
// new-session places the window at base-index whatever the recorded index is,
// hence the move. A NewSession window whose session is already live — undo of a
// window closed out of a surviving session — falls back to plain new-window.
func createWindow(ctx context.Context, t Runner, v CreateWindow) error {
	target := fmt.Sprintf("%s:%d", v.Session, v.Index)
	if v.NewSession {
		created, err := newSession(ctx, t, v)
		if err == nil {
			if created.index != v.Index {
				if _, err := t.Run(ctx, []string{"move-window", "-s", created.id, "-t", target}); err != nil {
					return err
				}
			}
			reenableAutomaticRename(ctx, t, v.AutomaticRename, target)
			return nil
		}
		if !isDuplicateSession(err) {
			return err
		}
	}
	args := []string{"new-window", "-t", target, "-n", v.Name, "-c", v.Cwd}
	if v.StartupCommand != "" {
		args = append(args, v.StartupCommand)
	}
	if _, err := t.Run(ctx, args); err != nil {
		return err
	}
	reenableAutomaticRename(ctx, t, v.AutomaticRename, target)
	return nil
}

// createdWindow is the window tmux reports back from new-session -P.
type createdWindow struct {
	id    string
	index int
}

// newSession creates v's session with v as its only window, reporting where
// tmux actually put that window.
func newSession(ctx context.Context, t Runner, v CreateWindow) (createdWindow, error) {
	args := []string{"new-session", "-d", "-s", v.Session, "-n", v.Name, "-c", v.Cwd, "-P", "-F", "#{window_id} #{window_index}"}
	if v.StartupCommand != "" {
		args = append(args, v.StartupCommand)
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return createdWindow{}, err
	}
	id, idx, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok {
		return createdWindow{}, fmt.Errorf("new-session %s: unparsable window %q", v.Session, out)
	}
	n, err := strconv.Atoi(idx)
	if err != nil {
		return createdWindow{}, fmt.Errorf("new-session %s: window index %q: %w", v.Session, idx, err)
	}
	return createdWindow{id: id, index: n}, nil
}

// isDuplicateSession reports whether err is tmux refusing to create a session
// that already exists ("duplicate session: <name>").
func isDuplicateSession(err error) bool {
	return strings.Contains(err.Error(), "duplicate session")
}

// reenableAutomaticRename undoes the automatic-rename that `-n` turns off. For
// windows that named themselves via automatic-rename-format, the live format
// should take over instead of pinning the stale stored name.
func reenableAutomaticRename(ctx context.Context, t Runner, on bool, target string) {
	if !on {
		return
	}
	_, _ = t.Run(ctx, []string{"set-window-option", "-t", target, "automatic-rename", "on"})
}
