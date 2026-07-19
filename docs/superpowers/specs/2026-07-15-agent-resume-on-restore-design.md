# Agent resume-on-restore (Claude / Codex / Cursor)

**Issue:** noamsto/tmux-remux#46
**Date:** 2026-07-15
**Status:** Design locked (adversarial spec review incorporated 2026-07-15)

## Problem

tmux-remux already has the machinery to restore a pane to its exact prior
state: the `@remux_relaunch` pane option is exec'd verbatim on restore
(`internal/restore/startup.go:71`, via `/bin/sh -c`; README:160), it round-trips
through save via `list-panes -F #{@remux_relaunch}`
(`internal/tmux/client.go:100`, `internal/snapshot/build.go`), and the
`OverrideCmd` branch unconditionally wins over the command allow-list
(`internal/restore/plan.go:124`). `claude --resume <uuid>` is the documented
worked example. What is missing is anything that *stamps* that option onto a
live agent pane.

Today the only thing that stamps it is external and lazytmux-specific:

- **Claude** — lazytmux's icon-update tick (`tmux-update-icons.sh`) polls every
  Claude pane's state file each tick, derives the session id from the transcript
  file basename, and stamps `@remux_relaunch "claude --resume <uuid>"`. Heavy
  (per-tick), coupled to lazytmux's status machinery, gated by `@resume_claude`.
  Crucially it also *clears* the stamp when a pane no longer runs an agent
  (`tmux-update-icons.sh:137`), which the event-driven design must preserve
  (see Design → Clearing).
- **Codex** — a bespoke `codex-relaunch-stamp.sh` SessionStart hook in lazytmux.

A standalone tmux-remux user (no lazytmux) gets a **bare shell** back for their
Claude/Codex/Cursor panes after a restore — the agent conversation is lost.

## Goal

Ship, from tmux-remux itself, an event-driven mechanism that stamps
`@remux_relaunch` with the correct resume command for an agent pane at session
start (and clears it at session end), so the pane restores as its exact prior
session. Cover Claude Code, Codex, and Cursor CLI through **one generic core**
with thin per-agent wiring, with no dependency on lazytmux.

All three agents share the same shape:

| Agent  | Resume command              | Session id source                        | Start-hook timing |
|--------|-----------------------------|------------------------------------------|-------------------|
| Claude | `claude --resume <id>`      | `SessionStart` hook, stdin `session_id`  | at pane creation  |
| Codex  | `codex resume <id>`         | `SessionStart` hook, stdin `session_id`  | **after first turn** (see limitation) |
| Cursor | `cursor-agent --resume <id>`| `sessionStart` hook, stdin `session_id`  | at pane creation (unverified, see Cursor) |

## Non-goals

- **Codex plugin packaging.** Codex has a real plugin system that can bundle
  `hooks/hooks.json`, but it is feature-gated (`CodexHooks`/`Plugins`, with a
  *removed* `PluginHooks` compat flag), has no `codex plugin install` verb, and
  adds a manifest + validation for zero runtime benefit over a hook wired into
  `config.toml`. Revisit only if bundling skills/commands becomes worthwhile.
- **Cursor plugin packaging.** Cursor's plugin system is IDE/marketplace-only;
  there is no way to install/enable a plugin *for* `cursor-agent` from the
  terminal. The CLI reads only `hooks.json`.
- **`--remote-control` force-on.** We do not force Remote Control on for panes
  that did not already have it.
- **Auto-*acting* on restore.** We resume the conversation; we do not inject a
  "continue" prompt to make the agent start working on its own.

## Design

### Core: `tmux-remux relaunch-stamp`

A subcommand designed to be a start-hook target. It:

1. Reads the agent hook's JSON payload from **stdin**.
2. Parses `session_id` with real Go JSON decoding (not a regex).
3. Substitutes the id into the agent's resume-command template.
4. Stamps `@remux_relaunch` on the pane via a new `internal/tmux` helper.

```
tmux-remux relaunch-stamp --agent claude|codex|cursor
tmux-remux relaunch-stamp --cmd 'foo --resume {id}'    # escape hatch, any agent
tmux-remux relaunch-stamp --clear                      # unset the stamp (session end)
```

- `--agent` presets carry the template and id field:
  - `claude` → `claude --resume {id}`
  - `codex`  → `codex resume {id}` (positional arg, not `--resume`)
  - `cursor` → `cursor-agent --resume {id}`
