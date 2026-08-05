# Session-aware undo: hierarchical close picker + original-index restore

Issue: [#61](https://github.com/noamsto/tmux-remux/issues/61)

## Problem

Two paths restore a closed entity, and both are session-blind.

`pick --kind=close` (`prefix+U`) lists the last 50 close events as a flat list
ordered by timestamp, all sessions mixed (`cmd/tmux-remux/main.go:440`). Row
labels name their session, but nothing groups by it and nothing indicates which
session the user is actually in.

`undo --pop` (`prefix+u`) restores the *server-wide* newest restorable close
(`restorableClose`, `cmd/tmux-remux/main.go:283`). Pressing it in session A can
resurrect a window that was closed in session B.

Separately, an undone window does not come back where it was. `buildRestorePlan`
runs `reindexIntoLiveSessions` (`cmd/tmux-remux/main.go:376`), which moves a
window whose recorded index is occupied to `max+1` — so it lands at the end of
the session. With `renumber-windows on` the recorded index is almost always
occupied, because closing a window renumbers the survivors into it.

## Verified tmux behavior

Probed on tmux `next-3.8` with `base-index 1`, `renumber-windows on` — the
settings this design targets. These are measurements, not assumptions.

| Command | Precondition | Result |
| --- | --- | --- |
| `new-window -b -t sess:3` | index 3 occupied | new window at 3; former 3 and 4 shift to 4 and 5 |
| `new-window -b -t sess:9` | index 9 free | new window at exactly 9 |
| `new-window -t sess:9` | index 9 occupied | fails: `create window failed: index 9 in use` |
| `#{hook_session_name}` | inside `window-unlinked` | expands to the session name (alongside `hook_session` = `$38`) |
| `#{hook_session_name}` | inside `session-closed` | still expands to the dying session's name |
| `#{hook_session_name}` | inside `after-kill-pane` | **empty** — a command hook gets no `hook_*` formats |
| `#{session_name}` | inside `after-kill-pane` | the session of the client that ran the command, **not** the victim pane's session |

`new-window -b` is therefore a single uniform primitive for "put it back at index
N": it inserts-and-shifts when N is taken and places exactly when N is free.

The `session-closed` row matters because a closed session cannot be looked up by
id afterwards — the name has to be captured at hook time or it is lost. The two
`after-kill-pane` rows rule out naming the session for `prefix+x` kills: the
value is either empty or actively wrong.

## Decisions

**Session-awareness covers both paths, with fallback.** `undo --pop` prefers the
current session's newest restorable close and falls back server-wide when this
session has none, reporting the source session in its message. A strictly
session-scoped `undo` was considered and rejected: silently refusing to restore
anything when a recoverable close exists one session over is worse than
restoring it and saying so.

**The picker renders tmux's own hierarchy, indented.** Session → window → pane,
so a pane-died close nests under the window it died in. Box-drawing guides
(`├─ └─ │`) carry depth rather than whitespace alone, because the list pane is
only ~40% of the popup width and indent-only nesting is what reads as flat
today.

**Numeric insert-and-shift for index restore.** A neighbour-relative placement
(find the window that preceded it in the snapshot, insert after wherever that
window lives now) was considered. It survives hand-renumbering and out-of-order
undos, but needs snapshot-neighbour lookup plus live-id resolution for a case
that does not arise in practice under `renumber-windows on`. Numeric restores the
index the user remembers, with one flag.

**Original-index restore applies only to the single-entity path.** Full snapshot
replay keeps the existing behavior. A mid-plan `-b` insert shifts windows that
later actions in the same plan target by index, which would corrupt a multi-
window restore.

## Component 1: events carry their own session name

Close events currently store `session_id` (`$38`) but not a name
(`CloseManifest`, `internal/closeevent/diff.go:18`). An **unrecoverable** event —
one whose entity no snapshot ever captured — has no resolved `ClosedItem` and
therefore no `SessionName`, so today there is no way to attribute it to a
session. Both the picker's `N unrecoverable hidden` counter and `undo --pop`'s
discard message need per-session attribution to stay truthful.

- `CloseManifest` gains a `SessionName` field with json tag `session_name`.
- `closeevent.Args` gains `SessionName`; `Capture` persists it.
- `pane-exited`, `window-unlinked` and `session-closed` pass
  `--session-name=#{hook_session_name}` in `tmux-remux.tmux` and
  `examples/tmux.conf`. (`install_hook.go` does not emit these hook lines —
  verified — so it needs no change.)
- `after-kill-pane` passes **nothing**, per the probe table: `hook_*` is empty
  there and `#{session_name}` names the client's session rather than the victim
  pane's. Those events are attributed through step 2 below, which
  `resolveKilledPane` already populates by diffing against the last snapshot.

Resolution chain for an event's owning session, first hit wins:

1. `CloseManifest.SessionName` — present on events recorded after this change.
2. `ClosedItem.SessionName` — the snapshot diff, for older recoverable events
   and for every `after-kill-pane` event.
3. Unattributed: the event sorts into `other sessions` under an
   `(unknown session)` header.

Old events keep working through steps 2–3; no migration is required, and the
events table schema is unchanged (the name rides inside `manifest_json`).

A fourth step — mapping the stored `session_id` to a live session name via
`tmux list-sessions` — was considered and rejected. tmux session ids restart at
`$0` with every server, so a `$3` recorded by a dead server can collide with a
different live session's `$3` and attribute a close to the wrong session. It
would also require adding `#{session_id}` to `sessionFormat` and changing
`ParseSessions`. The only events it could help are pre-change unrecoverable
ones, which the picker already hides behind its unrecoverable counter — a
misattribution risk bought for nearly nothing.

## Component 2: grouping — `internal/picker/closetree.go`

Target rendering of the list pane:

```
┌─ closes ──────────────────────────────────┐
│ ▾ this session · tmux-remux               │
│ ├─ ▾ 2: main 🧠 (1p)             14:03    │   window close (selectable)
│ │  └─ pane: nvim                 13:58    │   pane that died inside it
│ └─ ▾ 5: docs · live                       │   header only, window is alive
│    └─ pane: zsh                  14:01    │
│ ▾ other sessions                          │
│ ├─ ▾ lazytmux · gone                      │   header only, no session close
│ │  └─ 0: shell (1p)              11:52    │
│ └─ dispatcher (3w)               11:44    │   session close, no children
└───────────────────────────────────────────┘
```

A new node type, distinct from the snapshot-shaped `TreeNode`. Its rows mean
something different: a row is either a close *event* or a pure grouping
*header*, and there is no filter/skip decoration.

```go
type CloseNodeKind int

const (
    GroupThis CloseNodeKind = iota // "this session · <name>"
    GroupOther                     // "other sessions"
    CSession
    CWindow
    CPane
)

type CloseNode struct {
    Kind     CloseNodeKind
    Label    string
    Ts       int64  // 0 = pure header, no event of its own
    EventID  int64  // 0 = not selectable
    State    string // "live" | "gone" | ""
    Parent   *CloseNode
    Children []*CloseNode
    Expanded bool
}

// BuildCloseTree groups recoverable close events into the two-group hierarchy.
// current is the invoking session's name (empty = no session context, in which
// case every session lands in GroupOther). live is the set of session names
// currently running.
func BuildCloseTree(
    evs []store.Event,
    ctxs map[int64]CloseContext,
    current string,
    live map[string]bool,
) *CloseNode
```

`CloseContext` (`internal/picker/model.go:80`) gains the placement key that
grouping needs, filled by `buildCloseContexts` from the already-resolved
`ClosedItem`:

```go
type ClosePlacement struct {
    Session     string // resolution chain above
    WindowIndex int    // 0 for session scope
    WindowName  string
    Scope       string // "session" | "window" | "pane"
}
```

Grouping rules:

- Two groups: the current session, then everything else. `this session` starts
  expanded, `other sessions` collapsed.
- Within a group, one node per session; within a session, one node per window;
  pane events hang off their window.
- **Window and session nodes are dual-purpose.** A node created *by* a close
  event carries that event's `Ts` and `EventID` and is selectable. A node that
  exists only to parent something else is a header: `· live` when the window or
  session is still running, `· gone` when it is not and no close event for it
  was recorded.
- A session-closed event can be both selectable and a parent: earlier window
  closes in that session nest beneath it. Cascade dedup
  (`internal/closeevent/capture.go`) suppresses only the 2s window around the
  session close, so older siblings do survive.
- Sort: every subtree orders children by newest descendant timestamp,
  descending. Header nodes inherit their newest descendant's timestamp for
  sorting purposes only.

Ownership boundary: `closetree.go` owns grouping and flattening, `view.go` only
renders, `model.go` only navigates. The picker's contract to `PickCmd` stays
`SelectedID() int64`, so the downstream restore path is untouched.

### Navigation and rendering

`PickerModel.cursor` in close mode indexes the flattened visible-node list
rather than `m.events`. `Up`/`Down` land only on nodes that are selectable or
collapsible — the same rule `isNavTarget` (`internal/picker/model.go:196`)
already applies in the right-hand tree pane. `Left`/`Right` collapse and expand
headers. `Enter` on a header is a no-op with a footer note; `Enter` on an event
node sets `selectedID` exactly as today.

The flattening walker duplicates roughly a dozen lines of `visibleNodes()`.
Keeping them separate is deliberate: the nav semantics differ, and forcing a
shared interface over two small walkers costs more clarity than it saves.

Rendering adds `renderCloseTree` alongside `renderList`. Guide prefixes are
built per depth from the ancestor chain (`│  ` where an ancestor has later
siblings, spaces where it does not). **The guide prefix counts against
`innerWidth` before `ansi.Truncate`** — the frame wraps overflow onto a second
physical line otherwise, which desyncs the one-row-per-node windowing that
`scrollWindow` depends on.

The right-hand contents pane, the footer, and snapshot mode are unchanged.

## Component 3: session-scoped `undo --pop`

The invoking session's name reaches the binary explicitly:

- `bind-key u run-shell "'${bin}' undo --pop --session='#{session_name}'"`
- `bind-key U display-popup -E ... "'${bin}' pick --kind=close --session='#{session_name}'"`

tmux expands formats in `run-shell` and `display-popup` command arguments — the
plugin already relies on this for `#{hook_pane}`. When the flag is absent (a
stale `tmux.conf` that has not been re-sourced), the binary falls back to
`tmux display-message -p '#{client_session}'`, then to no session context at
all, which reproduces today's server-wide behavior.

The fallback is load-bearing, not decorative: installs that wire tmux-remux from
their own config rather than from `tmux-remux.tmux` — a Nix-generated
`tmux.conf`, for instance — will not pass `--session` until that config is
updated separately. Those installs must get session-aware behavior from
`#{client_session}` alone, so the fallback path deserves the same test coverage
as the flag path.

`restorableClose` gains a session parameter:

- Prefer the newest restorable close owned by `session`.
- **Discard only unrecoverable events owned by `session`.** Discarding is a
  garbage-collection of rows that can never become restorable, but scoping it
  matters for honesty: a press in A that silently consumed B's dead rows would
  leave B's own `never made it into a snapshot` message unshown when the user
  later presses `u` there.
- Nothing restorable in this session → fall back to the newest restorable
  anywhere and prefix the result message with `from session <name>`.
- No session context → today's behavior exactly.

The picker opens with the cursor on this session's newest close.

## Component 4: original-index restore

- `restore.CreateWindow` gains `InsertBefore bool`.
- `createWindow` (`internal/restore/apply.go:73`) appends `-b` to its
  `new-window` args when the flag is set.
- `buildRestorePlan` sets it on the single-entity undo / close-pick path only.
- `reindexIntoLiveSessions` is deleted — `-b` subsumes it. Its two call sites
  and its tests go with it.

Old-tmux guard: if `new-window -b` fails as a *usage* error (tmux rejecting an
unknown flag rather than refusing the operation), retry once without `-b`. That
retry lands the window wherever tmux picks, which is the pre-change behavior.
The check keys on tmux's usage-error text, not on a version probe.

### Pane parent-window resolution

`BuildPaneRestore` (`internal/restore/plan.go:212`) branches on `windowLive`,
which `windowLive` (`cmd/tmux-remux/main.go:420`) answers by looking up the
*snapshot's* window id `@N`. A window that was undone is a **new** window with a
new `@id`, so a pane close nested under it reads `windowLive == false`, takes
the `BuildPlan` branch, and recreates the whole window a second time. This is
reachable today via `prefix+U`; nesting panes under windows in the picker makes
it easy to hit.

Resolution becomes a chain: `@id` → `session:name` → `session:index`. A hit at
any level means the parent window is live and the pane is split into it.

This nesting is not cosmetic. A window close resolves against the newest
snapshot *before* it, which no longer contains a pane that died earlier — so the
restored window comes back without that pane, and the nested pane close remains
genuinely restorable afterwards.

## Error handling

Failures degrade to less information, never to a wrong restore.

| Failure | Behavior |
| --- | --- |
| Session name unresolvable for an event | Event groups under `(unknown session)` in `other sessions`; still selectable |
| No session context at all (`--session` absent, `client_session` empty) | Single `other sessions` group; `undo --pop` behaves exactly as today |
| `tmux list-sessions` fails | `PickCmd` already aborts on a hard `ListSessions` error before the picker opens; session attribution itself never consults the live server |
| `new-window -b` rejected as unknown flag | Retry without `-b`; window lands where tmux picks |
| Parent window unresolvable by id, name, or index | Treated as not live: the window is recreated, as today |
| `Enter` on a header node | Footer note, no restore |

## Testing

`internal/picker/closetree_test.go` — table-driven grouping:

- A pane close nests under its parent window node.
- A window close with a pane close inside it: one selectable window node, one
  pane child.
- A live window with only a dead pane: header node marked `· live`, not
  selectable.
- A gone session with no session-close event: header marked `· gone`.
- This-session vs other-sessions split, including the empty-`current` case.
- Sort order by newest descendant, at every level.
- An unrecoverable event attributed by `CloseManifest.SessionName` alone.

`internal/picker/view_internal_test.go` — guide prefixes at depth; a deep node
whose label exceeds the pane width truncates to exactly `innerWidth` including
its prefix; one row per node after rendering.

`cmd/tmux-remux/undo_test.go` — session-preferred target; server-wide fallback
and its message; per-session discard leaving another session's dead rows intact;
no-session-context path matching today's behavior.

`internal/restore/apply_test.go` — `-b` present on the undo path and absent on
snapshot replay; the usage-error retry drops the flag.

`internal/closeevent/capture_test.go` — `session_name` round-trips through
`manifest_json`; an event without it still resolves via the snapshot diff.

Manual verification: close a middle window in a session with `renumber-windows
on`, press `prefix+u`, confirm the window returns to its original index with the
survivors shifted; press `prefix+U` in a session with closes in two sessions and
confirm the grouping and that `Enter` restores the right entity.

## Out of scope

- Snapshot mode (`prefix+R`) grouping and rendering.
- Porting box-drawing guides to the right-hand contents pane. Worth doing so the
  two panes share a visual vocabulary, but it is a separate change.
- Neighbour-relative index placement.
- Restoring a pane to its original position within a window's layout (the
  existing `SetLayout` behavior stands).
