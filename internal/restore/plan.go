// Package restore plans and applies tmux-state restore operations.
package restore

import (
	"fmt"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

// Action is one step of a restore plan. Concrete types are in this file.
// Apply() type-switches on the concrete type.
type Action interface {
	isAction()
}

// CreateSession creates a new tmux session. StartupCommand, when non-empty,
// is passed as the trailing shell-command argument to tmux new-session.
//
// BuildPlan currently leaves StartupCommand empty: the implicit default
// window that `tmux new-session` creates is left empty, and the subsequent
// CreateWindow action carries the startup command for the first kept pane.
// This means restored sessions end up with an extra empty window-0 — a
// pre-existing issue scoped out of the fast-restore refactor (see spec
// 2026-05-10-fast-restore-design.md §"Non-goals"). The field is kept so a
// future fix can populate it (folding the first CreateWindow into the
// session-create call) without changing the action shape.
type CreateSession struct {
	Name           string
	Cwd            string
	StartupCommand string
}

func (CreateSession) isAction() {}

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

// BuildOptions carries the values needed to compose StartupCommands. Resolved
// once per restore by the caller.
type BuildOptions struct {
	// Self is the absolute path of the running tmux-state binary
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

// BuildPlan builds an ordered slice of Actions to restore the manifest,
// honoring the filter and the allow-list of commands. The returned PlanStats
// reports what was kept vs filtered, per reason.
func BuildPlan(m snapshot.Manifest, f filter.Filter, runningSessions map[string]bool, opts BuildOptions) ([]Action, PlanStats) {
	allowed := map[string]bool{}
	for _, c := range opts.AllowList {
		allowed[c] = true
	}

	startupFor := func(p snapshot.Pane) string {
		so := StartupOpts{
			Self:          opts.Self,
			DefaultShell:  opts.DefaultShell,
			IsBash:        opts.IsBash,
			ScrollbackSHA: p.ScrollbackSHA,
		}
		if allowed[p.Command] {
			so.RelaunchCmd = p.Command
			so.RelaunchArgs = p.CommandArgs
		}
		return BuildStartupCommand(so)
	}

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
			if !sessionStarted {
				plan = append(plan, CreateSession{Name: sess.Name, Cwd: firstPane.Cwd})
				sessionStarted = true
			}
			plan = append(plan, CreateWindow{
				Session:         sess.Name,
				Index:           win.Index,
				Name:            win.Name,
				Cwd:             firstPane.Cwd,
				StartupCommand:  startupFor(*firstPane),
				AutomaticRename: win.AutomaticRename,
			})
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
