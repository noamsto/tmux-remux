# Restore Picker — Tree Preview & Filter Toggles

**Date:** 2026-05-20
**Status:** Approved (design)
**Supersedes:** the fzf wrapper in `internal/picker/picker.go`

## Problem

The current `tmux-state pick` is an fzf wrapper showing one row per event:

```
844    2026-05-20 09:15:52  snapshot         timer
843    2026-05-20 09:14:26  snapshot         hook:window-linked
```

Before hitting enter the user has no way to see *what's in* a snapshot — which sessions, windows, panes, or processes it would recreate. Two real consequences:

1. Users can't tell which snapshot to pick when several look similar by timestamp/reason.
2. `pick` calls `restore.BuildPlan(m, filter.Filter{}, …)` with an **empty** filter, so it restores everything in the manifest unconditionally — even though `restore --auto` applies the smart filter. The two paths produce different results from the same snapshot.

## Goals

- Show what a snapshot contains (sessions → windows → panes) before restoring it.
- Make the smart filter visible and tunable at pick time, so `pick` and `restore --auto` can produce the same results when desired.
- Keep the lazytmux popup wrapper unchanged (`prefix + R`, `prefix + U` still call `tmux-state pick --kind=…`).
- Cancel/no-op semantics stay compatible with the existing popup (exit 130-equivalent → empty selection → no error).

## Non-goals (explicit YAGNI)

- Per-node checkboxes / selective restore. Filters toggle whole categories, not individual panes.
- Snapshot diffing across events.
- Persisting filter defaults to `config.Config`.
- Renaming or labeling snapshots.
- Search/filter typing in the list pane.

## UX

### Snapshot mode — two-pane

```
╭──── Snapshots ─────╮ ╭─── Contents (#844) ──────────╮
│▸ #844 09:15 timer  │ │ ▾ lazytmux (1w)              │
│  #843 09:14 wlink  │ │    ▾ 0: claude (1p)          │
│  #842 23:31 hook   │ │       • zsh    ~/lazytmux    │
│  #840 23:30 timer  │ │ ▾ nix-config (1w)            │
│  ...               │ │    ▾ 0: shell (1p)           │
│                    │ │       • fish   ~/nix-config  │
╰────────────────────╯ ╰──────────────────────────────╯
 [skip idle:◯]  [dedup running:●]  [age<24h:◯]    12 kept / 4 skipped     ↵ restore
```

- Left pane: 50 most recent events (same query as today). Cursor on highlight.
- Right pane: tree of the highlighted snapshot's manifest. Default expansion: sessions expanded, windows expanded, panes collapsed (showing pane count instead). User can drill in.
- Footer: three filter toggles, a kept/skipped counter, and a restore hint.
- Skipped nodes render dim + struck-through; kept nodes use the standard Catppuccin Mocha/Latte palette already used by the lazytmux session picker for visual continuity.

### Close mode — list only

`--kind=close` reuses the same chrome but hides the right pane and the filter toggles. Close events target a single pane/window/session — a tree is overkill and the filter doesn't apply. List pane fills the popup.

### Narrow terminal fallback

If `width < 80` cells, collapse to single pane (list-only). Pressing enter on a row opens a modal-style tree overlay; esc returns to the list. This degrades automatically without a separate code path beyond a width check in the View.

### Keys

| Key | Action |
|---|---|
| `↑/↓` / `j/k` | move cursor in focused pane |
| `→/←` / `l/h` | expand / collapse tree node (tree pane focused) |
| `tab` | switch focus: list ↔ tree |
| `s` | toggle "skip idle shells" |
| `d` | toggle "dedup sessions already running" |
| `a` | toggle "age ≤ 24h" |
| `enter` | restore highlighted snapshot through current filter |
| `?` | help overlay (`bubbles/help`) |
| `esc` / `ctrl-c` / `q` | quit, no-op |

Close mode disables tree keys and `s/d/a`.

## Architecture

### Package layout

```
internal/picker/
  picker.go        → deleted (fzf wrapper)
  model.go         → tea.Model: PickerModel
  tree.go          → pure helpers: BuildTree, FilterDecorate
  view.go          → lipgloss styles + render functions
  keys.go          → key.Binding map + bubbles/help integration
  picker_test.go   → kept; tests rewritten for the new types
cmd/tmux-state/main.go → newPickCmd() wires the model and runs tea.NewProgram
```

### Boundaries

Two units carry most of the logic:

**`tree.go` (pure)** — owns the data transform. No tea, no I/O.

```go
type NodeKind int
const (
    NodeSession NodeKind = iota
    NodeWindow
    NodePane
)

type TreeNode struct {
    Kind     NodeKind
    Label    string        // pre-rendered display label without skip styling
    Ref      any           // *snapshot.Session | *snapshot.Window | *snapshot.Pane
    Children []*TreeNode
    Skipped  bool          // set by FilterDecorate
    SkipReason string      // "idle shell" | "running" | "" (empty when kept)
}

func BuildTree(m snapshot.Manifest) *TreeNode
func FilterDecorate(root *TreeNode, f filter.Filter, runningSessions map[string]bool) (kept, skipped int)
```

Critically, `FilterDecorate` calls into the existing `filter.Filter` methods (`SkipSession`, `SkipWindow`, `SkipPane`). The picker can never drift from `restore --auto` because both code paths go through the same predicate.

**`model.go`** — owns TUI state only.

