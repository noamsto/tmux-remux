# Column grid for the close list row layout

**Issue:** noamsto/tmux-remux#142
**Date:** 2026-09-17
**Status:** Design approved, pending implementation plan

## Problem

The close list row has two anchored positions — the scope glyph at the far
left and the age at the far right — and everything between them floats.

**Sparse.** `cwdColumnWidth` reserves `innerWidth/4` (capped at 24, dropped
below 8) whenever *any* row in the visible list carries a non-modal cwd.
`fitCwd` then pads that full width into every row, including the rows that
elided theirs. `newCloseListView` elides a row's cwd when it matches the
session's modal cwd, which is the common case — so the column is sized for the
exception and billed to the rule. A section whose rows all sit at the modal cwd
renders a blank 24-cell gutter.

**Uncolumned.** `layoutRow`'s `build` closure joins the remaining fields with
single spaces:

```go
cols = append(cols, name)      // variable width
cols = append(cols, extra...)  // 0-3 items, variable width
return strings.Join(append(cols, target), " ")
```

So `→ session:index` — the field that says where a row restores to, and the
most repeated shape in the list — lands at a different column on every row.

**Untyped.** `name` is `snapshot.StripFormat(r.Placement.WindowName)`: one
opaque string that lazytmux has already composed from a ticket id, a title, and
an agent glyph run. remux cannot align, colour, or prioritise its parts because
it never sees them as parts. `clipName` then cuts that string from the left, so
a squeezed row keeps branch prose and loses the discriminating head.

**Shed order is wrong.** The ladder drops `extra` — which carries the closed
pane's command — before clipping the name to 4 cells:

```go
if lipgloss.Width(left) > avail && len(extra) > 0 {
    left = build(name, 0, nil)   // loses "claude" / "codex"
    extra = nil
}
if lipgloss.Width(left) > avail {
    left = build(clip(left, 4), 0, extra)   // keeps "…ocal"
}
```

## Goal

Give the row an explicit column model: fields occupy declared columns that
auto-size to the content actually present, shed in a declared order, and carry
typed meaning sourced from tmux window options rather than parsed out of a
rendered display string.

## Non-goals

- **Parsing the rendered window name.** Close events already in the store were
  captured with the narrow `DecorationOptions` allow-list and have no typed
  fields. They degrade to a single text column showing the name, as today, and
  age out on their own. No parser is written, on any path.
- **Backfilling old events from the live server.** A close list is mostly dead
  windows; the hit rate would be near zero for exactly the rows that need it.
- **PR state colouring.** Needs `@pr_check_state` alongside `@pr_number`. It is
  a config-only follow-up once the column spec exists, not part of this change.
- **Any lazytmux change.** It already stamps every field as a window option.

## Design

### Ownership of the row

The row's fields split cleanly by who produces them, and this split is what
makes the seam possible:

| Field | Owner | Source |
|---|---|---|
| scope glyph, `→ target`, age, `×N` | remux | `CloseRow` |
| pane command, `(gone)`, pane count | remux | sub-manifest + live session set |
| cwd tail | remux | sub-manifest, elided against the session's modal cwd |
| issue id, PR number, title | lazytmux | `@`-options on the window |

Note the pane command is remux's own data. The `claude` / `codex` /
`cursor-agent` text in the current rendering is `extra[0]`, already coloured by
`closeRowCmd` — it needs no decoration support.

### Three layers

**Capture** — unchanged mechanism. `Config.DecorationOptions` is an allow-list
of `@`-options read in `internal/tmux`'s window format and stored in
`Window.Decoration`, which already round-trips through the sub-manifest JSON and
is replayed on restore.

The one change: `DecorationOptions` becomes the **union** of its own restore
allow-list and the column spec's option names, so declaring a column guarantees
its option is captured. It must stay a union and not become derived-from-columns:
`@crew_name`/`@crew_color` are captured for *restore fidelity* and are not
rendered as columns, so deriving the allow-list from the column spec alone would
silently stop restoring persona decoration — the entire point of
`docs/superpowers/specs/2026-07-06-decoration-restore-design.md`.

**Spec** — `internal/config` gains:

```go
type ColumnRole int

const (
    RoleID    ColumnRole = iota // compact identity, left-anchored ("ENG-8224", "#56")
    RoleBadge                   // short marker, right of the flex ("#3511")
    RoleText                    // prose; the flex column
)

type DecorationColumn struct {
    Option string     // "@issue_id" — the only place this string appears
    Role   ColumnRole
    Max    int        // hard cap in cells
}
```

Parsed from a new global tmux option `@remux_columns`, following the
`@remux_ascii_glyphs` precedent in `internal/picker/theme.go`:

```tmux
set -g @remux_columns '@issue_id:id:10,@pr_number:badge:6'
```

Format is `option:role:max` triples, comma-separated. A malformed entry is
skipped rather than failing the picker — an unparsable column should cost that
column, not the list.

