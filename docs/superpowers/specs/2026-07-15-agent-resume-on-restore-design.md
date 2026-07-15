# Agent resume-on-restore (Claude / Codex / Cursor)

**Issue:** noamsto/tmux-remux#46
**Date:** 2026-07-15
**Status:** Design approved, pending implementation plan

## Problem

tmux-remux already has the machinery to restore a pane to its exact prior
state: the `@remux_relaunch` pane option is exec'd verbatim on restore
(README:160, `internal/restore/startup.go`), and `claude --resume <uuid>` is the
documented worked example. What is missing is anything that *stamps* that option
onto a live agent pane.

Today the only thing that stamps it is external and lazytmux-specific:

- **Claude** — lazytmux's icon-update tick (`tmux-update-icons.sh`) polls every
  Claude pane's state file each tick, extracts the session UUID from the
  transcript path, and stamps `@remux_relaunch "claude --resume <uuid>"`. Heavy
  (per-tick), coupled to lazytmux's whole status machinery, gated by
  `@resume_claude`.
- **Codex** — a bespoke `codex-relaunch-stamp.sh` SessionStart hook in lazytmux.

A standalone tmux-remux user (no lazytmux) gets a **bare shell** back for their
Claude/Codex/Cursor panes after a restore — the agent conversation is lost.

## Goal

Ship, from tmux-remux itself, an event-driven mechanism that stamps
`@remux_relaunch` with the correct resume command for an agent pane at session
start, so the pane restores as its exact prior session. Cover Claude Code,
Codex, and Cursor CLI through **one generic core** with thin per-agent wiring,
with no dependency on lazytmux.

All three agents share the same shape:

| Agent  | Resume command              | Session id source                         |
|--------|-----------------------------|-------------------------------------------|
| Claude | `claude --resume <id>`      | `SessionStart` hook, stdin `session_id`   |
| Codex  | `codex resume <id>`         | `SessionStart` hook, stdin `session_id`   |
| Cursor | `cursor-agent --resume <id>`| `sessionStart` hook, stdin `session_id`   |

## Non-goals

- **Codex plugin packaging.** Codex has a real plugin system that can bundle
  `hooks/hooks.json`, but it is feature-gated (`CodexHooks`/`Plugins`, with a
  *removed* `PluginHooks` compat flag), has no `codex plugin install` verb
  (install is via the `/plugins` TUI + marketplaces), and adds a manifest +
  validation for zero runtime benefit over a drop-in hook. The drop-in
  `~/.codex/hooks.json` produces identical behavior. Revisit only if bundling
  skills/commands ever becomes worthwhile.
- **Cursor plugin packaging.** Cursor's plugin system is IDE/marketplace-only;
  there is no way to install/enable a plugin *for* `cursor-agent` from the
  terminal. The CLI reads only `hooks.json`.
- **`--remote-control` force-on.** Claude `/remote-control` reconnects
  automatically on `--resume` for panes that had it (verified); we do not force
  it on for panes that did not.
- **Auto-*acting* on restore.** We resume the conversation; we do not inject a
  "continue" prompt to make the agent start working on its own.

## Design

### Core: `tmux-remux relaunch-stamp`

A subcommand designed to be a start-hook target. It:

1. Reads the agent hook's JSON payload from **stdin**.
2. Parses `session_id` with real Go JSON decoding (not a regex — kills the
   fragility the Cursor research flagged).
3. Substitutes the id into the agent's resume-command template.
4. Stamps `@remux_relaunch` on the pane via a new `internal/tmux` helper.

```
tmux-remux relaunch-stamp --agent claude|codex|cursor
tmux-remux relaunch-stamp --cmd 'foo --resume {id}'    # escape hatch, any agent
```

- `--agent` presets carry the template and id field:
  - `claude` → `claude --resume {id}`
  - `codex`  → `codex resume {id}` (positional arg, not `--resume`)
  - `cursor` → `cursor-agent --resume {id}`
- `--cmd` takes an arbitrary template; `{id}` is the substitution point.
- `--pane <%id>` overrides the target pane (defaults to `$TMUX_PANE`); exists so
  tests can drive it without a real tmux hook context.
- `--id-field <name>` overrides the stdin JSON field (default `session_id`).

**No-op conditions (all exit 0, silently):**

- `$TMUX_PANE` unset and no `--pane` → not in a tmux pane.
- `tmux` not on `PATH`.
- stdin JSON missing/empty `session_id` → **do not clobber** any existing stamp.

Idempotent: re-firing on `resume`/`compact` re-stamps the same string.

### `internal/tmux`: `SetPaneOption`

`internal/tmux` is read-only today. Add:

```go
func (c *Client) SetPaneOption(pane, name, value string) error
```

wrapping `tmux set-option -p -t <pane> <name> <value>`. This is the only new
tmux write path; it mirrors the existing `exec.Command("tmux", …)` wrapper style.

### Parity layer: `tmux-remux install-hook`

Gives Codex and Cursor one-command setup, matching Claude's plugin install:

```
tmux-remux install-hook codex|cursor
```

- Idempotent, marker-guarded **JSON merge** into the agent's native hooks file
  (`~/.codex/hooks.json`, `~/.cursor/hooks.json`): add our start-hook entry
  (calling `tmux-remux relaunch-stamp --agent <a>`), preserve all existing
  content, no-op if our entry is already present.
