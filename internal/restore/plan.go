// Package restore plans and applies tmux-remux restore operations.
package restore

import (
	"fmt"
	"sort"

	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

// Action is one step of a restore plan. Concrete types are in this file.
// Apply() type-switches on the concrete type.
type Action interface {
	isAction()
}

// CreateWindow creates a new tmux window inside a session. StartupCommand,
// when non-empty, is passed as the trailing shell-command argument to
// tmux new-window — the new window's first pane is born running it.
type CreateWindow struct {
	Session        string
	Index          int
	Name           string
	Cwd            string
	StartupCommand string
	// AutomaticRename re-enables automatic-rename on the window after creation,
	// so the live name format takes over instead of the pinned stored name.
	AutomaticRename bool
	// NewSession marks the session's first restored window. Apply creates the
	// session and this window in one new-session call, since tmux hands every
	// new session a window that would otherwise squat on this one's index.
	NewSession bool
	// InsertBefore emits new-window -b, which places the window at Index
	// exactly: tmux inserts and shifts the survivors up when Index is taken,
	// and places it directly when Index is free. Set only for single-entity
	// restores (undo, close picker) — inserting mid-plan would shift windows
	// that later actions in the same plan target by index.
	InsertBefore bool
}

func (CreateWindow) isAction() {}

// SplitPane creates a new pane inside a window via split-window.
// StartupCommand, when non-empty, is passed as the trailing shell-command
// argument; the new pane is born running it.
type SplitPane struct {
	Target         string // <session>:<window_index>
	Cwd            string
	StartupCommand string
}

func (SplitPane) isAction() {}

// SetLayout applies a tmux layout string to a window.
type SetLayout struct {
	Window string
	Layout string
}

func (SetLayout) isAction() {}

// SetOption re-applies one captured window/pane option on restore via
// set-window-option / set-option. Emitted per decoration key so persona
// decoration (agent codename, tint) survives a server restart.
type SetOption struct {
	Target string // <session>:<window_index>
	Pane   bool   // set-option -p vs set-window-option
	Name   string
	Value  string
}

func (SetOption) isAction() {}

// BuildOptions carries the values needed to compose StartupCommands. Resolved
// once per restore by the caller.
type BuildOptions struct {
	// Self is the absolute path of the running tmux-remux binary
	// (os.Executable() in production). Used only when a pane has stored
	// scrollback; ignored otherwise.
	Self string
	// DefaultShell is the resolved fallback shell for panes without an
	// allow-listed command. See restore.DefaultShell.
	DefaultShell string
	// IsBash is the second return value of restore.DefaultShell; signals
	// that DefaultShell should be exec'd with -l.
	IsBash bool
	// AllowList is the set of commands eligible for relaunch as the pane's
	// initial process. Anything not in the list falls through to DefaultShell.
	AllowList []string
}

// PlanStats summarizes what BuildPlan kept and filtered, for restore logging
// and the post-restore display-message. "Idle" sessions are ones the smart
// filter dropped entirely because every window was idle plain shells.
type PlanStats struct {
	SessionsKept           int
	SessionsSkippedRunning int
	SessionsSkippedStale   int
	SessionsSkippedIdle    int
	WindowsSkippedIdle     int
}

// paneStartup composes the startup shell-command for a restored pane: replay
// its stored scrollback, then relaunch via the pane's @remux_relaunch override if
// set, else the original command when it's on the allow-list (otherwise fall
// through to the default shell).
func paneStartup(p snapshot.Pane, opts BuildOptions) string {
	so := StartupOpts{
		Self:          opts.Self,
		DefaultShell:  opts.DefaultShell,
		IsBash:        opts.IsBash,
		ScrollbackSHA: p.ScrollbackSHA,
	}
	if p.Relaunch != "" {
		// A pane-supplied @remux_relaunch override wins over the allow-list.
		so.OverrideCmd = p.Relaunch
	} else {
		for _, c := range opts.AllowList {
			if c == p.Command {
				so.RelaunchCmd = p.Command
				so.RelaunchArgs = p.CommandArgs
				break
			}
		}
	}
	return BuildStartupCommand(so)
}

// BuildPlan builds an ordered slice of Actions to restore the manifest,
// honoring the filter and the allow-list of commands. The returned PlanStats
// reports what was kept vs filtered, per reason.
func BuildPlan(m snapshot.Manifest, f filter.Filter, runningSessions map[string]bool, opts BuildOptions) ([]Action, PlanStats) {
	startupFor := func(p snapshot.Pane) string { return paneStartup(p, opts) }

	var plan []Action
	var stats PlanStats
	for _, sess := range m.Sessions {
		switch f.SessionSkipReason(sess, runningSessions) {
		case "running":
			stats.SessionsSkippedRunning++
			continue
		case "stale":
			stats.SessionsSkippedStale++
			continue
		}
		var sessionStarted bool
		for _, win := range sess.Windows {
			if f.SkipWindow(win) {
				stats.WindowsSkippedIdle++
				continue
			}
			var firstPane *snapshot.Pane
			var keptPanes []snapshot.Pane
			for i := range win.Panes {
				p := win.Panes[i]
				if f.SkipPane(p) {
					continue
				}
				if firstPane == nil {
					firstPane = &p
				}
				keptPanes = append(keptPanes, p)
			}
			if firstPane == nil {
				stats.WindowsSkippedIdle++
				continue
			}
			plan = append(plan, CreateWindow{
				Session:         sess.Name,
				Index:           win.Index,
				Name:            win.Name,
				Cwd:             firstPane.Cwd,
				StartupCommand:  startupFor(*firstPane),
				AutomaticRename: win.AutomaticRename,
				NewSession:      !sessionStarted,
			})
			sessionStarted = true
			if len(win.Decoration) > 0 {
				names := make([]string, 0, len(win.Decoration))
				for k := range win.Decoration {
					names = append(names, k)
				}
				sort.Strings(names)
				target := fmt.Sprintf("%s:%d", sess.Name, win.Index)
				for _, k := range names {
					plan = append(plan, SetOption{Target: target, Name: k, Value: win.Decoration[k]})
				}
			}
			for _, p := range keptPanes[1:] {
				plan = append(plan, SplitPane{
					Target:         fmt.Sprintf("%s:%d", sess.Name, win.Index),
					Cwd:            p.Cwd,
					StartupCommand: startupFor(p),
				})
			}
			plan = append(plan, SetLayout{
				Window: fmt.Sprintf("%s:%d", sess.Name, win.Index),
				Layout: win.Layout,
			})
		}
		if sessionStarted {
			stats.SessionsKept++
		} else if len(sess.Windows) > 0 {
			stats.SessionsSkippedIdle++
		}
	}
	return plan, stats
}

// BuildPaneRestore plans the recovery of one lost pane. liveTarget is the tmux
// target of its parent window as resolved by the caller, or "" when that window
// is gone — in which case the window is rebuilt from the snapshot and brings
// its panes with it.
func BuildPaneRestore(lost snapshot.Pane, win snapshot.Window, session, liveTarget string, opts BuildOptions) []Action {
	if liveTarget == "" {
		plan, _ := BuildPlan(snapshot.Manifest{
			V:        1,
			Sessions: []snapshot.Session{{Name: session, Windows: []snapshot.Window{win}}},
		}, filter.Filter{}, nil, opts)
		// This plan rebuilds exactly one window, so inserting it at its recorded
		// index cannot shift anything else the plan targets. Without -b,
		// new-window collides with whatever renumber-windows moved into the
		// vacated index, and the SplitPane/SetLayout that follow land on that
		// unrelated window instead.
		for i, a := range plan {
			if cw, ok := a.(CreateWindow); ok {
				cw.InsertBefore = true
				plan[i] = cw
			}
		}
		return plan
	}
	return []Action{
		SplitPane{Target: liveTarget, Cwd: lost.Cwd, StartupCommand: paneStartup(lost, opts)},
		SetLayout{Window: liveTarget, Layout: win.Layout},
	}
}
