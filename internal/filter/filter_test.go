package filter_test

import (
	"testing"
	"time"

	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/snapshot"
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

func TestDedupRunningServer(t *testing.T) {
	f := filter.Filter{DedupRunningServer: true}
	running := map[string]bool{"foo": true}
	if !f.SkipSession(snapshot.Session{Name: "foo"}, running) {
		t.Error("name match should dedup")
	}
	if f.SkipSession(snapshot.Session{Name: "bar"}, running) {
		t.Error("name miss should not dedup")
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
