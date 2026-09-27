// Package closeevent records tmux close hooks (pane/window/session) as events
// and resolves them against pre-close snapshots for undo/restore.
package closeevent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

const dedupWindow = 2000 * time.Millisecond

// Args bundles the parameters of a tmux close hook.
type Args struct {
	Kind        string // "pane-died" | "window-unlinked" | "session-closed"
	SessionID   string
	SessionName string
	WindowID    string
	PaneID      string
	Host        string
	// ServerStarted is the current tmux server's start time in Unix
	// milliseconds, or 0 when unknown. tmux reuses pane and window ids after a
	// restart, so resolution ignores snapshots and close events older than it.
	ServerStarted int64
	// Index is the live tmux structure queried AFTER the close (the closed
	// entity is already gone when the hook fires). Empty when the server is
	// unreachable — i.e. the last session closed and nothing survived.
	Index IndexPost
	// Bridged is the set of sessions that are lazytmux bridge mirrors right
	// now, read from live tmux by the caller alongside Index. Asking tmux
	// rather than the last snapshot matters because the two disagree exactly
	// when it counts: a bridge attached since the last save, or a snapshot
	// written by a build that did not record the field. Either way the mirror
	// is already excluded from snapshots while its closes go on being
	// recorded as restorable.
	Bridged map[string]bool
}

