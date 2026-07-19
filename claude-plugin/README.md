# tmux-remux — Claude Code plugin

Stamps `@remux_relaunch` on this Claude pane at `SessionStart` (and clears it at
`SessionEnd`) so [tmux-remux](https://github.com/noamsto/tmux-remux) restores the
exact Claude session — `claude --resume <id>` — instead of a bare shell after a
tmux restart. Remote Control reconnects automatically for panes that had it.

**Requires** the `tmux-remux` binary on `PATH` (the hooks call it directly). Safe
to install anywhere: outside a tmux pane the hook no-ops.

## Install

    claude plugin marketplace add noamsto/tmux-remux
    claude plugin install tmux-remux@tmux-remux

Or point Claude at the dir directly:

    claude --plugin-dir /path/to/tmux-remux/claude-plugin

Nix pin: `claude --plugin-dir "${inputs.tmux-remux}/claude-plugin"`.

## Verify

    claude plugin list --enabled     # tmux-remux present
    # inside a tmux pane, after session start:
    tmux show -p @remux_relaunch      # → claude --resume <session-id>
