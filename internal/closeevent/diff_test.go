package closeevent_test

import (
	"testing"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/tmux"
)

func TestFindClosed_WindowUnlinked(t *testing.T) {
	prior := snapshot.Manifest{
		V:    1,
		Host: "h",
		Sessions: []snapshot.Session{
			{Name: "lazytmux", Windows: []snapshot.Window{
				{Index: 1, Name: "main", Panes: []snapshot.Pane{{Index: 1, Command: "claude", Cwd: "/x"}}},
				{Index: 2, Name: "logs", Panes: []snapshot.Pane{{Index: 1, Command: "fish", Cwd: "/y"}}},
			}},
		},
	}
	post := closeevent.CloseManifest{
		WindowID: "@7",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{
				{Session: "lazytmux", Index: 1, Name: "main"},
			},
		},
	}
	got := closeevent.FindClosed(prior, post, "window-unlinked")
	if got == nil {
		t.Fatal("expected ClosedItem, got nil")
	}
	if got.Window == nil || got.Window.Index != 2 {
		t.Errorf("got Window=%+v, want index=2", got.Window)
	}
	if got.SessionName != "lazytmux" {
		t.Errorf("got SessionName=%q, want lazytmux", got.SessionName)
	}
	if got.Describe() != "lazytmux/logs (1p)" {
		t.Errorf("got Describe()=%q, want lazytmux/logs (1p)", got.Describe())
	}
}

func TestFindClosed_SessionClosed(t *testing.T) {
	prior := snapshot.Manifest{
		V:    1,
		Host: "h",
		Sessions: []snapshot.Session{
			{Name: "lazytmux", Windows: []snapshot.Window{{Index: 1, Name: "main"}}},
			{Name: "scratch", Windows: []snapshot.Window{{Index: 1, Name: "main"}, {Index: 2, Name: "logs"}}},
		},
	}
	post := closeevent.CloseManifest{
		SessionID: "scratch",
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "lazytmux", Index: 1, Name: "main"}},
		},
	}
	got := closeevent.FindClosed(prior, post, "session-closed")
	if got == nil || got.Session == nil {
		t.Fatal("expected closed session, got nil")
	}
	if got.SessionName != "scratch" {
		t.Errorf("got SessionName=%q, want scratch", got.SessionName)
	}
	want := "session: scratch (2w)"
	if got.Describe() != want {
		t.Errorf("got Describe()=%q, want %q", got.Describe(), want)
	}
}

func TestFindClosed_NoDiff(t *testing.T) {
	prior := snapshot.Manifest{
		Sessions: []snapshot.Session{{Name: "s", Windows: []snapshot.Window{{Index: 1, Name: "w"}}}},
	}
	post := closeevent.CloseManifest{
		Index: closeevent.IndexPost{
			Windows: []tmux.WindowRow{{Session: "s", Index: 1, Name: "w"}},
		},
	}
	if got := closeevent.FindClosed(prior, post, "window-unlinked"); got != nil {
		t.Errorf("expected nil when nothing was lost, got %+v", got)
	}
}

func TestSubManifest_RoundTripsForRestore(t *testing.T) {
	item := &closeevent.ClosedItem{
		SessionName: "lazytmux",
		Window: &snapshot.Window{
			Index: 2, Name: "logs",
			Panes: []snapshot.Pane{{Index: 1, Command: "fish", Cwd: "/y"}},
		},
		WindowIndex: 2,
	}
	m := item.SubManifest("h", 100)
	if len(m.Sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(m.Sessions))
	}
	if m.Sessions[0].Name != "lazytmux" {
		t.Errorf("got session name %q, want lazytmux", m.Sessions[0].Name)
	}
	if len(m.Sessions[0].Windows) != 1 || m.Sessions[0].Windows[0].Index != 2 {
		t.Errorf("got windows %+v, want one window with Index=2", m.Sessions[0].Windows)
	}
}
