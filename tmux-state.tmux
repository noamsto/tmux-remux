#!/usr/bin/env bash
#
# TPM entry point for tmux-state: https://github.com/noamsto/tmux-state
#
# Resolves a tmux-state binary (PATH, or a cached/downloaded release build)
# and wires the same hooks/binds as examples/tmux.conf.
set -euo pipefail

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

resolve_latest_version() {
  local url
  url="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
  printf '%s' "${url##*/}"
}

verify_checksum() {
  local dir="$1" asset="$2"
  local sha_cmd
  if command -v sha256sum >/dev/null 2>&1; then
    sha_cmd=(sha256sum)
  elif command -v shasum >/dev/null 2>&1; then
    sha_cmd=(shasum -a 256)
  else
    printf 'tmux-state: no sha256sum or shasum found\n' >&2
    return 1
  fi
  (
    cd "$dir" || exit 1
    grep " ${asset}\$" checksums.txt > expected.sha256
    "${sha_cmd[@]}" -c expected.sha256
  ) >/dev/null
}

download_release_binary() {
  local version="$1" os="$2" arch="$3" dest_dir="$4"
  local asset="tmux-state_${os}_${arch}.tar.gz"
  local base_url="https://github.com/${REPO}/releases/download/${version}"
  local tmp_dir
  tmp_dir="$(mktemp -d)"
  trap 'rm -rf "$tmp_dir"; trap - RETURN' RETURN

  curl -fsSL -o "$tmp_dir/$asset" "$base_url/$asset" || return 1
  curl -fsSL -o "$tmp_dir/checksums.txt" "$base_url/checksums.txt" || return 1
  verify_checksum "$tmp_dir" "$asset" || return 1

  tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" || return 1
  mkdir -p "$dest_dir"
  mv "$tmp_dir/tmux-state" "$dest_dir/tmux-state"
  chmod +x "$dest_dir/tmux-state"
}

resolve_binary() {
  local path_bin
  if path_bin="$(command -v tmux-state 2>/dev/null)"; then
    printf '%s' "$path_bin"
    return 0
  fi

  local cache_dir="$CURRENT_DIR/bin"
  local cached_bin="$cache_dir/tmux-state"
  if [ -x "$cached_bin" ]; then
    printf '%s' "$cached_bin"
    return 0
  fi

  local os arch
  if ! os="$(detect_os)"; then
    return 1
  fi
  if ! arch="$(detect_arch)"; then
    return 1
  fi

  local version
  version="$(tmux_option_or_default "@tmux_state_version" "latest")"
  if [ "$version" = "latest" ]; then
    version="$(resolve_latest_version)" || return 1
  fi

  download_release_binary "$version" "$os" "$arch" "$cache_dir" || return 1
  printf '%s' "$cached_bin"
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

  tmux bind-key u   run-shell "${bin} undo --pop"
  tmux bind-key U   display-popup -E -w 90% -h 85% "${bin} pick --kind=close"
  tmux bind-key R   display-popup -E -w 90% -h 85% "${bin} pick --kind=snapshot"
  tmux bind-key C-s run-shell "${bin} save --reason=keybinding"
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
