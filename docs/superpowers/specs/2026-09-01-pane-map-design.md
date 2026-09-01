# Pane map: draw a window's real layout in the picker

Issue: [#91](https://github.com/noamsto/tmux-remux/issues/91).
Closes [#85](https://github.com/noamsto/tmux-remux/issues/85) as a side effect
of the narrow-width work below.

## Problem

The picker's Contents tree renders every pane through

```go
fmt.Sprintf("%-7s %s", cmd, cwd)
```

so a three-pane window in one repo reads as three near-identical rows that
differ only by a truncated path:

```
• fish    ~/Data/git/.worktrees/noamsto/tmux…
• fish    ~/Data/git/.worktrees/noamsto/tmux…
• tmux    ~/Data/git/.worktrees/noamsto/tmux…
```

Nothing conveys the shape of the window. Deciding whether a snapshot is the one
you want, or which closed pane to bring back, is a recognition task, and people
recognise their workspace by its layout long before they read a command name.

The close picker made this sharper. Since #87 it can preview a closed pane's
scrollback, which answers "what was in this pane" — but only one pane at a
time, and only after arrowing onto it. The window as a whole is still invisible.

## Verified: the layout string already carries the geometry

`Window.Layout` is stored in every manifest and is currently only replayed into
`select-layout` during restore. It encodes **absolute** coordinates per pane, so
no split-tree reconstruction is needed to draw it. Parsed from a real snapshot
of the demo scene:

```
1cb4,145x36,0,0[145x18,0,0,0,145x17,0,19{72x17,0,19,1,72x17,73,19,2}]

window 145x36
  pane 0: 145x18 at (0,0)     full-width top
  pane 1:  72x17 at (0,19)    bottom left
  pane 2:  72x17 at (73,19)   bottom right
```

Those three rectangles match the scene they came from. The grammar that matters
is small: a tuple `WxH,X,Y,N` is a pane, while `WxH,X,Y` followed by `[` or `{`
is a container whose children follow. Only the pane tuples are needed, because
their coordinates are already absolute.

Consequences worth stating plainly: no new capture, no schema change, no
migration, and every snapshot already on disk can be drawn.

## Design

### `internal/panemap`

One job — turn a layout string into box art.

| function | contract |
|---|---|
| `Parse(layout string) (Grid, error)` | `Grid{W, H int; Panes []Rect{Index, W, H, X, Y}}` |
| `Render(g Grid, w, h int, label func(int) string, marked func(int) bool) string` | box art scaled into `w`×`h` |

`label` and `marked` are callbacks so the package stays free of snapshot,
picker, and close-event concepts. It never imports them; it takes rectangles and
returns a string, which is also what makes it testable without a tmux server.

The picker supplies `label` from the manifest's panes (index plus command) and,
in close mode, supplies `marked` from the close event's `PaneID`.

### Picker integration

`renderPreview` already has a branch for a selected node that is not a pane:

```go
if n.Kind != NodePane {
    return frame.Render(rowDim.Render("(press → to expand, ↑↓ to find a pane)"))
}
```

That dead string becomes the map for `NodeWindow`. Pane nodes keep rendering
scrollback exactly as they do now, so #87's behaviour is untouched.

Both pickers gain the map from this one change, because close mode already
shares `renderTree` and `renderPreview` with snapshot mode. In the close picker
the pane that died is drawn with a dashed border, which turns "what did this
window look like" into "which pane am I getting back". For a window- or
session-scope close every box is marked, since the whole thing came down.

### Narrow widths

Below the three-column threshold the map/scrollback panel stacks *under* the
tree instead of being dropped. `View()` grows a stacked plan alongside its
column plan; `paneWidthsThree` keeps its current contract for the column case.

This is the only structural rework in the design, and it is what closes #85: the
preview stops being a feature only visible on a terminal wider than ~134
columns, which is what the 90%-width popup demands today.

### Degradation

In order, so that a small panel is never worse than the text it replaces:

- A panel below `minMapWidth`×`minMapHeight` renders a one-line summary
  (`3 panes · 145×36`) rather than unreadable art. Those are named constants
  starting at 24×6, adjusted once there is something on screen to judge.
- A box too small for `2 agent-work` shows the index alone; too small for that,
  it stays empty. The box is still drawn, so the shape survives.
- An absent or unparsable layout falls back to today's hint text. Nothing
  regresses for a snapshot written before this change.
- Scaling rounds onto a character grid with deduplicated borders, so rounding
  cannot make boxes overlap or draw a doubled edge.

## Testing

- **Parser:** table tests over real layout strings — nested, single pane,
  zoomed, and malformed input that must return an error rather than a partial
  grid.
- **Renderer:** exact-string assertions on small grids. Deterministic, so no
  golden files; a wrong border or an off-by-one column fails loudly.
- **Picker:** a window node's panel contains the map; a pane node's panel still
  contains scrollback; a close event's map marks the pane that died.
- **Layout:** the stacked plan is chosen below the threshold and the column plan
  above it.

## Out of scope

Mouse selection or click-to-focus a box, colour-coding boxes by command, and a
session-level map spanning several windows. Session nodes keep today's
behaviour; the map is per-window.
