// Package filter implements the smart-restore filter as pure functions.
package filter

import (
	"time"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

var defaultIdleShells = map[string]bool{
	"bash": true, "fish": true, "zsh": true, "sh": true,
}

// Filter holds the configurable thresholds and feature flags for the
// smart-restore filter.
type Filter struct {
	Now                 time.Time     // injectable for tests
	MaxSessionAge       time.Duration // 0 = no limit
	MaxSnapshotAge      time.Duration // 0 = no limit
	SkipIdleShells      bool
	SkipIdleWindows     bool
	SkipRunningSessions bool
	IdleShellNames      map[string]bool // override default set
	// Bridged names the sessions that are lazytmux bridge mirrors right now,
	// read from live tmux (#{@bridge_host}) by whoever built this filter. It
	// is a field rather than a per-query argument like `running` because the
	// answer cannot come from the snapshot being restored: a snapshot may
	// predate the bridge, predate the option, or have been written by a build
	// that did not record it, and restoring a mirror it happens to contain
	// recreates a session that is a rendering of a remote — panes that were
	// only ever the renderer's own local shell.
	Bridged map[string]bool
}

// SkipSnapshot returns true if the whole snapshot should be skipped due to age.
func (f Filter) SkipSnapshot(savedAtMillis int64) bool {
	if f.MaxSnapshotAge == 0 {
		return false
	}
	now := f.now()
	saved := time.UnixMilli(savedAtMillis)
	return now.Sub(saved) > f.MaxSnapshotAge
}

// SessionSkipReason reports why the session would be filtered out:
// "bridged", "running", "stale", or "" when it should be kept.
func (f Filter) SessionSkipReason(s snapshot.Session, running map[string]bool) string {
	// Checked before "running", and unconditionally: a mirror is not a thing
	// to restore whatever the flags say, and it is usually running too, which
	// would otherwise report the less specific reason.
	if f.Bridged[s.Name] {
		return "bridged"
	}
	if f.SkipRunningSessions && running[s.Name] {
		return "running"
	}
	if f.MaxSessionAge > 0 {
		la := time.Unix(s.LastAttached, 0)
		if f.now().Sub(la) > f.MaxSessionAge {
			return "stale"
		}
	}
	return ""
}

// SkipSession returns true if the session should be filtered out (a live
// bridge mirror, already running, or stale).
func (f Filter) SkipSession(s snapshot.Session, running map[string]bool) bool {
	return f.SessionSkipReason(s, running) != ""
}

// SkipPane returns true if the pane should be filtered out (idle plain shell).
func (f Filter) SkipPane(p snapshot.Pane) bool {
	if !f.SkipIdleShells {
		return false
	}
	idle := f.IdleShellNames
	if idle == nil {
		idle = defaultIdleShells
	}
	return idle[p.Command] && p.ChildCount == 0
}

// SkipWindow returns true if every pane in the window would itself be skipped.
func (f Filter) SkipWindow(w snapshot.Window) bool {
	if !f.SkipIdleWindows {
		return false
	}
	for _, p := range w.Panes {
		if !f.SkipPane(p) {
			return false
		}
	}
	return true
}

func (f Filter) now() time.Time {
	if f.Now.IsZero() {
		return time.Now()
	}
	return f.Now
}