// Capture inserts a close event into the store unless a fresh outer-scope
// event for the same session exists (cascade dedup). Returns the inserted
// event id, or 0 if deduped.
//
// The dedup runs both ways, because tmux's cascade runs inner-first: an outer
// event already stored suppresses this one, and this one retracts the inner
// rows it subsumes. See retractSuperseded.
func Capture(ctx context.Context, db *store.Store, a Args) (int64, error) {
	// after-kill-pane is a command hook, so it carries no hook_pane: a pane
	// killed with prefix+x used to leave no trace at all. Recover its id by
	// diffing the survivors against the last snapshot instead.
	if a.Kind == "pane-died" && a.PaneID == "" {
		resolved, ok, err := resolveKilledPane(ctx, db, a)
		if err != nil || !ok {
			return 0, err
		}
		a = resolved
	}

	// window-unlinked also fires on move-window, where the window survives under
	// another session. When the closed entity's id is still in the post-close
	// index nothing was lost, so drop it at the source rather than storing a row
	// the picker can only render as "no recoverable entity".
	if entityStillLive(a) {
		return 0, nil
	}

	// A lazytmux bridge mirror is left out of every snapshot (always
	// reconstructible from its remote), so nothing inside one ever resolves.
	// Same reasoning as above: drop it at the source. An unnamed session —
	// after-kill-pane carries no hook_session_name — matches nothing and
	// stays on the ordinary resolve path, where the snapshot diff finds no
	// missing pane for a mirror and drops the event anyway.
	if a.Bridged[a.SessionName] {
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

	man := CloseManifest{
		SessionID:   a.SessionID,
		SessionName: a.SessionName,
		WindowID:    a.WindowID,
		PaneID:      a.PaneID,
		Index:       a.Index,
	}
	man.Resolved = resolveAtCapture(ctx, db, a, man)

	wrapped, err := json.Marshal(man)
	if err != nil {
		return 0, err
	}

	id, err := db.InsertEvent(ctx, store.Event{
		Ts:           now,
		Kind:         a.Kind,
		Scope:        scopeFor(a.Kind),
		Reason:       "hook",
		Host:         a.Host,
		ManifestJSON: string(wrapped),
	})
	if err != nil {
		return 0, err
	}

	if err := retractSuperseded(ctx, db, a, cutoff); err != nil {
		return 0, err
	}

	linkResolvedScrollback(ctx, db, id, man.Resolved)
	return id, nil
}

// retractSuperseded deletes the inner-scope events this one subsumes. The
// cascade checks above only look backwards, which never fires for the order
// tmux actually produces: a window's last pane exits *before* the window
// unlinks, and every pane and window goes before the session closes. The inner
// row is therefore already stored by the time the outer event arrives, and
// restoring the outer one recreates what the inner one held — so the list grew
// two rows for a close the reader only made once.
//
// Scrollback rows reference events ON DELETE CASCADE and parent_event_id is ON
// DELETE SET NULL, so a delete here leaves nothing dangling.
func retractSuperseded(ctx context.Context, db *store.Store, a Args, cutoff int64) error {
	var inner []string
	switch a.Kind {
	case "window-unlinked":
		inner = []string{"pane-died"}
	case "session-closed":
		inner = []string{"pane-died", "window-unlinked"}
	default:
		return nil
	}

	evs, err := db.ListEvents(ctx, store.ListOpts{Kinds: inner, Limit: 50})
	if err != nil {
		return err
	}
	var ids []int64
	for _, ev := range evs {
		if ev.Ts < cutoff {
			continue
		}
		match := eventReferencesWindow(ev.ManifestJSON, a.SessionID, a.WindowID)
		if a.Kind == "session-closed" {
			match = eventReferencesSession(ev.ManifestJSON, a.SessionID)
		}
		if match {
			ids = append(ids, ev.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return db.DeleteEvents(ctx, ids)
}

// resolveAtCapture embeds the closed entity in the event at capture time so a
// later read no longer needs a surviving snapshot to resolve it. Returns nil
// whenever embedding isn't safe, leaving the event on the snapshot-diff path.
func resolveAtCapture(ctx context.Context, db *store.Store, a Args, man CloseManifest) *ResolvedClose {
	idKnown := (a.Kind == "pane-died" && a.PaneID != "") || (a.Kind == "window-unlinked" && a.WindowID != "")
	if !idKnown {
		// session-closed is excluded: findClosedSession isn't id-aware and, when
		// no name matches, guesses the sole remaining candidate anyway. Re-derived
		// per read that guess decays as other sessions close; embedded it would be
		// permanent.
		return nil
	}

	snap := preCloseSnapshot(ctx, db, a)
	if snap == nil {
		return nil
	}
	prior := *snap

	// findClosedWindow/findClosedPane only refuse to guess inside their id
	// branch when the prior is also id-aware; an old, id-less snapshot falls
	// through to a positional session:index fallback that, under
	// renumber-windows, can return a SURVIVING entity instead of the closed one.
	switch a.Kind {
	case "pane-died":
		if !priorHasPaneIDs(prior) {
			return nil
		}
	case "window-unlinked":
		if !priorHasWindowIDs(prior) {
			return nil
		}
	}

	item := FindClosed(prior, man, a.Kind)
	if item == nil {
		return nil
	}
	// Before the item is embedded, not after: the SHAs this fills in are what
	// linkResolvedScrollback then pins, which is what keeps the blobs alive
	// once the snapshot that captured them is pruned.
	FillScrollback(ctx, db, item, prior.SavedAt)
	return &ResolvedClose{Item: *item, SavedAt: prior.SavedAt}
}

// linkResolvedScrollback links the embedded entity's panes to the event so
// their scrollback blobs survive the source snapshot being pruned. Failures
// are swallowed: the event row is already committed by this point, and
// capture-event runs from a `run-shell -b` tmux hook, so surfacing an error
// here would print a tmux-remux: error: banner on an ordinary pane close that
// actually succeeded — e.g. a gc racing between the snapshot read and this
// link trips the scrollback_sha foreign key.
func linkResolvedScrollback(ctx context.Context, db *store.Store, id int64, resolved *ResolvedClose) {
	if resolved == nil {
		return
	}
	item := resolved.Item
	// The pane key includes the window index, so a missing window leaves
	// nothing to link regardless of which branch item.Pane took.
	if item.Window == nil {
		return
	}
	panes := item.Window.Panes
	if item.Pane != nil {
		panes = []snapshot.Pane{*item.Pane}
	}
	for _, p := range panes {
		if p.ScrollbackSHA == "" {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", item.SessionName, item.Window.Index, p.Index)
		_ = db.LinkEventScrollback(ctx, id, key, p.ScrollbackSHA)
	}
}

// snapshotScanLimit bounds how far back resolveKilledPane and preCloseSnapshot
// look. A handful of background saves can land inside one close's window; far
// more than this would mean snapshots are arriving faster than hooks run.
const snapshotScanLimit = 100

// recentSnapshots returns the server's snapshots newest-first, skipping any row
// that fails to parse and, when serverStarted > 0, any written before the
// current server incarnation began (tmux ids reset on restart, so an older
// incarnation's ids would otherwise collide). ListEvents already orders by
// ts DESC, id DESC.
func recentSnapshots(ctx context.Context, db *store.Store, serverStarted int64) ([]snapshot.Manifest, error) {
	evs, err := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: snapshotScanLimit})
	if err != nil {
		return nil, err
	}
	snaps := make([]snapshot.Manifest, 0, len(evs))
	for _, ev := range evs {
		var m snapshot.Manifest
		if json.Unmarshal([]byte(ev.ManifestJSON), &m) != nil {
			continue
		}
		if serverStarted > 0 && m.SavedAt < serverStarted {
			continue
		}
		snaps = append(snaps, m)
	}
	return snaps, nil
}

// preCloseSnapshot returns the newest snapshot manifest that still holds the
// entity a close event refers to, or nil. Saves run from backgrounded hooks, so
// the newest snapshot can land after the close and already show the entity
// gone; resolution and embedding both need the snapshot taken just before the
// close, which is the newest one that still contains it.
func preCloseSnapshot(ctx context.Context, db *store.Store, a Args) *snapshot.Manifest {
	if (a.Kind != "pane-died" || a.PaneID == "") && (a.Kind != "window-unlinked" || a.WindowID == "") {
		return nil
	}
	snaps, err := recentSnapshots(ctx, db, a.ServerStarted)
	if err != nil {
		return nil
	}
	for i := range snaps {
		m := &snaps[i]
		for si := range m.Sessions {
			for wi := range m.Sessions[si].Windows {
				w := &m.Sessions[si].Windows[wi]
				if a.Kind == "window-unlinked" && w.ID == a.WindowID {
					return m
				}
				if a.Kind != "pane-died" {
					continue
				}
				for pi := range w.Panes {
					if w.Panes[pi].ID == a.PaneID {
						return m
					}
				}
			}
		}
	}
	return nil
}

// recordedPaneDeaths returns the set of panes this server incarnation already
// recorded a close for. A pane that died earlier and was recorded must not be
// mistaken for the pane an id-less hook just killed.
func recordedPaneDeaths(ctx context.Context, db *store.Store, serverStarted int64) (map[string]bool, error) {
	evs, err := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"pane-died"}})
	if err != nil {
		return nil, err
	}
	recorded := make(map[string]bool, len(evs))
	for _, ev := range evs {
		if serverStarted > 0 && ev.Ts < serverStarted {
			continue
		}
		var m CloseManifest
		if json.Unmarshal([]byte(ev.ManifestJSON), &m) != nil || m.PaneID == "" {
			continue
		}
		recorded[m.PaneID] = true
	}
	return recorded, nil
}

