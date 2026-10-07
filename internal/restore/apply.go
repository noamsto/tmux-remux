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

// FailedAction identifies an action that could not be applied. Apply continues
// after individual failures, leaving callers to decide which failures are
// fatal for their workflow.
type FailedAction struct {
	Action Action
	Err    error
}

func (f FailedAction) Error() string { return f.Err.Error() }

// Apply executes the plan via the Runner. Best-effort: a failed action is
// collected and the rest of the plan still runs. The returned slice holds one
// error per failed action so callers can report a partial restore; only an
// unknown action type — a programming error — aborts with a non-nil error.
//
// CreateWindow / SplitPane never start their StartupCommand: saved layouts
// resize the window to the screen they were saved on, and a full-screen program
// launched before that settles redraws through every resize. Panes are born
// with tmux's default-command, and each non-empty StartupCommand is run via
// respawn-pane once its window's SetLayout has applied the layout and fitted
// the window back to its current size. Panes with an empty StartupCommand keep
// the default-command. Scrollback rendering is the responsibility of the
// startup command itself — see restore.BuildStartupCommand.
func Apply(ctx context.Context, t Runner, plan []Action) ([]FailedAction, error) {
	var failed []FailedAction
	// pending holds, per "<session>:<index>" window target, the panes still
	// waiting for their startup command; pendingOrder keeps launches in plan
	// order. Panes are addressed by id because renumber-windows may reshuffle
	// indices mid-restore.
	pending := map[string][]pendingPane{}
	var pendingOrder []string
	queue := func(target string, p pendingPane) {
		if p.startup == "" {
			return
		}
		if _, ok := pending[target]; !ok {
			pendingOrder = append(pendingOrder, target)
		}
		pending[target] = append(pending[target], p)
	}
	// failedWindows holds the "<session>:<index>" target of every CreateWindow
	// that failed. Apply is best-effort, so without this a later SplitPane or
	// SetLayout aimed at the same index would still run and land on whatever
	// unrelated live window happens to sit there.
	failedWindows := map[string]bool{}
	for _, a := range plan {
		switch v := a.(type) {
		case CreateWindow:
			target := fmt.Sprintf("%s:%d", v.Session, v.Index)
			paneID, err := createWindow(ctx, t, v)
			if err != nil {
				failed = append(failed, FailedAction{Action: a, Err: err})
				failedWindows[target] = true
				continue
			}
			if paneID == "" && v.StartupCommand != "" {
				failed = append(failed, FailedAction{Action: a, Err: fmt.Errorf("new-window %s: no pane id reported", target)})
				continue
			}
			queue(target, pendingPane{id: paneID, startup: v.StartupCommand})
		case SplitPane:
			if failedWindows[v.Target] {
				continue
			}
			out, err := t.Run(ctx, []string{"split-window", "-t", v.Target, "-c", v.Cwd, "-P", "-F", "#{pane_id}"})
			if err != nil {
				failed = append(failed, FailedAction{Action: a, Err: err})
				continue
			}
			paneID := strings.TrimSpace(out)
			if paneID == "" && v.StartupCommand != "" {
				failed = append(failed, FailedAction{Action: a, Err: fmt.Errorf("split-window %s: no pane id reported", v.Target)})
				continue
			}
			queue(v.Target, pendingPane{id: paneID, startup: v.StartupCommand})
		case SetLayout:
			if failedWindows[v.Window] {
				continue
			}
			failed = append(failed, settleWindow(ctx, t, v.Window, v.Layout, pending[v.Window])...)
			delete(pending, v.Window)
		case SetOption:
			if failedWindows[v.Target] {
				continue
			}
			cmd := "set-window-option"
			flags := "-q"
			if v.Pane {
				cmd = "set-option"
				flags = "-pq"
			}
			if _, err := t.Run(ctx, []string{cmd, flags, "-t", v.Target, v.Name, v.Value}); err != nil {
				failed = append(failed, FailedAction{Action: a, Err: err})
			}
		default:
			// Unknown action type is a programming error (not a runtime
			// failure), so we abort rather than silently skip — callers
			// are expected to handle all Action variants.
			return failed, fmt.Errorf("unknown action: %T", a)
		}
	}
	for _, target := range pendingOrder {
		if len(pending[target]) > 0 {
			failed = append(failed, settleWindow(ctx, t, target, "", pending[target])...)
		}
	}
	return failed, nil
}

// pendingPane is a freshly created pane whose startup command has not run yet.
type pendingPane struct {
	id      string
	startup string
}