- `--cmd` takes an arbitrary template; `{id}` is the substitution point.
- `--clear` sets `@remux_relaunch` to empty (see Clearing). No stdin needed.
- `--pane <%id>` overrides the target pane (defaults to `$TMUX_PANE`); exists so
  tests can drive it without a real tmux hook context.
- `--id-field <name>` overrides the stdin JSON field (default `session_id`).

**No-op conditions (all exit 0, silently):**

- `$TMUX_PANE` unset and no `--pane` → not in a tmux pane.
- `tmux` not on `PATH`.
- stdin JSON missing/empty `session_id` (stamp mode) → **do not clobber** any
  existing stamp.
- The pane vanished between hook-fire and the tmux call → the `set-option` uses
  `-q` (quiet) and any error is ignored, so a dying agent's SessionStart hook
  never surfaces a spurious failure.

Idempotent: re-firing on `resume`/`compact` re-stamps the same string.

### Clearing (stale-stamp self-heal)

The superseded polling stamp cleared `@remux_relaunch` when a pane stopped
running an agent (`tmux-update-icons.sh:137`). Without an equivalent, a pane
that once ran an agent and is later reused as a plain shell would wrongly
`claude --resume <old-id>` on restore (the `OverrideCmd` branch wins over the
allow-list). So:

- **Claude / Cursor:** wire `relaunch-stamp --clear` to the agent's session-end
  event (`SessionEnd` / `sessionEnd`). On clean exit the stamp is removed and
  the pane restores per normal rules.
- **Codex:** has no clean session-end event (only `Stop`, which fires per turn).
  Codex therefore keeps a **best-effort stale stamp** — documented limitation.
- Hard kills (server crash, `kill -9`) skip the end hook by nature; the stamp
  persists, which is the correct behavior (the agent *was* running when last
  seen). Accepted.

### `internal/tmux`: `SetPaneOption`

`internal/tmux` is read-only today. Add:

```go
func (c *Client) SetPaneOption(pane, name, value string) error
```

wrapping `tmux set-option -pq -t <pane> <name> <value>` (`-q` per the no-op
rule above). This is the only new tmux write path; it mirrors the existing
`exec.Command("tmux", …)` wrapper style. Clearing sets `value` to `""`.

### Parity layer: `tmux-remux install-hook`

Gives Codex and Cursor one-command setup, matching Claude's plugin install.
**The two backends differ** (Codex is TOML, Cursor is JSON — there is no shared
merge helper):

```
tmux-remux install-hook codex|cursor
```

- **Codex → `~/.codex/config.toml` marker-guarded append.** Matches lazytmux's
  proven approach (`home-manager.nix:716-739`): the #140 spike established Codex
  auto-loads only that one global file (no drop-in `hooks.json` dir to rely on).
  Append a block guarded by a fixed marker sentinel; never touch existing
  content; no-op if the marker is already present. The block wires both
  `SessionStart` (→ `relaunch-stamp --agent codex`) and `SessionEnd` if the
  installed Codex version supports it (else SessionStart only + the stale-stamp
  limitation above).
- **Cursor → `~/.cursor/hooks.json` marker-keyed JSON merge.** Parse the v1
  JSON, add our `sessionStart` (and `sessionEnd`) entry if our marker isn't
  already present, preserve all other entries/fields, write back. No-op if
  present.
- **Idempotency key:** a fixed embedded **marker sentinel** (not command-string
  matching — a binary-path or whitespace change must not produce a duplicate).
  If multiple pre-existing start-hook entries exist, ours is appended as one
  additional entry; we never rewrite or dedupe the user's entries.
- **Codex trust step (required, cannot be pre-seeded):** the Codex hook will not
  run until the user does a one-time `/hooks` → "Trust all" per machine (the
  trust hash is an undocumented content digest — `home-manager.nix:723`).
  `install-hook codex` **prints this as a mandatory follow-up**; setup is not
  complete until the user performs it.

Claude is **not** handled by `install-hook` — it ships as a plugin (below).

### Claude Code plugin (`claude-plugin/`)

A real, installable Claude Code plugin:

```
.claude-plugin/marketplace.json          # repo root: `claude plugin marketplace add noamsto/tmux-remux`
claude-plugin/
├── .claude-plugin/plugin.json            # name: tmux-remux
├── hooks/hooks.json                      # SessionStart (stamp) + SessionEnd (clear)
└── README.md                             # install paths, how it works
```

