# Capture & restore per-window decoration options

**Issue:** noamsto/tmux-state#16
**Date:** 2026-07-06
**Status:** Design approved, pending implementation plan

## Problem

Fan-out orchestrators (e.g. the `dispatcher` Claude command) tag a window with
per-agent decoration by setting custom tmux options:

- `@crew_name` — agent codename shown as a leading badge in the status bar.
- `@crew_color` — a tmux colour (e.g. `colour141`) that tints the window's
  badge, index, and label.

lazytmux's status-format reads these options to render the tint; the window
*name* itself is derived each status tick via `automatic-rename-format`, so the
name is not where the color lives.

On a `tmux-state` restore, windows are recreated fresh (`new-window`,
`split-window`). None of these custom options carry over, and nothing
re-stamps them — the orchestrator that set them (a transient dispatcher
session) is long gone. The persona decoration is therefore lost after restore.

## Goal

Snapshot a configured allow-list of tmux options per window (and per pane where
relevant) and re-apply them on restore, so decoration survives a server
restart. The mechanism must be generic — `@crew_name`/`@crew_color` are the
initial defaults, not hardcoded.

## Non-goals

- Restoring window *names* with embedded `#[fg=...]` format directives — names
  already re-derive via `automatic-rename` reading the restored options.
- Capturing arbitrary global tmux options or theme state — only the configured
  per-window/per-pane allow-list.
- Any change to the `@ts_relaunch` relaunch mechanism.

## Design

### Configuration

Add to `internal/config/config.go`:

```go
// DecorationOptions is the allow-list of tmux user/style options snapshotted
// per window and re-applied verbatim on restore. Restores persona decoration
// (agent codename, tint) that a fan-out orchestrator stamped but that nothing
// re-derives after a server restart.
DecorationOptions []string
```

Default in `Default()`:

```go
DecorationOptions: []string{"@crew_name", "@crew_color"},
```

All defaults are window-scoped. The design keeps room for pane-scoped options
(see Capture), but the initial allow-list has none.

### Capture

`internal/tmux/client.go` builds `-F` format strings for `list-windows` /
`list-panes`. The allow-list is dynamic, so the format string is composed at
runtime rather than being the current `const`.

- Append one `#{@opt}` field per allow-listed option to `windowFormat`, each
  separated by `FieldSep`.
- The number of decoration fields is known from `len(DecorationOptions)`, so
  the parser splits the fixed leading fields, then reads the trailing N as
  decoration values zipped against the option names.
- Empty values (option unset on that window) are dropped — not stored.

`internal/tmux/parse.go`:

- `WindowRow` gains `Decoration map[string]string`.
- `ParseWindows` takes the ordered option-name list so it can label the
  trailing fields. (Threading the names in keeps `parse.go` free of config
  knowledge.)

`internal/snapshot/manifest.go`:

- `Window` gains `Decoration map[string]string \`json:"decoration,omitempty"\``.
- `Fingerprint()` zeroes `Decoration` alongside `LastAttached`/`LastUsed`, so a
  color change alone does not trip a new snapshot.

`internal/snapshot/build.go`:

- Copy `w.Decoration` into the `Window` it constructs.

### Restore

`internal/restore/plan.go`:

- New action:

  ```go
  // SetOption applies one tmux option to a restored window or pane via
  // `set -w`/`set -p`. Emitted per captured decoration option.
  type SetOption struct {
      Target string // <session>:<window_index>
      Pane   bool   // set -p vs set -w
      Name   string // e.g. "@crew_color"
      Value  string
  }
  ```

- In `BuildPlan`, after the `CreateWindow` action and before `SetLayout`, emit a
  `SetOption` for each entry in the window's `Decoration` map. Order within the
  map is non-deterministic; sort keys for a stable plan (aids testing).

`internal/restore/apply.go`:

- Type-switch case for `SetOption` runs `set` with `-w` or `-p`, `-q` (quiet),
  and `-t <target>`. Errors swallowed like other actions.

### Why options, not names

lazytmux keeps `automatic-rename on` (re-asserted every tick in
`tmux-update-icons.sh`). The window name is rebuilt from a format that reads
`@crew_*` and other options. So once the options are restored, the tint and
badge reappear on the next status tick with no name manipulation. This is why
capturing the options is sufficient and name-format handling is a non-goal.

## Data flow

```
save:    list-windows -F "...#{@crew_name}\x1f#{@crew_color}"
           -> ParseWindows(names) -> WindowRow.Decoration
           -> Build -> Window.Decoration -> manifest JSON

restore: manifest -> BuildPlan
           -> CreateWindow, [SetOption per decoration key], SetLayout
           -> Apply: set -wq -t <win> @crew_color colour141
           -> next status tick: lazytmux format re-renders the tint
```

## Testing

- `parse_test.go`: `ParseWindows` with 0, 1, and N decoration options; unset
  option yields no map entry; extra `FieldSep` handling stays correct.
- `manifest_test.go`: `Fingerprint()` identical when only `Decoration` differs.
- `plan_test.go`: `BuildPlan` emits sorted `SetOption` actions between
  `CreateWindow` and `SetLayout`; none emitted when `Decoration` is empty.
- `apply_test.go` / fake runner: `SetOption{Pane:false}` runs `set -wq`.
- `integration_test.go`: create a window, `set -w @crew_color colour141`,
  snapshot, restore into a fresh server, assert the option is present on the
  restored window.

## Open items

- Confirm no allow-listed option needs pane scope in practice; if one does, the
  same pattern extends to `paneFormat`/`PaneRow`/`Pane` with `SetOption{Pane:true}`.
