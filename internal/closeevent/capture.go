// Package closeevent records tmux close hooks (pane/window/session) as events
// and resolves them against pre-close snapshots for undo/restore.
package closeevent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/noamsto/tmux-state/internal/store"
)

const dedupWindow = 2000 * time.Millisecond

// Args bundles the parameters of a tmux close hook.
type Args struct {
	Kind      string // "pane-died" | "window-unlinked" | "session-closed"
	SessionID string
	WindowID  string
	PaneID    string
	Host      string
	// Index is the live tmux structure queried AFTER the close (the closed
	// entity is already gone when the hook fires). Empty when the server is
	// unreachable — i.e. the last session closed and nothing survived.
	Index IndexPost
}

// Capture inserts a close event into the store unless a fresh outer-scope
// event for the same session exists (cascade dedup). Returns the inserted
// event id, or 0 if deduped.
func Capture(ctx context.Context, db *store.Store, a Args) (int64, error) {
	// window-unlinked also fires on move-window, where the window survives under
	// another session. When the closed entity's id is still in the post-close
	// index nothing was lost, so drop it at the source rather than storing a row
	// the picker can only render as "no recoverable entity".
	if entityStillLive(a) {
		return 0, nil
	}

	now := time.Now().UnixMilli()
	cutoff := now - dedupWindow.Milliseconds()

	if a.Kind != "session-closed" {
		evs, err := db.ListEvents(ctx, store.ListOpts{
			Kinds: []string{"session-closed"},
			Limit: 5,
		})
		if err != nil {
			return 0, err
		}
		for _, ev := range evs {
			if ev.Ts >= cutoff && eventReferencesSession(ev.ManifestJSON, a.SessionID) {
				return 0, nil
			}
		}
	}
	if a.Kind == "pane-died" {
		evs, err := db.ListEvents(ctx, store.ListOpts{
			Kinds: []string{"window-unlinked"},
			Limit: 5,
		})
		if err != nil {
			return 0, err
		}
		for _, ev := range evs {
			if ev.Ts >= cutoff && eventReferencesWindow(ev.ManifestJSON, a.SessionID, a.WindowID) {
				return 0, nil
			}
		}
	}

	wrapped, err := json.Marshal(CloseManifest{
		SessionID: a.SessionID,
		WindowID:  a.WindowID,
		PaneID:    a.PaneID,
		Index:     a.Index,
	})
	if err != nil {
		return 0, err
	}

	return db.InsertEvent(ctx, store.Event{
		Ts:           now,
		Kind:         a.Kind,
		Scope:        scopeFor(a.Kind),
		Reason:       "hook",
		Host:         a.Host,
		ManifestJSON: string(wrapped),
	})
}

// entityStillLive reports whether the close event's target still appears in the
// post-close index, meaning it was not actually closed (e.g. a moved window).
// Mirrors the id-still-present checks in findClosedWindow/findClosedPane.
func entityStillLive(a Args) bool {
	switch a.Kind {
	case "window-unlinked":
		if a.WindowID == "" {
			return false
		}
		for _, w := range a.Index.Windows {
			if w.ID == a.WindowID {
				return true
			}
		}
	case "pane-died":
		if a.PaneID == "" {
			return false
		}
		for _, p := range a.Index.Panes {
			if p.ID == a.PaneID {
				return true
			}
		}
	}
	return false
}

func scopeFor(kind string) string {
	switch kind {
	case "session-closed":
		return "session"
	case "window-unlinked":
		return "window"
	default:
		return "pane"
	}
}

type envelope struct {
	SessionID string `json:"session_id"`
	WindowID  string `json:"window_id"`
}

func eventReferencesSession(manifest, sessionID string) bool {
	if sessionID == "" {
		return false
	}
	var e envelope
	if json.Unmarshal([]byte(manifest), &e) != nil {
		return false
	}
	return e.SessionID == sessionID
}

func eventReferencesWindow(manifest, sessionID, windowID string) bool {
	if windowID == "" {
		return false
	}
	var e envelope
	if json.Unmarshal([]byte(manifest), &e) != nil {
		return false
	}
	return e.SessionID == sessionID && e.WindowID == windowID
}
