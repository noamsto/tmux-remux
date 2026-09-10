package filter_test

import (
	"testing"
	"time"

	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/snapshot"
)

func TestSkipIdleShells(t *testing.T) {
	cases := []struct {
		name string
		pane snapshot.Pane
		want bool
	}{
		{"bash no children", snapshot.Pane{Command: "bash", ChildCount: 0}, true},
		{"bash with children", snapshot.Pane{Command: "bash", ChildCount: 2}, false},
		{"nvim no children", snapshot.Pane{Command: "nvim", ChildCount: 0}, false},
		{"fish no children", snapshot.Pane{Command: "fish", ChildCount: 0}, true},
	}
	f := filter.Filter{SkipIdleShells: true}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := f.SkipPane(c.pane); got != c.want {
				t.Errorf("SkipPane(%v) = %v, want %v", c.pane, got, c.want)
			}
		})
	}
}

func TestSkipStaleSession(t *testing.T) {
	now := time.Unix(1000000, 0)
	f := filter.Filter{Now: now, MaxSessionAge: time.Hour}
	old := snapshot.Session{LastAttached: now.Add(-2 * time.Hour).Unix()}
	fresh := snapshot.Session{LastAttached: now.Add(-30 * time.Minute).Unix()}
	if !f.SkipSession(old, nil) {
		t.Error("old session should be skipped")
	}
	if f.SkipSession(fresh, nil) {
		t.Error("fresh session should not be skipped")
	}
}

func TestSkipRunningSessions(t *testing.T) {
	f := filter.Filter{SkipRunningSessions: true}
	running := map[string]bool{"foo": true}
	if !f.SkipSession(snapshot.Session{Name: "foo"}, running) {
		t.Error("name match should be skipped")
	}
	if f.SkipSession(snapshot.Session{Name: "bar"}, running) {
		t.Error("name miss should not be skipped")
	}
}

func TestSkipIdleWindow(t *testing.T) {
	f := filter.Filter{SkipIdleShells: true, SkipIdleWindows: true}
	allIdle := snapshot.Window{Panes: []snapshot.Pane{
		{Command: "bash", ChildCount: 0},
		{Command: "fish", ChildCount: 0},
	}}
	mixed := snapshot.Window{Panes: []snapshot.Pane{
		{Command: "bash", ChildCount: 0},
		{Command: "nvim", ChildCount: 0},
	}}
	if !f.SkipWindow(allIdle) {
		t.Error("all-idle window should be skipped")
	}
	if f.SkipWindow(mixed) {
		t.Error("mixed window should not be skipped")
	}
}

func TestSessionSkipReason(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := filter.Filter{
		Now:                 now,
		MaxSessionAge:       time.Hour,
		SkipRunningSessions: true,
	}
	fresh := snapshot.Session{Name: "fresh", LastAttached: now.Unix() - 60}
	stale := snapshot.Session{Name: "stale", LastAttached: now.Unix() - 7200}
	running := map[string]bool{"fresh": true}

	if got := f.SessionSkipReason(fresh, running); got != "running" {
		t.Errorf("running session: reason = %q, want \"running\"", got)
	}
	if got := f.SessionSkipReason(stale, nil); got != "stale" {
		t.Errorf("stale session: reason = %q, want \"stale\"", got)
	}
	if got := f.SessionSkipReason(fresh, nil); got != "" {
		t.Errorf("kept session: reason = %q, want \"\"", got)
	}
}

// A session that is a bridge mirror right now is a rendering of a remote: its
// panes were only ever the renderer's own local shell, so recreating them is a
// lie however the flags are set. The reason is reported ahead of "running"
// because a mirror is normally running too, and "running" would send a reader
// looking for a session they could just attach to.
func TestSessionSkipReason_LiveBridgeMirror(t *testing.T) {
	mirror := snapshot.Session{Name: "halo-houston", LastAttached: time.Now().Unix()}
	f := filter.Filter{Bridged: map[string]bool{"halo-houston": true}}
	if got := f.SessionSkipReason(mirror, nil); got != "bridged" {
		t.Errorf("SessionSkipReason = %q, want %q with no flags set at all", got, "bridged")
	}
	f.SkipRunningSessions = true
	if got := f.SessionSkipReason(mirror, map[string]bool{"halo-houston": true}); got != "bridged" {
		t.Errorf("SessionSkipReason = %q, want %q ahead of \"running\"", got, "bridged")
	}
	other := snapshot.Session{Name: "mono", LastAttached: time.Now().Unix()}
	if got := f.SessionSkipReason(other, nil); got != "" {
		t.Errorf("SessionSkipReason(mono) = %q, want it kept", got)
	}
}
