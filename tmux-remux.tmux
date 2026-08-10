#!/usr/bin/env bash
#
# TPM entry point for tmux-remux: https://github.com/noamsto/tmux-remux
#
# Resolves a tmux-remux binary (PATH, or a cached/downloaded release build)
# and wires the same hooks/binds as examples/tmux.conf.
set -euo pipefail

CURRENT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO="noamsto/tmux-remux"

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
    printf 'tmux-remux: no sha256sum or shasum found\n' >&2
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
  local asset="tmux-remux_${os}_${arch}.tar.gz"
  local base_url="https://github.com/${REPO}/releases/download/${version}"
  local tmp_dir

  mkdir -p "$dest_dir" || return 1
  # Stage on the same filesystem as dest_dir so the final `mv` is an atomic rename.
  tmp_dir="$(mktemp -d "$dest_dir/tmux-remux-tmp.XXXXXX")" || return 1
  trap 'rm -rf "$tmp_dir"; trap - RETURN' RETURN

  curl -fsSL -o "$tmp_dir/$asset" "$base_url/$asset" || return 1
  curl -fsSL -o "$tmp_dir/checksums.txt" "$base_url/checksums.txt" || return 1
  verify_checksum "$tmp_dir" "$asset" || return 1

  tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" || return 1
  mv "$tmp_dir/tmux-remux" "$dest_dir/tmux-remux" || return 1
  chmod +x "$dest_dir/tmux-remux" || return 1
}

resolve_binary() {
  local path_bin
  if path_bin="$(command -v tmux-remux 2>/dev/null)"; then
    printf '%s' "$path_bin"
    return 0
  fi

  local cache_dir="$CURRENT_DIR/bin"
  local cached_bin="$cache_dir/tmux-remux"
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
  version="$(tmux_option_or_default "@tmux_remux_version" "latest")"
  if [ "$version" = "latest" ]; then
    version="$(resolve_latest_version)" || return 1
  fi

  download_release_binary "$version" "$os" "$arch" "$cache_dir" || return 1
  printf '%s' "$cached_bin"
}

wire_plugin() {
  local bin="$1"
  local auto_restore
  auto_restore="$(tmux_option_or_default "@tmux_remux_auto_restore" "on")"

  # internal/triggers renders every hook, bind and option, gated on the tmux
  # version it detects. --bin is omitted deliberately: the binary defaults it to
  # its own path, which is the one resolve_binary just found.
  #
  # A binary older than this plugin script has no `triggers` subcommand.
  if ! "$bin" triggers --auto-restore="$auto_restore" | tmux source-file -; then
    tmux display-message "tmux-remux: binary too old for this plugin version — update it or unpin @tmux_remux_version"
  fi
}

main() {
  local bin
  if ! bin="$(resolve_binary)"; then
    tmux display-message "tmux-remux: no binary found for this platform — install manually: https://github.com/${REPO}/releases or 'go install github.com/${REPO}/cmd/tmux-remux@latest'"
    return 0
  fi
  wire_plugin "$bin"
}

if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
  main "$@"
fi