The compiled default is **no decoration columns**. remux is a published plugin
and must not ship another tool's schema; an unconfigured install renders exactly
today's fields, correctly columned. Noam's spec lives in nix-config's tmux.conf.
This is not the rejected "tmux option only" shape: `DecorationOptions` keeps its
own non-empty default, so restore fidelity is unaffected by an empty column spec.

`Max` applies to `RoleID` and `RoleBadge`. `RoleText` is the flex column and
ignores `Max`; its width comes from the space left over, floored at 12.

**Render** — `internal/picker/rowgrid.go`, new and pure: no tmux, no store, no
lipgloss frames. It takes cells plus roles and returns aligned lines. It
switches on `ColumnRole` and never on an option name.

### Column model

```
[glyph] [cwd] [id] [title ···flex··· ] [badge] [cmd] [→ target] [age]
  1cell  auto  auto        flex          auto   auto     auto    fixed
```

- **auto** — width is the widest value *actually present in the visible list*,
  and **0 when no row has one**, which removes the column and its separator
  entirely. This is the general form of the cwd fix: the gutter disappears by
  the same rule that sizes every other column, rather than by a special case.
  The cwd column additionally caps at `min(innerWidth/4, 24)` and is fitted
  with `fitCwd`, which snaps a cut to a path boundary. Without the cap a single
  34-cell outlier path costs *every* row its cwd at `closeListMin` — and that
  floor was calibrated, per its own comment, on a list whose rows carry every
  column including the cwd tail.
- **flex** — the title absorbs all remaining space and is the first to shrink.
  Its value is the declared `RoleText` column's option when that column is
  configured and the row has a value for it; otherwise the window name. So a
  configured install shows `@issue_title` and an unconfigured one shows the name,
  through the same column — there is no second code path for the fallback.
- The `cmd → target` pair and `age` right-anchor as one block. `cmd → target`
  keeps an idiom already present in the rendering (`claude → nix-config:2`),
  and anchoring the pair means the arrow lands in one place on every row.

### Shed order

Declared as data, replacing the implicit ladder:

1. cwd drops
2. title shrinks to its floor (12 cells)
3. badge drops
4. cmd drops
5. title clips below its floor
6. target clips from the left (`…nix-config:2`)

The cwd yields *whole* before the title loses a cell. That is the existing
policy pinned by `TestCloseListRow_CwdYieldsBeforeTheName`, and an earlier
draft of this spec inverted it. The title is the row's primary identifier; the
cwd column only appears at all when a row's cwd is surprising, so it is the
cheaper thing to give up.

Age never drops: it is three cells and it is the list's sort key. This ordering
fixes the inversion noted above — the title is squeezed before the command is
discarded.

### Degradation for pre-existing events

A row whose window carries no `Decoration` yields no id and no badge cells. If
*no* row in the list has them, those columns are zero-width and the row is
`glyph | cwd | title | cmd → target | age` — the current shape, minus the
sparse gutter, plus an anchored right edge. A mixed list shows real columns for
new closes and a long title for old ones. It self-heals as events age out.

## Testing

`rowgrid` is pure, so it is table-driven across many widths:

- every emitted line is exactly `innerWidth` cells
- a column whose values are all empty occupies zero cells, separator included
- columns align across rows of a list
- the shed order holds: at each width, the field that disappears is the next
  one in the declared sequence
- target clips from the left, never the right

Seam guard, as a real test: walk `internal/picker`'s source and assert no
`@issue_*` or `@pr_*` literal appears. The decoupling claim then cannot rot
silently.

Existing frame-geometry tests (`TestRenderCloseList_NeverOverflowsFrame`,
`TestRenderClosePreview_NeverOverflowsFrame`) stay as the integration guard.

Config tests cover `@remux_columns` parsing: well-formed spec, malformed entry
skipped, empty option, and the derived `DecorationOptions` union.

## Blast radius

| Package | Change |
|---|---|
| `internal/config` | new types + `@remux_columns` parser; `DecorationOptions` derived |
| `internal/picker/rowgrid.go` | new — column and shedding engine |
| `internal/picker/view.go` | `layoutRow`, `cwdColumnWidth`, `fitCwd` deleted |
| `internal/picker/closelist.go` | `closedWindow(cc)` helper beside `closedPaneInfo` |
| `internal/snapshot` | none |

Net line count is expected to be negative in `view.go`.

## `clipName`'s left-cut heuristic

`clipName`/`hasGlyphRun` cut a name from the left when it carries a Nerd Font
glyph run, because the run identifies the window and the prose head is the
affordable half. That reasoning holds only while the glyph run is inside the
title.

Keep the helper as-is. The flex column feeds it whatever it holds, so a
configured row (plain `@issue_title`, no glyph run) takes the right-cut branch
and a degraded row (full window name, glyph run present) takes the left-cut
branch — which is the correct choice in both cases, with no new conditional.
`hasGlyphRun` already makes that decision per value.