- A shared merge helper serves both agents (both are `{version/hooks}` JSON).
- Keyed on the command string (or an embedded marker) so re-runs are safe.

Codex is wired via the drop-in `~/.codex/hooks.json` (a file tmux-remux can own)
rather than appending to the user's hand-edited `config.toml`.

Claude is **not** handled by `install-hook` — it ships as a plugin (below).

### Claude Code plugin (`claude-plugin/`)

A real, installable Claude Code plugin:

```
.claude-plugin/marketplace.json          # repo root: `claude plugin marketplace add noamsto/tmux-remux`
claude-plugin/
├── .claude-plugin/plugin.json            # name: tmux-remux
├── hooks/hooks.json                      # SessionStart startup+resume → relaunch-stamp --agent claude
└── README.md                             # install paths, how it works
```

`hooks.json` wires `SessionStart` on `startup` and `resume` (session_id stable
across both; `startup` stamps a fresh pane at creation, `resume` re-stamps
idempotently). Each entry calls the binary on `PATH` —
`tmux-remux relaunch-stamp --agent claude` — not a bundled script, since the
logic lives in the binary. (The binary is already assumed on `PATH` for these
users: restore itself exec's `tmux-remux cat-scrollback`.)

Install paths (documented in the plugin README, mirroring lazytmux's):

- `claude plugin marketplace add noamsto/tmux-remux` + `claude plugin install`
- `claude --plugin-dir /path/to/claude-plugin`
- Nix pin via `${inputs.tmux-remux}/claude-plugin` — **no flake change needed**
  (the plugin is just files in the repo).

Claude `/remote-control` reconnects for free: `claude --resume <id>` reconnects
to the RC session recorded in that conversation (verified), so panes that had RC
come back drivable; panes that did not come back normal; an expired server-side
RC session degrades gracefully to a non-RC session.

### Cursor — experimental

Cursor ships but is marked **experimental** in its README section, because two
facts are unverified by Cursor's docs and are load-bearing:

1. **Id equivalence.** The `sessionStart` hook exposes `session_id`
   (docs: "same as `conversation_id`"), while `--resume` takes a `chatId`. Docs
   never state these are the same string, and IDE vs CLI chats use *separate*
   session stores. If they are not equal, resume fails.
2. **Event firing.** Docs only explicitly guarantee `sessionStart` does *not*
   fire for cloud agents; local `cursor-agent` firing is strongly implied, not
   documented verbatim.

**Mandatory verification** (part of implementation, see Testing → Manual):
capture `session_id` from a real `cursor-agent` `sessionStart` hook, confirm
`cursor-agent --resume <that value>` reloads the session and it appears in
`cursor-agent ls`.

**Documented fallback** if id-equivalence fails: mint the id up front with
`cursor-agent create-chat` and launch/relaunch with `--resume <minted-id>`,
sidestepping the hook-id ⇄ resume-id question entirely. (Not implemented now;
documented as the escape route.)

## Data flow

**Phase 1 — live session stamps (once per agent pane):**

```
agent start hook fires
  → tmux-remux relaunch-stamp --agent <a>   (reads session_id from stdin)
  → tmux set -p -t $TMUX_PANE @remux_relaunch "<resume cmd>"
```

**Phase 2 — restore consumes (after tmux restart):**

```
tmux-remux save            (snapshot captures @remux_relaunch on the pane)
tmux-remux restore --auto  (OverrideCmd path exec's the stored resume cmd verbatim)
  → agent reopens its exact prior session
  → (Claude only) RC reconnects if it was on
```

## Testing

All automated tests are **Go** — no new `bats` infra.

- **`relaunch-stamp`** (table-driven):
  - each `--agent` preset produces the correct `set-option` args from a sample
    stdin payload;
  - `--cmd` escape hatch substitutes `{id}`;
  - missing/empty `session_id` → no `set-option` issued;
  - no `$TMUX_PANE` / no `--pane` → no-op;
  - the tmux call is verified against a fake/mock (assert argv), so no real tmux
    server is required.
- **`install-hook`** (temp `$HOME`):
  - file absent → created with our entry;
  - file present without our entry → entry added, unrelated content preserved;
  - file present with our entry → no-op (idempotent);
  - for both `codex` and `cursor` schemas.
- **`SetPaneOption`** — covered via the `relaunch-stamp` argv assertions.

- **Manual (Cursor only, blocking the Cursor adapter):** the id-equivalence and
  event-firing verification described above, on a real `cursor-agent`.

## Open items

- Confirm the exact Claude Code plugin `hooks.json` matcher values that fire at
  pane creation vs. resume on the installed CC version.
- Decide whether `relaunch-stamp` should emit a line to tmux-remux's
  `state.log` on stamp/skip, for parity with other subcommands' logging.
- Follow-up (separate, **lazytmux** repo, not this spec): retire the
  `@resume_claude` per-tick polling stamp in `tmux-update-icons.sh` and the
  bespoke `codex-relaunch-stamp.sh`; point both at `tmux-remux relaunch-stamp`.
  Transitional double-stamp is the same string, so no breakage.