// resolveKilledPane fills in the pane and window ids of an id-less pane-died
// event. A save runs from a backgrounded hook, so the newest snapshot can
// postdate the kill and already show the pane gone; reading only it loses the
// event entirely. Instead, consider every pane this server incarnation has
// snapshotted whose window still exists, drop the ones already recorded, and
// accept the survivor only when exactly one remains. A second unresolved pane
// means either a bulk teardown (which window-unlinked/session-closed record) or
// an earlier death this hook cannot distinguish from its own — guessing would
// restore the wrong pane, and recording nothing beats recording a lie.
func resolveKilledPane(ctx context.Context, db *store.Store, a Args) (Args, bool, error) {
	snaps, err := recentSnapshots(ctx, db, a.ServerStarted)
	if err != nil {
		return a, false, err
	}
	recorded, err := recordedPaneDeaths(ctx, db, a.ServerStarted)
	if err != nil {
		return a, false, err
	}

	live := map[string]bool{}
	for _, p := range a.Index.Panes {
		live[p.ID] = true
	}
	liveWindows := map[string]bool{}
	for _, w := range a.Index.Windows {
		liveWindows[w.ID] = true
	}

	// Newest occurrence wins, so the window id is the one the pane had when it
	// was last seen alive.
	alive := map[string]string{}
	for i := range snaps {
		for j := range snaps[i].Sessions {
			for k := range snaps[i].Sessions[j].Windows {
				w := &snaps[i].Sessions[j].Windows[k]
				for l := range w.Panes {
					if p := w.Panes[l]; p.ID != "" {
						if _, ok := alive[p.ID]; !ok {
							alive[p.ID] = w.ID
						}
					}
				}
			}
		}
	}

	lost := a
	found := 0
	for id, windowID := range alive {
		if live[id] || recorded[id] || !liveWindows[windowID] {
			continue
		}
		found++
		lost.PaneID, lost.WindowID = id, windowID
	}
	if found != 1 {
		return a, false, nil
	}
	return lost, true, nil
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
