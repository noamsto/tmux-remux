# tmux-state

Fast, smart tmux state persistence. Replaces `tmux-resurrect` and `tmux-continuum` with a Go binary backed by SQLite and content-addressed scrollback files.

## Why

`tmux-resurrect` and `tmux-continuum` are unmaintained, slow, and dumb on restore. `tmux-state` saves state in parallel, applies a smart filter on restore (drops stale sessions, idle plain-shell panes, duplicate sessions), and offers proper history with an `fzf` picker — all in a single static binary.

## Install

### Nix flake

```nix
{
  inputs.tmux-state.url = "github:noamsto/tmux-state";
  # ... reference .packages.${system}.default in your environment
}
```

### From source

```bash
git clone https://github.com/noamsto/tmux-state
cd tmux-state
go build -o tmux-state ./cmd/tmux-state
```

## Configure

In `~/.tmux.conf`, add hooks and keybindings (see `examples/tmux.conf`).

Schedule periodic saves via systemd:

```ini
# ~/.config/systemd/user/tmux-state-save.service
[Service]
Type=oneshot
ExecStart=%h/.local/bin/tmux-state save --reason=timer

# ~/.config/systemd/user/tmux-state-save.timer
[Timer]
OnBootSec=2min
OnUnitActiveSec=60s

[Install]
WantedBy=timers.target
```

```bash
systemctl --user enable --now tmux-state-save.timer
```

## Subcommands

| Command | Purpose |
|---|---|
| `tmux-state save` | Snapshot the running server now |
| `tmux-state restore --auto` | Restore latest snapshot through smart filter |
| `tmux-state undo --pop` | Restore most recent close event |
| `tmux-state pick --kind=close` | fzf picker for close events |
| `tmux-state pick --kind=snapshot` | fzf picker for snapshots |
| `tmux-state list` | List events |
| `tmux-state list --json` | List events as newline-delimited JSON |
| `tmux-state prune` | Apply retention limits |
| `tmux-state gc` | Reap orphan scrollback files |

## Storage

- DB: `$XDG_DATA_HOME/tmux-state/state.db` (default `~/.local/share/tmux-state/state.db`)
- Scrollbacks: `$XDG_DATA_HOME/tmux-state/scrollbacks/<sha[:2]>/<sha>.zst`

## Spec

See `docs/specs/2026-04-26-tmux-state-design.md`.

## License

MIT
