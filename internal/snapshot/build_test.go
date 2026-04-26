package snapshot_test

import (
	"context"
	"testing"

	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/tmux"
)

type fakeClient struct {
	sessions []tmux.SessionRow
	windows  []tmux.WindowRow
	panes    []tmux.PaneRow
}

func (f *fakeClient) ListSessions(context.Context) ([]tmux.SessionRow, error) {
	return f.sessions, nil
}
func (f *fakeClient) ListWindows(context.Context) ([]tmux.WindowRow, error) { return f.windows, nil }
func (f *fakeClient) ListPanes(context.Context) ([]tmux.PaneRow, error)     { return f.panes, nil }

func TestBuildAssemblesTree(t *testing.T) {
	fc := &fakeClient{
		sessions: []tmux.SessionRow{
			{Name: "s1", LastAttached: 100},
		},
		windows: []tmux.WindowRow{
			{Session: "s1", Index: 1, Name: "main", Layout: "L"},
		},
		panes: []tmux.PaneRow{
			{Session: "s1", WindowIndex: 1, PaneIndex: 1, Cwd: "/home", Command: "nvim", PID: 1234, LastUsed: 99},
			{Session: "s1", WindowIndex: 1, PaneIndex: 2, Cwd: "/tmp", Command: "bash", PID: 1235, LastUsed: 50},
		},
	}
	m, err := snapshot.Build(context.Background(), fc, "host1", 200)
	if err != nil {
		t.Fatal(err)
	}
	if m.Host != "host1" || m.SavedAt != 200 {
		t.Errorf("envelope wrong: %+v", m)
	}
	if len(m.Sessions) != 1 || m.Sessions[0].Name != "s1" {
		t.Fatalf("sessions: %+v", m.Sessions)
	}
	if len(m.Sessions[0].Windows) != 1 {
		t.Fatalf("windows: %+v", m.Sessions[0].Windows)
	}
	if len(m.Sessions[0].Windows[0].Panes) != 2 {
		t.Fatalf("panes: %+v", m.Sessions[0].Windows[0].Panes)
	}
}