```go
type PickerModel struct {
    mode        string                  // "snapshot" | "close"
    events      []store.Event
    cursor      int
    manifests   map[int64]snapshot.Manifest  // lazy parse cache, keyed by event ID
    trees       map[int64]*TreeNode          // lazy build cache, keyed by event ID
    filter      filter.Filter
    runningSet  map[string]bool         // tmux ListSessions snapshot at boot
    keys        keyMap
    help        help.Model
    width, height int
    focus       focusZone               // list | tree
}
```

`PickerModel.Update` returns one of three things on `enter`:
- `tea.Quit` with `model.selectedID` set when the user confirms restore.
- `tea.Quit` with `selectedID == 0` on cancel (esc/ctrl-c/q).
- Stays in-place when the highlighted event has an unparsable manifest (footer shows the parse error briefly).

The `cmd/tmux-state` wiring runs the program, then — if a non-zero `selectedID` came back — calls `restore.BuildPlan(manifest, model.Filter(), nil, buildOpts)` followed by `restore.Apply(ctx, t, plan)`. The actual restore stays outside the Tea model so it doesn't have to swallow tmux I/O.

### Data flow

1. **Boot**: `db.ListEvents({Limit: 50, kind filter})` → `events`. `t.ListSessions(ctx)` → `runningSet`. Parse the first event's manifest synchronously so the right pane has something to render.
2. **Cursor move**: if the new highlight's manifest isn't cached, parse it lazily (cache by event ID); rebuild + decorate tree.
3. **Filter toggle**: mutate `filter`, re-run `FilterDecorate` on the current tree (no DB hit, no re-parse).
4. **Enter**: model exits with `selectedID`. Caller runs `BuildPlan` + `Apply`. Errors surface to stderr as today.
5. **Cancel**: model exits with `selectedID == 0`. Caller returns nil — same path as fzf exit-130 today.

## Filter defaults

| Toggle | Default | Notes |
|---|---|---|
| skip idle shells | off | Matches today's `pick` behavior (everything restores). |
| dedup running | **on** | Almost always wanted — avoids spawning a duplicate of a session already attached. |
| age ≤ 24h | off | List dims older snapshots when on; tree pane unaffected. |

Filter state is session-only. No persistence to config. If usage patterns reveal a consistent preference we'll revisit and read from the `config.Config` block that already drives `restoreMode = auto`.

## Edge cases

| Case | Behavior |
|---|---|
| No events in DB | List pane shows `No snapshots yet — run \`tmux-state save\`.` Right pane empty. |
| Manifest fails to parse | Tree pane shows `(invalid manifest)`. Restore disabled for that row with footer warning. Other rows remain selectable. |
| Snapshot has zero sessions | Caller should never have saved this (per `save.go` empty-manifest guard), but defensive: render `(empty snapshot)`, restore disabled. |
| `tmux` not running on restore | `restore.Apply` already handles spawning. Errors surface as today. |
| Width < 80 cells | Tree pane hidden until the user hits enter; opens as a modal overlay; esc returns to list. |
| SIGINT / ctrl-c | `tea.Quit` with empty selection; caller returns nil. Lazytmux popup closes cleanly. |

## Testing

**Pure helpers (high coverage):**

- `TestBuildTree` — golden tests over fixture manifests already in `internal/snapshot/testdata/`. Verifies node structure, labels, counts.
- `TestFilterDecorate` — table tests with several `filter.Filter` configurations and expected `Skipped`/`SkipReason` per node.
- `TestFilterDecorate_RunningSet` — `dedup running` correctly skips sessions whose names appear in the running set.

**Model (lighter, transcript-style):**

- `TestModel_ToggleFilterUpdatesCounter` — synthesize key events through `Update`, assert footer counter changes.
- `TestModel_EnterOnInvalidManifestIsBlocked` — model with one parse-error row; pressing enter doesn't set `selectedID` and the footer warning appears.
- `TestModel_TabSwitchesFocus` — focus zone alternates on tab.

If `teatest` (from `charm.land/x/teatest` or similar) is straightforward to add, use it for the model tests. If not, drive `Update` directly with synthetic `tea.KeyMsg` values — the v2 API exposes this cleanly.

**Integration:**

No new integration test for the picker itself (it's a TUI loop). The restore path it triggers is already covered by `integration_test.go`.

## Dependencies

Add to `go.mod` (matching lazytmux's picker):

- `charm.land/bubbletea/v2`
- `charm.land/bubbles/v2` (for `list`, `help`, `key`)
- `charm.land/lipgloss/v2`

No new system requirements. The fzf binary dep is dropped from the picker (the project allowlist may still reference `fzf` elsewhere; check before removing).

## Migration

1. Implement the new picker behind the same `pick` subcommand. Delete `internal/picker/picker.go` (`Pick` + `FormatRow`).
2. Update `newPickCmd` in `cmd/tmux-state/main.go` to construct the model, run `tea.NewProgram`, then run `BuildPlan`/`Apply` with the model's filter.
3. The lazytmux wrapper bindings (`bind R …`, `bind U …`) are unchanged. The `env -u FZF_DEFAULT_OPTS` prefix becomes unnecessary but harmless; leave it for one cycle to minimize churn in the lazytmux config, then strip in a follow-up.

## Open questions for implementation

- `runningSet` is captured once at boot. Worth refreshing if the popup is open for a long time? Probably not — the popup is modal and short-lived.
- Tree expansion state across cursor moves: persist per-event or reset to default each time? Default plan: reset to default (sessions + windows expanded, panes collapsed) — keeps mental model simple.
- Help overlay: use `bubbles/help` short/full views or a custom keymap renderer? Lean on `bubbles/help` if it composes cleanly with the two-pane layout; otherwise inline.
