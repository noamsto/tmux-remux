# tmux-remux for coding agents

You are an agent running inside a tmux pane. Installing tmux-remux lets your
session survive a tmux server restart, a reboot, or an accidental close — and
lets you mark *your own* pane so it relaunches into the exact state you're in
(e.g. a resumed conversation), not a bare shell.

Run the steps below verbatim. They are idempotent — safe to re-run.

## 1. Install the binary

Pick the first that applies to the host:

```bash
# Nix (if `nix` is on PATH)
nix profile install github:noamsto/tmux-remux

# Go toolchain (Go 1.23+)
go install github.com/noamsto/tmux-remux/cmd/tmux-remux@latest

# Prebuilt release — no toolchain (set os/arch from `uname -s`/`uname -m`)
#   linux|darwin  ×  amd64|arm64
```

Confirm it resolves, and remember the path — later steps need it:

```bash
command -v tmux-remux    # e.g. ~/go/bin/tmux-remux or ~/.local/bin/tmux-remux
tmux-remux version
```

## 2. Wire the tmux hooks and binds

These capture structural saves (new session/window, detach, close) and undo
events. Source the shipped example into the live server and append it to the
user's config so it persists:

```bash
tmux source-file examples/tmux.conf          # from a checkout, or:
curl -fsSL https://raw.githubusercontent.com/noamsto/tmux-remux/main/examples/tmux.conf -o /tmp/tmux-remux.conf
tmux source-file /tmp/tmux-remux.conf
```

Add `source ~/.tmux/…/examples/tmux.conf` (or the TPM plugin line
`set -g @plugin 'noamsto/tmux-remux'`) to `~/.tmux.conf` so it survives restart.

## 3. Schedule the periodic save timer

Hooks only fire on structural change. The 60s snapshot timer is separate and
platform-specific. Detect the platform with `uname -s`:

### Linux — `Linux` (systemd)

```bash
BIN=$(command -v tmux-remux)
mkdir -p ~/.config/systemd/user
printf '[Unit]\nDescription=Save tmux-remux snapshot\n[Service]\nType=oneshot\nExecStart=%s save --reason=timer\n' "$BIN" > ~/.config/systemd/user/tmux-remux-save.service
printf '[Unit]\nDescription=Periodic tmux-remux save\n[Timer]\nOnBootSec=2min\nOnUnitActiveSec=60s\nUnit=tmux-remux-save.service\n[Install]\nWantedBy=timers.target\n' > ~/.config/systemd/user/tmux-remux-save.timer
systemctl --user daemon-reload
systemctl --user enable --now tmux-remux-save.timer
```

### macOS — `Darwin` (launchd)

launchd needs an **absolute** binary path (no `~`/`$HOME` expansion) and a
`PATH` that reaches `tmux`, since it starts with a minimal environment. Copy
[`../examples/tmux-remux-save.plist`](../examples/tmux-remux-save.plist), patch
the path, and load it:

```bash
BIN=$(command -v tmux-remux)
PLIST=~/Library/LaunchAgents/io.github.noamsto.tmux-remux-save.plist
cp examples/tmux-remux-save.plist "$PLIST"
sed -i '' "s#/Users/YOU/.local/bin/tmux-remux#$BIN#" "$PLIST"
launchctl bootstrap gui/$(id -u) "$PLIST"
```

Unload with `launchctl bootout gui/$(id -u)/io.github.noamsto.tmux-remux-save`.

## 4. Mark your own pane for exact relaunch

By default a pane restores by re-running its bare command name (or a fresh
shell). To restore your *exact* state, set `@remux_relaunch` on your pane to the
full command that re-enters it — it's exec'd verbatim on restore, bypassing the
allow-list. You are responsible for quoting the value.

```bash
tmux set -p @remux_relaunch "claude --resume <session-uuid>"
```

## 5. Verify

```bash
tmux-remux save --reason=manual
tmux-remux list                  # your session should appear, with the pane's relaunch command
```
