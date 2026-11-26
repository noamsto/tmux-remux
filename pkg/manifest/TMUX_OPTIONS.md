# tmux Option Contract

This document describes the tmux user options (`@*`) read and written by `tmux-remux`, along with the pane/window ID uniqueness contract for consumers of `pkg/manifest`.

## Bridge options

| Option | Scope | Meaning |
| --- | --- | --- |
| `@bridge_host` | Session or window | Remote host name for a bridge mirror. |
| `@bridge_session` | Session or window | Remote session name for a bridge mirror. |
| `@bridge_placed` | Window | Marks a bridge mirror window placed into a local session (e.g. `og-remote-open --into`). |

- **Dedicated bridge-mirror sessions:** `tmux-remux` reads session-scoped `@bridge_host` via `show-options` (never a `-F` format string, which would inherit into windows). Sessions carrying `@bridge_host` are omitted from `Manifest.Sessions` and recorded in `Manifest.Bridged`.
- **Placed bridge mirrors:** Window-scoped `@bridge_host` / `@bridge_session` / `@bridge_placed` identify renderer windows placed inside a local session. Their renderer windows are omitted from `Session.Windows` and recorded in `Session.PlacedBridges` (`Host`, `RemoteSession`).
- Bridge keys are stripped from `Window.Decoration` and `Pane.Decoration` even if listed in decoration options.

## Relaunch options

| Option | Scope | Meaning |
| --- | --- | --- |
| `@remux_relaunch` | Pane | Full shell command exec'd verbatim on restore, bypassing the command allow-list. Stored in `Pane.Relaunch`. |
| `@remux_relaunch_owner` | Pane | `<pid> <start-time> <stamp>` binding `@remux_relaunch` to its owning process. |
| `@remux_relaunch_pinned` | Pane | When `1`, `tmux-remux relaunch-stamp` (both stamp and `--clear`) is a no-op so external tools can manage `@remux_relaunch` without hook interference. |

- **Owner-bound stamps:** `relaunch-stamp` records `@remux_relaunch_owner` alongside `@remux_relaunch`. At capture time, if the owning process is gone, was replaced (pid reuse caught by `<start-time>`), or no longer runs under the pane, the stamp is dropped.
- **Owner-less stamps:** Hand-set stamps or stamps with `@remux_relaunch_owner` cleared fall back to the idle-shell rule: restore skips the override when the pane's command was a plain shell with no child processes.
- `@remux_relaunch_pinned` is not captured into the manifest; the owning tool is responsible for clearing both `@remux_relaunch` and `@remux_relaunch_pinned` on normal exit (and clearing `@remux_relaunch_owner` when setting the pin).

## Crew & decoration options

`tmux-remux` snapshots local (non-inherited) window and pane options into `Window.Decoration` and `Pane.Decoration`:

- **Window defaults (`@remux_decoration_options`):** `@crew_name`, `@crew_color`, `pane-border-style`, `pane-active-border-style`, `pane-border-format`, `pane-border-status`.
- **Pane defaults (`@remux_pane_decoration_options`):** `@crew_role`, `@crew_role_color`, `@crew_state`, `pane-border-style`, `pane-active-border-style`, `pane-border-format`.
- **Picker columns (`@remux_columns`):** Declares extra `@*` options to render in the close list; captured into `Decoration` only when set locally on the window or pane.

## Pane and window ID uniqueness

`Window.ID` (`@N`) and `Pane.ID` (`%N`) are **only unique within a single tmux server incarnation**. When a tmux server restarts, it resets its ID counters, so IDs from an earlier incarnation will collide with new windows and panes. Consumers matching `Window.ID` or `Pane.ID` across snapshots or close events must scope matches to the same server incarnation (e.g. via `Manifest.TmuxPID` or server-start boundaries).