`hooks.json` wires `SessionStart` on `startup` and `resume`
(→ `tmux-remux relaunch-stamp --agent claude`) and `SessionEnd`
(→ `tmux-remux relaunch-stamp --clear`). Each entry calls the binary on `PATH`,
not a bundled script, since the logic lives in the binary (the binary is already
assumed on `PATH` — restore itself exec's `tmux-remux cat-scrollback`).

Install paths (documented in the plugin README, mirroring lazytmux's):

- `claude plugin marketplace add noamsto/tmux-remux` + `claude plugin install`
- `claude --plugin-dir /path/to/claude-plugin`
- Nix pin via `${inputs.tmux-remux}/claude-plugin` — no flake change needed.

**Assumptions to confirm during implementation** (external CC behavior, not
checkable in-repo — carry into the plan, do not treat as settled):

- `SessionStart` `startup`/`resume` matchers fire at the points assumed above
  and both carry a stable `session_id`.
- `claude --resume <id>` reconnects Remote Control for panes that had it, and
  degrades to a non-RC session if the server-side RC session expired.

### Cursor — verify-gated

Cursor's adapter is **gated on empirical verification**, not shipped
speculatively. Two facts are unverified by Cursor's docs and load-bearing:

1. **Id equivalence.** The `sessionStart` hook exposes `session_id` (docs: "same
   as `conversation_id`"), while `--resume` takes a `chatId`. Docs never equate
   these, and IDE vs CLI chats use *separate* session stores. If unequal, resume
   fails.
2. **Event firing.** Docs only guarantee `sessionStart` does *not* fire for
   cloud agents; local `cursor-agent` firing is implied, not documented.

**Gate:** during implementation, capture `session_id` from a real `cursor-agent`
`sessionStart` hook and confirm `cursor-agent --resume <that value>` reloads the
session and it appears in `cursor-agent ls`.

- **If it passes:** ship the `--agent cursor` preset + `install-hook cursor`.
- **If it fails:** **defer the Cursor adapter entirely** to a follow-up (keep
  this design note). Do not ship a preset that emits a resume command that does
  not resume. The documented fallback for a future follow-up is minting the id
  up front with `cursor-agent create-chat` and launching with `--resume <id>`.

## Data flow

**Phase 1 — live session stamps / clears:**

```
agent start hook fires
  → tmux-remux relaunch-stamp --agent <a>   (reads session_id from stdin)
  → tmux set -pq -t $TMUX_PANE @remux_relaunch "<resume cmd>"

agent session-end hook fires (Claude/Cursor)
  → tmux-remux relaunch-stamp --clear
  → tmux set -pq -t $TMUX_PANE @remux_relaunch ""
```

Note: Codex stamps only after the first user turn completes
(`codex-relaunch-stamp.sh:6`) — a Codex pane snapshotted before its first turn
restores bare. Known limitation.

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
  - `--clear` issues `set-option … @remux_relaunch ""`;
  - missing/empty `session_id` → no `set-option` issued;
  - no `$TMUX_PANE` / no `--pane` → no-op;
  - the tmux call is verified against a fake/mock (assert argv), so no real tmux
    server is required.
- **`install-hook`** (temp `$HOME`):
  - **codex:** `config.toml` absent → created with the marker block; present
    without marker → block appended, existing content preserved; present with
    marker → no-op; assert the trust-step message is printed.
  - **cursor:** `hooks.json` absent → created; present without our marker →
    entry added, unrelated entries/fields preserved; present with marker →
    no-op.
- **`SetPaneOption`** — covered via the `relaunch-stamp` argv assertions
  (asserts `-pq`).

- **Manual, blocking the Cursor adapter only:** the id-equivalence and
  event-firing gate described under Cursor. Claude + Codex do not depend on it.

## Open items

- Confirm the Claude `SessionStart` matcher firing points and `session_id`
  stability (listed as assumptions above) on the installed CC version.
- Confirm whether the installed Codex version fires a usable session-end event;
  if so, wire `--clear` for Codex and drop its stale-stamp limitation.
- Decide whether `relaunch-stamp` should log stamp/clear/skip to `state.log`,
  for parity with other subcommands.
- Follow-up (separate, **lazytmux** repo, not this spec): retire the
  `@resume_claude` polling stamp and the bespoke `codex-relaunch-stamp.sh`;
  point both at `tmux-remux relaunch-stamp`. The polling stamp derives the id
  from the transcript-file basename while the hook uses the `session_id` field;
  these usually coincide but may diverge after resume/compaction (transcript
  fork). Before removing the polling stamp, verify byte-equality, or accept that
  during the transition both resume *a* valid session and the pane option may
  churn between ticks.
