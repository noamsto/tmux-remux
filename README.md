# tmux-state

Fast, smart tmux state persistence. A Go replacement for [`tmux-resurrect`](https://github.com/tmux-plugins/tmux-resurrect) and [`tmux-continuum`](https://github.com/tmux-plugins/tmux-continuum), backed by SQLite and content-addressed scrollback files.

> **Status:** v0.1.0. Pre-release. Personal tool — used and shaped by my workflow. Bug reports welcome; feature requests will be answered with "PR welcome."

## What it does

- **Periodic save** — snapshots every tmux session, window, pane (cwd, command, layout, scrollback) every 60s and on every structural change.
- **Smart restore** — on tmux start, applies a filter that drops stale sessions, idle plain-shell panes, and duplicates of sessions already running. No more "all my closed splits from last week reappear."
- **Undo** — `prefix + u` instantly re-opens the last pane, window, or session you closed by accident (e.g. `Ctrl+D` cascade).
- **History** — every snapshot and close event lives in a SQLite store you can query, browse interactively, and prune.
- **One static binary** — no plugin manager, no shell scripts, no cron. systemd user timer + tmux hooks do the work.

## Why not tmux-resurrect / tmux-continuum?

| | resurrect + continuum | tmux-state |
|---|---|---|
| Maintenance | Stalled (last meaningful commit ~2020) | Active |
| Save speed (40 panes) | ~3-5s, sequential `tmux display-message` per pane | ~70ms, three batched `tmux -F` queries + parallel `capture-pane` |
| Auto-restore filter | None — restores everything | Smart filter (skip-running, idle-shell, stale age) |
| History | Single overwriting save file | SQLite with N rolling snapshots + close events |
| Undo for accidental `Ctrl+D` | No | `prefix + u` |
| Storage | Plain text + bash glue | SQLite + content-addressed compressed scrollback (refcount-deduped) |
| Implementation | ~1500 lines of bash | ~3500 lines of Go (with tests) |

If you love your existing resurrect+continuum setup, this won't change your life. If you've been keeping `@continuum-restore 'off'` because auto-restore is too noisy to trust — that's the problem `tmux-state` exists to fix.

## Install

### Nix flake (recommended)

```nix
{
  inputs.tmux-state.url = "github:noamsto/tmux-state";

  outputs = { self, nixpkgs, tmux-state, ... }: {
    # … reference tmux-state.packages.${system}.default in home-manager
    # or your environment.systemPackages, e.g.:
    # home.packages = [ tmux-state.packages.${pkgs.system}.default ];
  };
}
```

Or run directly: `nix run github:noamsto/tmux-state -- version`.

### From source

```bash
git clone https://github.com/noamsto/tmux-state
cd tmux-state
go build -o tmux-state ./cmd/tmux-state
```

Requires Go 1.23+. No CGO needed (pure-Go SQLite via `modernc.org/sqlite`).

## Quick start

Copy [`examples/tmux.conf`](examples/tmux.conf) into your `~/.tmux.conf` (or `source` it). It wires:

- 6 tmux hooks for save + close-event capture
- `prefix + u` (undo pop), `prefix + U` (close-event picker popup), `prefix + R` (snapshot picker popup), `prefix + Ctrl-s` (save now)
- `run-shell -b 'tmux-state restore --auto'` for auto-restore on tmux start

Then schedule the save timer:

```ini
# ~/.config/systemd/user/tmux-state-save.service
[Unit]
Description=Save tmux-state snapshot

[Service]
Type=oneshot
ExecStart=%h/.local/bin/tmux-state save --reason=timer

# ~/.config/systemd/user/tmux-state-save.timer
[Unit]
Description=Periodic tmux-state save

[Timer]
OnBootSec=2min
OnUnitActiveSec=60s
Unit=tmux-state-save.service

[Install]
WantedBy=timers.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now tmux-state-save.timer
```

That's it. `tmux-state save --reason=manual` to test, `tmux-state list` to see what was captured.

## Subcommands

| Command | Purpose |
|---|---|
| `tmux-state save` | Snapshot the running server now (idempotent — skipped if nothing changed) |
| `tmux-state restore --auto` | Restore the newest snapshot from before the current server started (so saves made by the freshly started server never shadow the pre-shutdown state), filtered by smart filter |
| `tmux-state undo --pop` | Restore the most recent close event (pane / window / session) |
| `tmux-state pick --kind=close` | Interactive picker over close events |
| `tmux-state pick --kind=snapshot` | Interactive picker over snapshot history (default) |
| `tmux-state capture-event KIND` | Record a close event (called from tmux hooks; not for direct use) |
| `tmux-state list` | List events, human-readable |
| `tmux-state list --json` | List events as newline-delimited JSON (for external pickers) |
| `tmux-state prune` | Apply retention limits (default: keep the 20 newest snapshots plus the newest snapshot per UTC day for the last 7 days; 50 close events) |
| `tmux-state gc` | Reap orphan scrollback files (refcount = 0) |
| `tmux-state version` | Print version |

### `pick`

Open an interactive picker over snapshot or close events. The picker is a Bubble Tea TUI that shows each snapshot's full session → window → pane tree before you restore it, and exposes the smart-restore filter as live footer toggles.

- `--kind=snapshot` (default) — two-pane view (snapshots on the left, tree on the right). Toggle `s` to skip idle shells, `d` to skip sessions already running (shown collapsed with a `(running)` tag), `a` to dim snapshots older than 24h.
- `--kind=close` — list-only view of close events, used by `prefix + U` in lazytmux.

Tab switches focus between panes. `?` shows the full keymap. `enter` restores; `esc` cancels.

## Smart restore filter

Configurable via env vars (TODO: also via flags). Defaults:

| Filter | Default | Effect |
|---|---|---|
| `restoreMaxSnapshotAge` | 30 days | Skip whole snapshot if older (host probably reinstalled) |
| `restoreMaxSessionAge` | 14 days | Skip session if `last_attached` older than threshold |
| `restoreSkipIdleShells` | on | Skip pane if command ∈ {bash, fish, zsh, sh} AND no children |
| `restoreSkipIdleWindows` | on | Skip window if every pane filtered out |
| `skipRunningSessions` | on | Skip session if a session with that name is already running |

Allow-list of commands to re-launch on restore: `nvim`, `vim`, `htop`, `btop`, `lazygit`, `lazydocker`, `k9s`, `kubectl`, `ssh`, `mosh`, `less`, `tail`, `watch`, etc. Anything not on the list restores as a fresh shell in the saved cwd.

**Per-pane relaunch override.** A pane may set the `@ts_relaunch` user option to a full shell command (e.g. `set -p @ts_relaunch "claude --resume <uuid>"`); on restore that command is exec'd verbatim, bypassing the allow-list. This lets a tool restore a pane's exact state (a resumed session, a specific REPL) that the bare command name can't capture. The owning tool is responsible for quoting the value.

## Storage

```
$XDG_DATA_HOME/tmux-state/
├── state.db                                  SQLite event store (events, scrollbacks, meta)
├── state.db-wal                              SQLite WAL file
├── state.db-shm                              SQLite shared memory
├── state.log                                 Operational decisions and errors (rotated at 1 MB)
└── scrollbacks/
    └── <sha256[:2]>/<sha256>.zst             Content-addressed, zstd-compressed pane scrollbacks
```

`$XDG_DATA_HOME` defaults to `~/.local/share`. Storage lives outside `/nix/store` and survives Nix garbage collection / generation rollback.

Scrollback files are content-addressed and refcounted — identical scrollbacks across snapshots are stored once. Files orphan-reaped weekly by `tmux-state gc`.

Concurrent writers are serialized by an advisory `flock` on `$XDG_RUNTIME_DIR/tmux-state/write.lock` plus SQLite WAL.

## Privacy and security

**Local-only by design.** Tmux scrollback regularly contains:

- File paths and command history (low sensitivity)
- Error messages with stack traces and internal hostnames (medium)
- Secrets pasted into prompts, env vars echoed by buggy programs, or output of `env` / `printenv` (high)

Don't sync `$XDG_DATA_HOME/tmux-state/` to cloud storage, don't commit it, don't share snapshots. If you need cross-host portability of session structure (without the scrollback bytes), set `captureScrollback = false` and rely on cwd + command relaunch.

## Architecture

- `cmd/tmux-state/main.go` — cobra CLI with the 11 subcommands above
- `internal/store` — SQLite layer (atomic transactional migrations, prepared queries)
- `internal/scrollback` — content-addressed file store with zstd compression and refcount-driven GC
- `internal/tmux` — wrapper around `exec.Command("tmux", …)` and parsers for `-F` output
- `internal/snapshot` — manifest types, parallel `capture-pane`, fingerprint-based throttle
- `internal/filter` — smart-restore filter as pure functions
- `internal/restore` — plan builder + apply (best-effort tmux command sequence)
- `internal/closeevent` — pane/window/session close hooks with cascade dedup
- `internal/lockfile` — advisory `flock` to serialize concurrent writers
- `internal/picker` — Bubble Tea TUI for `tmux-state pick` (master/detail tree preview + filter toggles)
- `internal/config` — XDG-resolved paths and threshold defaults

Full design at [`docs/specs/2026-04-26-tmux-state-design.md`](docs/specs/2026-04-26-tmux-state-design.md).

## Status and roadmap

**v0.1.0 — current.** Save, restore (manual + auto), undo, Bubble Tea picker, list/prune/gc, systemd timer.

**v0.2.0 — planned.**
- Per-unit restore (restore *just* this pane / window / session from a snapshot)
- nvim cooperation (companion Lua module that writes `mksession` files on `VimLeave`)
- Cross-host cwd remap rules

**Out of scope (likely forever):**
- Cloud sync — wrong threat model (see "Privacy and security")
- Plugin-manager packaging (TPM, etc.) — Nix is the supported install path; from-source works for everyone else

## Contributing

This is a personal tool I publish in case it's useful. Bug reports with reproduction steps are welcome. Feature requests are unlikely to be implemented unless I hit them in my own workflow — fork freely.

## Acknowledgements

- [`tmux-resurrect`](https://github.com/tmux-plugins/tmux-resurrect) and [`tmux-continuum`](https://github.com/tmux-plugins/tmux-continuum) for blazing the trail. The smart filter exists because they didn't.
- [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) for pure-Go SQLite that cross-compiles trivially.
- [`klauspost/compress`](https://github.com/klauspost/compress) for fast pure-Go zstd.

## License

MIT — see [`LICENSE`](LICENSE).