// settleWindow gives target its final geometry, then runs every pending pane's
// startup command. A non-empty layout is applied first, and the window is then
// resized back to the size it had beforehand: select-layout resizes the window
// to the saved layout's dimensions, but a window restored on this screen must
// fit this screen. resize-window pins window-size to manual, which would stop
// the window following its client, so the previous setting is put back.
func settleWindow(ctx context.Context, t Runner, target, layout string, panes []pendingPane) []FailedAction {
	var failed []FailedAction
	if layout != "" {
		size, sizeErr := t.Run(ctx, []string{"display-message", "-p", "-t", target, "#{window_width} #{window_height}"})
		if _, err := t.Run(ctx, []string{"select-layout", "-t", target, layout}); err != nil {
			failed = append(failed, FailedAction{Action: SetLayout{Window: target, Layout: layout}, Err: err})
		} else if sizeErr == nil {
			fitWindow(ctx, t, target, size)
		}
	}
	for _, p := range panes {
		// respawn-pane keeps history, so whatever the placeholder shell printed
		// would sit above the scrollback the startup command replays.
		_, _ = t.Run(ctx, []string{"clear-history", "-t", p.id})
		if _, err := t.Run(ctx, []string{"respawn-pane", "-k", "-t", p.id, p.startup}); err != nil {
			failed = append(failed, FailedAction{Action: SplitPane{Target: target}, Err: err})
		}
	}
	return failed
}

// fitWindow resizes target to size ("<width> <height>"), keeping its
// window-size option as it was. A size that does not parse is skipped:
// leaving the saved dimensions is better than a bogus resize.
func fitWindow(ctx context.Context, t Runner, target, size string) {
	w, h, ok := strings.Cut(strings.TrimSpace(size), " ")
	if _, err := strconv.Atoi(w); !ok || err != nil {
		return
	}
	if _, err := strconv.Atoi(h); err != nil {
		return
	}
	prev, _ := t.Run(ctx, []string{"show-options", "-wv", "-t", target, "window-size"})
	if _, err := t.Run(ctx, []string{"resize-window", "-t", target, "-x", w, "-y", h}); err != nil {
		return
	}
	if prev = strings.TrimSpace(prev); prev != "" {
		_, _ = t.Run(ctx, []string{"set-window-option", "-t", target, "window-size", prev})
		return
	}
	_, _ = t.Run(ctx, []string{"set-window-option", "-u", "-t", target, "window-size"})
}

// createWindow materializes one window. For the session's first window it
// creates the session too, in a single new-session call: tmux always gives a
// new session a window of its own, so creating the two separately leaves that
// window squatting on base-index and the window-create for that index fails
// with "index in use". The window's first pane id is returned so its startup
// command can be respawned once the layout is final.
//
// new-session places the window at base-index whatever the recorded index is,
// hence the move. A NewSession window whose session is already live — undo of a
// window closed out of a surviving session — falls back to plain new-window.
func createWindow(ctx context.Context, t Runner, v CreateWindow) (string, error) {
	target := fmt.Sprintf("%s:%d", v.Session, v.Index)
	if v.NewSession {
		created, err := newSession(ctx, t, v)
		if err == nil {
			if created.index != v.Index {
				if _, err := t.Run(ctx, []string{"move-window", "-s", created.id, "-t", target}); err != nil {
					return "", err
				}
			}
			reenableAutomaticRename(ctx, t, v.AutomaticRename, target)
			return created.paneID, nil
		}
		if !isDuplicateSession(err) {
			return "", err
		}
	}
	newWindowArgs := func(insertBefore bool) []string {
		a := []string{"new-window"}
		if insertBefore {
			a = append(a, "-b")
		}
		return append(a, "-t", target, "-n", v.Name, "-c", v.Cwd, "-P", "-F", "#{pane_id}")
	}
	out, err := t.Run(ctx, newWindowArgs(v.InsertBefore))
	if err != nil {
		// A tmux without new-window -b rejects the command line itself. Drop
		// the flag and let tmux place the window, rather than losing it.
		if !v.InsertBefore || !isUsageError(err) {
			return "", err
		}
		if out, err = t.Run(ctx, newWindowArgs(false)); err != nil {
			return "", err
		}
	}
	reenableAutomaticRename(ctx, t, v.AutomaticRename, target)
	return strings.TrimSpace(out), nil
}

// isUsageError reports whether err is tmux rejecting the command line — an
// unknown flag — rather than refusing the operation. tmux answers a bad flag
// with "usage:" and the command synopsis.
func isUsageError(err error) bool {
	return strings.Contains(err.Error(), "usage:")
}

const newSessionFormat = "#{window_id} #{window_index} #{pane_id}"

// createdWindow is the window and first pane tmux reports back from
// new-session -P.
type createdWindow struct {
	id     string
	index  int
	paneID string
}

// newSession creates v's session with v as its only window, reporting where
// tmux actually put that window.
func newSession(ctx context.Context, t Runner, v CreateWindow) (createdWindow, error) {
	args := []string{"new-session", "-d", "-s", v.Session, "-n", v.Name, "-c", v.Cwd, "-P", "-F", newSessionFormat}
	out, err := t.Run(ctx, args)
	if err != nil {
		return createdWindow{}, err
	}
	f := strings.Fields(out)
	if len(f) != 3 {
		return createdWindow{}, fmt.Errorf("new-session %s: unparsable window %q", v.Session, out)
	}
	n, err := strconv.Atoi(f[1])
	if err != nil {
		return createdWindow{}, fmt.Errorf("new-session %s: window index %q: %w", v.Session, f[1], err)
	}
	return createdWindow{id: f[0], index: n, paneID: f[2]}, nil
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
