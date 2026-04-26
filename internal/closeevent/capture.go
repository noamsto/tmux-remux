package closeevent

import (
	"context"
	"fmt"
	"strings"
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
			if ev.Ts >= cutoff && containsQuoted(ev.ManifestJSON, a.SessionID) {
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
			if ev.Ts >= cutoff && containsQuoted(ev.ManifestJSON, a.SessionID) && containsQuoted(ev.ManifestJSON, a.WindowID) {
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

// containsQuoted is true if the JSON manifest contains the quoted form of id
// (e.g., id="$1" matches `"$1"` in the JSON). Used as a cheap heuristic to
// determine whether an event references a particular session/window id.
func containsQuoted(manifest, id string) bool {
	if id == "" {
		return false
	}
	target := fmt.Sprintf("%q", id)
	return strings.Contains(manifest, target)
}
