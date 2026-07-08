# TPM Plugin Entry Script Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `tmux-state.tmux` at the repo root so tmux-state installs via TPM (`set -g @plugin 'noamsto/tmux-state'`), and flip the README to document that path.

**Architecture:** A single executable bash script at the repo root. TPM executes every `*.tmux` file at tmux start (and on `prefix + I`); the script resolves a `tmux-state` binary (PATH → cached download → fresh release download) and then calls `tmux set-hook` / `tmux bind-key` / `tmux run-shell` directly against the running server — the same commands `examples/tmux.conf` has tmux read declaratively, just issued imperatively with the resolved binary path substituted in. Functions are guarded behind `if [ "${BASH_SOURCE[0]}" = "${0}" ]; then main "$@"; fi` so tests can `source` the file and call individual functions without running `main` (which touches `tmux` / the network).

**Tech Stack:** bash (`set -euo pipefail`), coreutils, curl, tar, sha256sum/shasum. No new dependencies. No Go code touched.

## Global Constraints

- Script lives at repo root: `tmux-state.tmux`, executable, shebang `#!/usr/bin/env bash`.
- Must be shellcheck-clean (zero warnings/errors) — this is a REQUIRED verification gate, run it after every step that touches the file.
- Must work on Linux and macOS/BSD: prefer `curl`; use `sha256sum` when present, fall back to `shasum -a 256`; use `mktemp -d` (no GNU-only flags).
- Release asset naming (from `.goreleaser.yaml`, do not deviate): `tmux-state_{os}_{arch}.tar.gz` where `os` ∈ `{linux, darwin}` (from `uname -s` → `Linux`/`Darwin`), `arch` ∈ `{amd64, arm64}` (from `uname -m` → `x86_64`/`aarch64`|`arm64`). Checksums file: `checksums.txt`, same release.
- GitHub repo slug for release URLs: `noamsto/tmux-state`.
- Hook/bind wiring must exactly match `examples/tmux.conf`'s behavior (3 save hooks, 3 capture-event hooks with `pane-exited` → kind `pane-died`, auto-restore, 4 binds `u`/`U`/`R`/`C-s`) — do not adopt lazytmux's `[99]` indices, bordered-popup titles, or `restoreMode` gating; only the binary-path substitution pattern is reused from that reference.
- No systemd/launchd install code. No Go changes. No flake/lazytmux changes. Do not push a `v*` tag or cut a release.
- The live GitHub download path cannot be exercised end-to-end (no release exists yet — `gh release list` is empty). Every task that touches it must say so explicitly in its test step and the plan's final PR-body note must repeat it.

---

## File Structure

- Create: `tmux-state.tmux` (repo root) — the entire plugin: option helpers, os/arch detection, checksum verification, release download, binary resolution, hook/bind wiring, entry guard. One file, per TPM convention (plugins are a single `*.tmux` script) and because the task caps scope at "self-contained, dependency-light."
- Modify: `README.md` — flip the TPM non-goal (~line 192) and add a "TPM" subsection under `## Install` (~line 29-55).

No new test files are added to the repo — this task's deliverables are the script and docs (see task's "Scope / non-goals"). Verification instead uses ad hoc bash commands run against a throwaway tmux server, shown in full in each task below so they're reproducible.

---

### Task 1: Script skeleton — option helper, os/arch detection, PATH resolution, hook/bind wiring

**Files:**
- Create: `tmux-state.tmux`

**Interfaces:**
- Produces: `tmux_option_or_default(option, default) -> stdout`, `detect_os([raw]) -> stdout|exit1`, `detect_arch([raw]) -> stdout|exit1`, `wire_plugin(bin)` (calls `tmux set-hook`/`bind-key`/`run-shell`), `main()`. Task 2 extends `resolve_binary` (introduced here as the PATH-only stub) with the download fallback and calls the same `wire_plugin`.

- [ ] **Step 1: Create `tmux-state.tmux` with PATH-only resolution and full hook/bind wiring**

```bash
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
```

Make it executable:

```bash
chmod +x /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
```

- [ ] **Step 2: shellcheck — must be zero warnings/errors**

