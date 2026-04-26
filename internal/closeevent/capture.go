package closeevent

import (
	"context"
	"encoding/json"
	"fmt"
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
}

// Capture inserts a close event into the store unless a fresh outer-scope
// event for the same session exists (cascade dedup). Returns the inserted
// event id, or 0 if deduped.
func Capture(ctx context.Context, db *store.Store, a Args) (int64, error) {
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

	manifest, err := GetIndex(ctx, db.DB(), a.SessionID)
	if err != nil {
		return 0, err
	}
	if manifest == "" {
		manifest = "{}"
	}
	wrapped := fmt.Sprintf(`{"session_id":%q,"window_id":%q,"pane_id":%q,"index":%s}`,
		a.SessionID, a.WindowID, a.PaneID, manifest)

	id, err := db.InsertEvent(ctx, store.Event{
		Ts:           now,
		Kind:         a.Kind,
		Scope:        scopeFor(a.Kind),
		Reason:       "hook",
		Host:         a.Host,
		ManifestJSON: wrapped,
	})
	if err != nil {
		return 0, err
	}

	if a.Kind == "session-closed" {
		_ = DeleteIndex(ctx, db.DB(), a.SessionID)
	}
	return id, nil
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
