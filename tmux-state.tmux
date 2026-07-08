#!/usr/bin/env bash
#
# TPM entry point for tmux-state: https://github.com/noamsto/tmux-state
#
# Resolves a tmux-state binary (PATH, or a cached/downloaded release build)
# and wires the same hooks/binds as examples/tmux.conf.
set -euo pipefail

# shellcheck disable=SC2034  # CURRENT_DIR is used by the download fallback added in the release-download commit
CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO="noamsto/tmux-state"

tmux_option_or_default() {
  local option="$1"
  local default_value="$2"
  local value
  value="$(tmux show-option -gqv "$option")"
  if [ -z "$value" ]; then
    printf '%s' "$default_value"
  else
    printf '%s' "$value"
  fi
}

detect_os() {
  local raw="${1:-$(uname -s)}"
  case "$raw" in
    Linux) printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    *) return 1 ;;
  esac
}

detect_arch() {
  local raw="${1:-$(uname -m)}"
  case "$raw" in
    x86_64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *) return 1 ;;
  esac
}

resolve_binary() {
  local path_bin
  if path_bin="$(command -v tmux-state 2>/dev/null)"; then
    printf '%s' "$path_bin"
    return 0
  fi
  return 1
}

wire_plugin() {
  local bin="$1"
  local auto_restore
  auto_restore="$(tmux_option_or_default "@tmux_state_auto_restore" "on")"

  tmux set-hook -g session-created "run-shell -b '${bin} save --reason=hook:session-created'"
  tmux set-hook -g window-linked   "run-shell -b '${bin} save --reason=hook:window-linked'"
  tmux set-hook -g client-detached "run-shell -b '${bin} save --reason=hook:client-detached'"

  tmux set-hook -g pane-exited     "run-shell -b '${bin} capture-event pane-died --pane=#{hook_pane} --window=#{hook_window} --session=#{hook_session}'"
  tmux set-hook -g window-unlinked "run-shell -b '${bin} capture-event window-unlinked --window=#{hook_window} --session=#{hook_session}'"
  tmux set-hook -g session-closed  "run-shell -b '${bin} capture-event session-closed --session=#{hook_session}'"

  if [ "$auto_restore" = "on" ]; then
    tmux run-shell -b "${bin} restore --auto"
  fi

  tmux bind-key u   run-shell "'${bin} undo --pop'"
  tmux bind-key U   display-popup -E -w 90% -h 85% "'${bin} pick --kind=close'"
  tmux bind-key R   display-popup -E -w 90% -h 85% "'${bin} pick --kind=snapshot'"
  tmux bind-key C-s run-shell "'${bin} save --reason=keybinding'"
}

main() {
  local bin
  if ! bin="$(resolve_binary)"; then
    tmux display-message "tmux-state: no binary found for this platform — install manually: https://github.com/${REPO}/releases or 'go install github.com/${REPO}/cmd/tmux-state@latest'"
    return 0
  fi
  wire_plugin "$bin"
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