Run:
```bash
shellcheck /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
```
Expected: no output, exit code 0. Fix any warning before moving on (this file will grow in Task 2 — keep it clean now so Task 2's diff stays clean too).

- [ ] **Step 3: Unit-test the pure functions by sourcing the file**

Run (fish shell, so use `bash -c` to keep the test script POSIX/bash):
```bash
bash -c '
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  set -e
  [ "$(detect_os Linux)" = "linux" ] || { echo "FAIL: detect_os Linux"; exit 1; }
  [ "$(detect_os Darwin)" = "darwin" ] || { echo "FAIL: detect_os Darwin"; exit 1; }
  detect_os Windows_NT >/dev/null 2>&1 && { echo "FAIL: detect_os Windows_NT should fail"; exit 1; }
  [ "$(detect_arch x86_64)" = "amd64" ] || { echo "FAIL: detect_arch x86_64"; exit 1; }
  [ "$(detect_arch aarch64)" = "arm64" ] || { echo "FAIL: detect_arch aarch64"; exit 1; }
  [ "$(detect_arch arm64)" = "arm64" ] || { echo "FAIL: detect_arch arm64"; exit 1; }
  detect_arch riscv64 >/dev/null 2>&1 && { echo "FAIL: detect_arch riscv64 should fail"; exit 1; }
  echo "PASS: detect_os / detect_arch"
'
```
Expected: `PASS: detect_os / detect_arch`, exit 0. `source` runs top-level code but `main` is guarded by the `BASH_SOURCE == 0` check, so nothing touches `tmux` or the network here.

- [ ] **Step 4: Integration test — fake `tmux-state` on PATH, throwaway tmux server**

```bash
TEST_DIR="$(mktemp -d)"
printf '#!/usr/bin/env bash\necho "fake-tmux-state $*" >> "%s/calls.log"\n' "$TEST_DIR" > "$TEST_DIR/tmux-state"
chmod +x "$TEST_DIR/tmux-state"
tmux -L ts-plugin-test kill-server 2>/dev/null
env PATH="$TEST_DIR:$PATH" tmux -L ts-plugin-test new-session -d -s test
tmux -L ts-plugin-test run-shell "/home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux"
tmux -L ts-plugin-test list-hooks -g
tmux -L ts-plugin-test list-keys -T prefix | grep -E ' u | U | R | C-s '
tmux -L ts-plugin-test kill-server
```
Note: tmux `run-shell` jobs inherit the environment captured when the *server* was spawned, not the invoking client's environment — so `PATH` must be set on the `new-session` command that starts the server, not on the later `run-shell` call.

Expected:
- `list-hooks -g` shows `session-created`, `window-linked`, `client-detached`, `pane-exited`, `window-unlinked`, `session-closed`, each running `<TEST_DIR>/tmux-state ...` with the right args (grep for `capture-event pane-died` on the `pane-exited` line specifically).
- `list-keys -T prefix` shows binds for `u`, `U`, `R`, `C-s`.
- No errors printed to the terminal from `run-shell`.
- `cat "$TEST_DIR/calls.log"` shows one line: `fake-tmux-state restore --auto` (the auto-restore `run-shell -b` fired once, since `@tmux_state_auto_restore` defaults to `on`).

- [ ] **Step 5: Commit**

```bash
git add tmux-state.tmux
git commit -m "feat: add TPM entry script (PATH resolution + hook/bind wiring)"
```

---

### Task 2: Download fallback — version resolution, checksum verification, release download, caching

**Files:**
- Modify: `tmux-state.tmux`

**Interfaces:**
- Consumes: `CURRENT_DIR`, `REPO`, `tmux_option_or_default`, `detect_os`, `detect_arch` from Task 1.
- Produces: `resolve_latest_version() -> stdout(tag)|exit1`, `verify_checksum(dir, asset) -> exit0|exit1`, `download_release_binary(version, os, arch, dest_dir) -> exit0|exit1`. Extends `resolve_binary()` to fall back to cache-dir lookup then download instead of failing immediately.

- [ ] **Step 1: Add the three new functions and extend `resolve_binary`**

Read the current file first:
```bash
cat -n /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
```

Replace the `resolve_binary` function (the one added in Task 1) with the block below, which adds the new functions above it and extends `resolve_binary`:

```bash
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
  trap 'rm -rf "$tmp_dir"' RETURN

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
```

- [ ] **Step 2: shellcheck**

```bash
shellcheck /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
```
Expected: no output, exit 0.

- [ ] **Step 3: Build a local fixture binary + checksums.txt, test `verify_checksum` pass and fail cases**

```bash
FIXTURE_DIR="$(mktemp -d)"
cd /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin
go build -o "$FIXTURE_DIR/tmux-state" ./cmd/tmux-state
tar -czf "$FIXTURE_DIR/tmux-state_linux_amd64.tar.gz" -C "$FIXTURE_DIR" tmux-state
sha256sum "$FIXTURE_DIR/tmux-state_linux_amd64.tar.gz" | sed "s|$FIXTURE_DIR/||" > "$FIXTURE_DIR/checksums.txt"
cat "$FIXTURE_DIR/checksums.txt"

bash -c '
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  verify_checksum "'"$FIXTURE_DIR"'" "tmux-state_linux_amd64.tar.gz" \
    && echo "PASS: verify_checksum accepts a correct archive" \
    || echo "FAIL: verify_checksum rejected a correct archive"
'

echo "corrupt data" >> "$FIXTURE_DIR/tmux-state_linux_amd64.tar.gz"
bash -c '
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  verify_checksum "'"$FIXTURE_DIR"'" "tmux-state_linux_amd64.tar.gz" \
    && echo "FAIL: verify_checksum accepted a corrupted archive" \
    || echo "PASS: verify_checksum rejects a corrupted archive"
'
```
Expected: `PASS: verify_checksum accepts a correct archive` then `PASS: verify_checksum rejects a corrupted archive`.

- [ ] **Step 4: Test the full download path against a local HTTP server (stands in for GitHub releases)**

Rebuild a clean (uncorrupted) fixture, then serve it locally and point `download_release_binary` at it by wrapping `curl` with a `PATH`-shadowed shim that rewrites the GitHub URL prefix to `localhost` — this proves the download → verify → extract → cache pipeline end to end without needing a real release:

```bash
FIXTURE_DIR="$(mktemp -d)"
mkdir -p "$FIXTURE_DIR/site/download/v0.0.0-test"
cd /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin
go build -o "$FIXTURE_DIR/tmux-state" ./cmd/tmux-state
tar -czf "$FIXTURE_DIR/site/download/v0.0.0-test/tmux-state_linux_amd64.tar.gz" -C "$FIXTURE_DIR" tmux-state
( cd "$FIXTURE_DIR/site/download/v0.0.0-test" && sha256sum tmux-state_linux_amd64.tar.gz > checksums.txt )

python3 -m http.server 8917 --directory "$FIXTURE_DIR/site" &
HTTP_PID=$!

DEST_DIR="$(mktemp -d)/bin"
bash -c '
  REPO="noamsto/tmux-state"
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  download_release_binary() {
    local version="$1" os="$2" arch="$3" dest_dir="$4"
    local asset="tmux-state_${os}_${arch}.tar.gz"
    local base_url="http://127.0.0.1:8917/download/${version}"
    local tmp_dir
    tmp_dir="$(mktemp -d)"
    trap "rm -rf \"$tmp_dir\"" RETURN
    curl -fsSL -o "$tmp_dir/$asset" "$base_url/$asset" || return 1
    curl -fsSL -o "$tmp_dir/checksums.txt" "$base_url/checksums.txt" || return 1
    verify_checksum "$tmp_dir" "$asset" || return 1
    tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" || return 1
    mkdir -p "$dest_dir"
    mv "$tmp_dir/tmux-state" "$dest_dir/tmux-state"
    chmod +x "$dest_dir/tmux-state"
  }
  download_release_binary "v0.0.0-test" "linux" "amd64" "'"$DEST_DIR"'" \
    && echo "PASS: download_release_binary fetched, verified, extracted" \
    || echo "FAIL: download_release_binary"
  [ -x "'"$DEST_DIR"'/tmux-state" ] && echo "PASS: cached binary is executable" || echo "FAIL: cached binary missing"
'
kill $HTTP_PID
```
Expected: `PASS: download_release_binary fetched, verified, extracted`, `PASS: cached binary is executable`.

Note: this proves the pipeline logic (URL construction from version/os/arch, checksum-gated extraction, caching) against a stand-in server. It does not prove `resolve_latest_version`'s redirect-parsing against a real GitHub "latest" redirect, since no `v*` tag has been pushed yet — flag this in the PR body (see Task 4).

- [ ] **Step 5: Test the caching short-circuit (no re-download when a cached binary already exists)**

```bash
CACHE_TEST_DIR="$(mktemp -d)"
mkdir -p "$CACHE_TEST_DIR/bin"
printf '#!/usr/bin/env bash\necho cached\n' > "$CACHE_TEST_DIR/bin/tmux-state"
chmod +x "$CACHE_TEST_DIR/bin/tmux-state"

bash -c '
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  # Override the CURRENT_DIR the sourced script just set, so resolve_binary looks in our fake cache dir.
  CURRENT_DIR="'"$CACHE_TEST_DIR"'"
  # Shadow command -v tmux-state to simulate "not on PATH"
  command() { if [ "$1" = "-v" ] && [ "$2" = "tmux-state" ]; then return 1; fi; builtin command "$@"; }
  result="$(resolve_binary)"
  [ "$result" = "'"$CACHE_TEST_DIR"'/bin/tmux-state" ] \
    && echo "PASS: resolve_binary reused the cached binary" \
    || echo "FAIL: resolve_binary did not use cache (got: $result)"
'
```
Note: `CURRENT_DIR` must be set *after* the `source` line — the script unconditionally reassigns `CURRENT_DIR` to its own directory at source time, which would otherwise clobber the fake cache path.

Expected: `PASS: resolve_binary reused the cached binary`. Since no network call happens on this path (the function returns before reaching `detect_os`/download), this also implicitly proves no re-download is attempted.

- [ ] **Step 6: Test the unsupported-platform failure path**

```bash
UNAME_SHIM_DIR="$(mktemp -d)"
printf '#!/usr/bin/env bash\ncase "$1" in -s) echo PlanNine ;; -m) echo mips ;; esac\n' > "$UNAME_SHIM_DIR/uname"
chmod +x "$UNAME_SHIM_DIR/uname"

bash -c '
  source /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
  command() { if [ "$1" = "-v" ] && [ "$2" = "tmux-state" ]; then return 1; fi; builtin command "$@"; }
  export PATH="'"$UNAME_SHIM_DIR"':$PATH"
  resolve_binary >/dev/null \
    && echo "FAIL: resolve_binary should have failed on an unmapped platform" \
    || echo "PASS: resolve_binary fails cleanly on an unmapped platform"
'
```
Expected: `PASS: resolve_binary fails cleanly on an unmapped platform`.

- [ ] **Step 7: Commit**

```bash
git add tmux-state.tmux
git commit -m "feat: add release-download fallback with checksum verification"
```

---

### Task 3: README updates — flip the TPM non-goal, add TPM install instructions

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Remove the stale non-goal line**

Read the surrounding lines first:
```bash
sed -n '188,193p' /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
```

Edit `README.md`: delete the line
```
- Plugin-manager packaging (TPM, etc.) — Nix is the supported install path; from-source works for everyone else
```
from the "Out of scope (likely forever)" list (leave the "Cloud sync" line above it intact).

- [ ] **Step 2: Add a TPM install subsection under `## Install`**

Read the current Install section first:
```bash
sed -n '29,56p' /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
```

Insert a new subsection after "### From source" and its `go build` block (before `## Quick start`):

```markdown
### TPM (tmux plugin manager)

```tmux
set -g @plugin 'noamsto/tmux-state'
```

Then `prefix + I` to fetch and load it. The plugin script (`tmux-state.tmux`)
resolves a `tmux-state` binary in this order: an existing copy on `PATH`, a
previously-downloaded copy cached in the plugin's own `bin/` directory, or a
fresh download of the matching prebuilt release archive (verified against its
published `checksums.txt`) for your OS/arch. It then wires the same hooks and
binds as [`examples/tmux.conf`](examples/tmux.conf).

Options (set before `run '~/.tmux/plugins/tpm/tpm'`):

| Option | Default | Meaning |
|---|---|---|
| `@tmux_state_version` | `latest` | Pin a specific release tag instead of always fetching the newest. |
| `@tmux_state_auto_restore` | `on` | Set to `off` to skip `restore --auto` on tmux start (undo/save/picker binds still work). |

The systemd/launchd save timer (see below) is not managed by the plugin — the
tmux hooks above cover structural saves (new session, new window, detach,
close); the periodic 60s snapshot timer is still a separate, optional manual
step.
```

- [ ] **Step 3: Verify the edits**

```bash
grep -n "Plugin-manager packaging" /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
grep -n "TPM (tmux plugin manager)" /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
grep -n "@tmux_state_version\|@tmux_state_auto_restore" /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
```
Expected: first `grep` prints nothing (line removed); the other two each print at least one match.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document TPM install path"
```

---

### Task 4: Final verification pass + PR

**Files:** none (verification only, then open the PR).

- [ ] **Step 1: shellcheck one more time on the final file**

```bash
shellcheck /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux
```
Expected: no output, exit 0.

- [ ] **Step 2: Re-run the Task 1 Step 4 integration test to confirm nothing regressed after Task 2's edits**

```bash
TEST_DIR="$(mktemp -d)"
printf '#!/usr/bin/env bash\necho "fake-tmux-state $*" >> "%s/calls.log"\n' "$TEST_DIR" > "$TEST_DIR/tmux-state"
chmod +x "$TEST_DIR/tmux-state"
tmux -L ts-plugin-test kill-server 2>/dev/null
env PATH="$TEST_DIR:$PATH" tmux -L ts-plugin-test new-session -d -s test
tmux -L ts-plugin-test run-shell "/home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux"
tmux -L ts-plugin-test list-hooks -g
tmux -L ts-plugin-test list-keys -T prefix | grep -E ' u | U | R | C-s '
cat "$TEST_DIR/calls.log"
tmux -L ts-plugin-test kill-server
```
Note: `PATH` must be set on the `new-session` command (server spawn), not on `run-shell` — `run-shell` jobs inherit the tmux server's captured environment, not the invoking client's.

Expected: same as Task 1 Step 4 — all 6 hooks, all 4 binds, one `restore --auto` call logged.

- [ ] **Step 3: Test the `@tmux_state_auto_restore off` toggle end to end**

```bash
TEST_DIR="$(mktemp -d)"
printf '#!/usr/bin/env bash\necho "fake-tmux-state $*" >> "%s/calls.log"\n' "$TEST_DIR" > "$TEST_DIR/tmux-state"
chmod +x "$TEST_DIR/tmux-state"
tmux -L ts-plugin-test2 kill-server 2>/dev/null
env PATH="$TEST_DIR:$PATH" tmux -L ts-plugin-test2 new-session -d -s test
tmux -L ts-plugin-test2 set-option -g "@tmux_state_auto_restore" "off"
tmux -L ts-plugin-test2 run-shell "/home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/tmux-state.tmux"
test -f "$TEST_DIR/calls.log" && grep -q "restore --auto" "$TEST_DIR/calls.log" \
  && echo "FAIL: restore --auto ran despite @tmux_state_auto_restore=off" \
  || echo "PASS: restore --auto skipped when @tmux_state_auto_restore=off"
tmux -L ts-plugin-test2 list-keys -T prefix | grep -E ' u | U | R | C-s ' && echo "PASS: binds still registered with auto_restore off"
tmux -L ts-plugin-test2 kill-server
```
Expected: `PASS: restore --auto skipped when @tmux_state_auto_restore=off`, `PASS: binds still registered with auto_restore off`.

- [ ] **Step 4: Confirm README renders sanely**

```bash
grep -c "^#" /home/noams/Data/git/noamsto/tmux-state-worktrees/feat-35-tpm-plugin-entry-script-with-release-bin/README.md
```
(Sanity check only — no broken heading nesting. Read the diff visually via `git diff README.md` and confirm the new subsection sits between "### From source" and "## Quick start" with correct heading level `###`.)

- [ ] **Step 5: Open the PR**

```bash
git push -u origin feat/35-tpm-plugin-entry-script-with-release-bin
```

```bash
gh pr create --assignee @me --title "feat: TPM plugin entry script with release binary fetch" --body "$(cat <<'EOF'
## Summary
- Add `tmux-state.tmux` at the repo root: a TPM-compatible entry script that
  resolves a `tmux-state` binary (PATH → cached download → fresh release
  download, checksum-verified against `checksums.txt`) and wires the same
  hooks/binds as `examples/tmux.conf`.
- Add `@tmux_state_version` (default `latest`) and `@tmux_state_auto_restore`
  (default `on`) tmux options.
- Flip the README: TPM is no longer a non-goal; document `set -g @plugin
  'noamsto/tmux-state'` alongside the existing Nix and from-source install
  paths. `examples/tmux.conf` is kept as the manual/non-TPM path.

## Verified
- `shellcheck tmux-state.tmux` is clean.
- Unit-tested `detect_os`/`detect_arch` mapping for all documented uname values
  and unmapped-platform failure.
- Integration-tested hook/bind wiring against a throwaway tmux server
  (`tmux -L ts-plugin-test`) with a fake `tmux-state` on PATH: all 6 hooks,
  all 4 binds, and the auto-restore `run-shell` call are present; verified the
  `@tmux_state_auto_restore=off` toggle skips the restore call while keeping
  binds.
- Verified checksum verification (`verify_checksum`) accepts a correct
  archive and rejects a corrupted one, using a locally `go build`-produced
  `tmux-state` binary and a hand-computed `checksums.txt`.
- Verified the full download → checksum-verify → extract → cache pipeline
  (`download_release_binary`) against a local `python3 -m http.server`
  standing in for GitHub releases.
- Verified the caching short-circuit (existing cached binary is reused, no
  re-download attempted) and the unsupported-platform failure path (clean
  `display-message`, no hooks wired).

## NOT verified (documented limitation)
- `gh release list` is currently empty — no `v*` tag has ever been pushed, so
  there is no real GitHub release to test against. `resolve_latest_version`'s
  parsing of the real `https://github.com/noamsto/tmux-state/releases/latest`
  redirect, and a real download of a `tmux-state_<os>_<arch>.tar.gz` asset
  plus its `checksums.txt`, are untested end-to-end. Pushing a `v*` tag (the
  maintainer's call, not done here) is the prerequisite to exercise this live.

## Test plan
- [x] `shellcheck tmux-state.tmux` clean
- [x] Hook/bind wiring verified against a throwaway tmux server
- [x] Checksum verification unit-tested against a local fixture
- [x] Download pipeline verified against a local HTTP stand-in server
- [ ] Live GitHub release download — blocked on first `v*` tag push
EOF
)"
```

---

## Self-Review Notes

- **Spec coverage:** binary resolution order (PATH → cache → download) — Task 1 Step 1 + Task 2 Step 1 `resolve_binary`. os/arch mapping — Task 1. Version option + `latest` resolution — Task 2. Checksum verification with sha256sum/shasum fallback — Task 2 `verify_checksum`. Failure message + no-hook-wiring on failure — `main()` in Task 1, unchanged by Task 2. Exact hook/bind wiring — Task 1 `wire_plugin`. Auto-restore toggle option — Task 1 `wire_plugin` + Task 4 Step 3 test. README non-goal flip + TPM subsection — Task 3. PR body documenting the untested live-download limitation — Task 4 Step 5. Caching (reuse extracted binary) — Task 2 Step 1 + Step 5 test.
- **Placeholder scan:** no TBD/TODO markers; every step has literal, runnable code or commands.
- **Type/name consistency:** `resolve_binary`, `wire_plugin`, `detect_os`, `detect_arch`, `tmux_option_or_default`, `resolve_latest_version`, `verify_checksum`, `download_release_binary`, `CURRENT_DIR`, `REPO` are used identically across Task 1 and Task 2's diff — Task 2 replaces the Task-1 stub `resolve_binary` in place rather than introducing a second function with a similar name.
