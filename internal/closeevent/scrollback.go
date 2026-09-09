package closeevent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
)

// How far back FillScrollback looks. A save that lands inside
// min_save_interval still records structure — snapshot.Saver downgrades it to
// a scrollback-free snapshot rather than dropping a window or pane that would
// then be unrestorable — and a close resolves against whichever snapshot is
// newest before it, so on a busy server that is usually one of the downgraded
// ones. The span has to clear a run of them: observed on a live server, full
// saves land about once a minute against three downgraded ones, so a quarter
// of an hour clears any plausible run. It stops there because a screen from
// longer before the close is no longer what the pane looked like when it went,
// and the count stops at the snapshot history a prune keeps, past which the
// blob a manifest names is gone anyway.
const (
	scrollbackLookback     = 20
	scrollbackLookbackSpan = 15 * time.Minute
)

// FillScrollback gives every pane of item that carries no scrollback the
// newest blob recorded for that same pane before ts, and reports whether it
// filled any. Best-effort: an unreadable snapshot is skipped, and a pane no
// snapshot in range captured keeps its empty SHA — which the preview reads as
// "nothing captured", the truth once nothing is in reach.
func FillScrollback(ctx context.Context, db *store.Store, item *ClosedItem, ts int64) bool {
	panes := panesMissingScrollback(item)
	if len(panes) == 0 {
		return false
	}
	evs, err := db.SnapshotsBefore(ctx, ts, ts-scrollbackLookbackSpan.Milliseconds(), scrollbackLookback)
	if err != nil {
		return false
	}
	missing := len(panes)
	for _, ev := range evs {
		var m snapshot.Manifest
		if json.Unmarshal([]byte(ev.ManifestJSON), &m) != nil {
			continue
		}
		if missing -= fillFrom(panes, m); missing == 0 {
			break
		}
	}
	return missing < len(panes)
}

// panesMissingScrollback returns the panes of item a preview would draw and a
// restore would replay, minus those that already carry scrollback or have no
// id to match one by. The branch order mirrors SubManifest's: a pane close
// draws the pane that died, not the siblings that survived it.
func panesMissingScrollback(item *ClosedItem) []*snapshot.Pane {
	if item == nil {
		return nil
	}
	var out []*snapshot.Pane
	add := func(panes []snapshot.Pane) {
		for i := range panes {
			if panes[i].ScrollbackSHA == "" && panes[i].ID != "" {
				out = append(out, &panes[i])
			}
		}
	}
	switch {
	case item.Session != nil:
		for i := range item.Session.Windows {
			add(item.Session.Windows[i].Panes)
		}
	case item.Pane != nil:
		if item.Pane.ScrollbackSHA == "" && item.Pane.ID != "" {
			out = append(out, item.Pane)
		}
	case item.Window != nil:
		add(item.Window.Panes)
	}
	return out
}

// fillFrom stamps every still-empty pane in panes with what m recorded for the
// same tmux pane id, and returns how many it filled. The match is on the pane
// id, which is unique for the life of a tmux server: matching on
// session:window:pane numbers would hand a pane the output of whatever pane
// later took its number.
func fillFrom(panes []*snapshot.Pane, m snapshot.Manifest) int {
	byID := map[string]string{}
	for _, sess := range m.Sessions {
		for _, w := range sess.Windows {
			for _, p := range w.Panes {
				if p.ID != "" && p.ScrollbackSHA != "" {
					byID[p.ID] = p.ScrollbackSHA
				}
			}
		}
	}
	filled := 0
	for _, p := range panes {
		if p.ScrollbackSHA != "" {
			continue
		}
		if sha := byID[p.ID]; sha != "" {
			p.ScrollbackSHA = sha
			filled++
		}
	}
	return filled
}
