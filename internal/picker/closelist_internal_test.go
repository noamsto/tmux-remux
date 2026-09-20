package picker

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/snapshot"
)

// The close list reads typed fields off the window the event took down, so it
// needs that window — closedPaneInfo only reaches the pane.
func TestClosedWindow(t *testing.T) {
	cc := CloseContext{
		Placement: ClosePlacement{Scope: "window", WindowIndex: 3},
		SubManifest: snapshot.Manifest{Sessions: []snapshot.Session{{
			Name: "s",
			Windows: []snapshot.Window{{
				Index:      3,
				Name:       "w",
				Decoration: map[string]string{"@issue_id": "ENG-8224"},
				Panes:      []snapshot.Pane{{Index: 0, Command: "claude"}},
			}},
		}}},
	}
	w := closedWindow(cc)
	if w == nil {
		t.Fatal("closedWindow returned nil for a window-scope close")
	}
	if got := w.Decoration["@issue_id"]; got != "ENG-8224" {
		t.Errorf("Decoration[@issue_id] = %q, want %q", got, "ENG-8224")
	}
}

func TestClosedWindow_NilWhenAbsent(t *testing.T) {
	if w := closedWindow(CloseContext{}); w != nil {
		t.Errorf("closedWindow(zero) = %v, want nil", w)
	}
}
