# tmux-state

Fast, smart tmux state persistence. Replaces `tmux-resurrect` and `tmux-continuum` with a Go binary backed by SQLite.

## Status

Pre-release. v0.1.0 in development.

## Features (v0.1.0)

- Periodic save of all sessions/windows/panes (cwd, command, layout, scrollback)
- Smart restore filter (dedup, idle-shell drop, stale age)
- Undo for closed sessions/windows/panes via tmux hooks
- fzf picker for browsing history

## Install

(TODO: add once published)

## Usage

(TODO: add once CLI is implemented)

## Spec

See `docs/specs/2026-04-26-tmux-state-design.md`.
