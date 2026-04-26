// Package restore plans and applies tmux-state restore operations.
package restore

import (
	"fmt"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
)

// Action is one step of a restore plan. Concrete types implement kind().
type Action interface{ kind() string }

// CreateSession creates a new tmux session.
type CreateSession struct {
	Name string
	Cwd  string
}

func (CreateSession) kind() string { return "CreateSession" }

// CreateWindow creates a new tmux window inside a session.
type CreateWindow struct {
	Session string
	Index   int
	Name    string
	Cwd     string
}

func (CreateWindow) kind() string { return "CreateWindow" }

// SplitPane creates a new pane inside a window via split-window.
type SplitPane struct {
	Target string // <session>:<window_index>
	Cwd    string
}

func (SplitPane) kind() string { return "SplitPane" }

// SetLayout applies a tmux layout string to a window.
type SetLayout struct {
	Window string
	Layout string
}

func (SetLayout) kind() string { return "SetLayout" }

// RelaunchCommand re-issues an allow-listed command to a pane.
type RelaunchCommand struct {
	Pane    string // <session>:<window_index>.<pane_index>
	Command string
	Args    []string
}

func (RelaunchCommand) kind() string { return "RelaunchCommand" }

// RestoreScrollback pastes a stored scrollback into a pane.
//
//nolint:revive // canonical action name; matches the verb pattern of other actions
type RestoreScrollback struct {
	Pane string
	SHA  string
}

func (RestoreScrollback) kind() string { return "RestoreScrollback" }

// BuildPlan builds an ordered slice of Actions to restore the manifest,
// honoring the filter and the allow-list of commands.
func BuildPlan(m snapshot.Manifest, f filter.Filter, runningSessions map[string]bool, allowList []string) []Action {
	allowed := map[string]bool{}
	for _, c := range allowList {
		allowed[c] = true
	}

	var plan []Action
	for _, sess := range m.Sessions {
		if f.SkipSession(sess, runningSessions) {
			continue
		}
		var sessionStarted bool
		for _, win := range sess.Windows {
			if f.SkipWindow(win) {
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
				continue
			}
			if !sessionStarted {
				plan = append(plan, CreateSession{Name: sess.Name, Cwd: firstPane.Cwd})
				sessionStarted = true
			}
			plan = append(plan, CreateWindow{
				Session: sess.Name, Index: win.Index, Name: win.Name, Cwd: firstPane.Cwd,
			})
			for _, p := range keptPanes[1:] {
				plan = append(plan, SplitPane{
					Target: fmt.Sprintf("%s:%d", sess.Name, win.Index),
					Cwd:    p.Cwd,
				})
			}
			plan = append(plan, SetLayout{
				Window: fmt.Sprintf("%s:%d", sess.Name, win.Index),
				Layout: win.Layout,
			})
			for _, p := range keptPanes {
				if allowed[p.Command] {
					plan = append(plan, RelaunchCommand{
						Pane:    fmt.Sprintf("%s:%d.%d", sess.Name, win.Index, p.Index),
						Command: p.Command, Args: p.CommandArgs,
					})
				}
				if p.ScrollbackSHA != "" {
					plan = append(plan, RestoreScrollback{
						Pane: fmt.Sprintf("%s:%d.%d", sess.Name, win.Index, p.Index),
						SHA:  p.ScrollbackSHA,
					})
				}
			}
		}
	}
	return plan
}
