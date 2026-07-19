# Agent resume-on-restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stamp `@remux_relaunch` on agent panes (Claude/Codex/Cursor) at session start so tmux-remux restores the exact prior session instead of a bare shell.

**Architecture:** One generic `tmux-remux relaunch-stamp` subcommand reads a hook's stdin JSON, substitutes the `session_id` into a per-agent resume template, and stamps the pane option via a new `internal/tmux` write path. A `tmux-remux install-hook` subcommand wires the hook into each agent's native config (Codex = `config.toml` append; Cursor = `hooks.json` merge). Claude ships as an installable Claude Code plugin.

**Tech Stack:** Go 1.25.5, cobra CLI, pure-Go (no CGO). Tests are standard `go test`.

## Global Constraints

- Module path: `github.com/noamsto/tmux-remux`. Go 1.25.5, no CGO.
- All automated tests are Go — no `bats`.
- A start hook must **never fail** the agent: `relaunch-stamp` returns nil on no-pane / no-id / pane-gone (only a wiring bug like an unknown `--agent` errors).
- The tmux write uses `set-option -pq` (quiet) — a vanished pane must not surface an error.
- Idempotency keys are **fixed sentinels**, never command-string equality: Codex uses a literal marker comment line; Cursor keys on a hook `command` containing the substring `relaunch-stamp`.
- Spec: `docs/superpowers/specs/2026-07-15-agent-resume-on-restore-design.md`.

---

### Task 1: `SetPaneOption` write path in `internal/tmux`

**Files:**
- Modify: `internal/tmux/client.go` (add method after `ServerStartTime`)
- Test: `internal/tmux/client_test.go` (uses existing `writeFakeTmux` helper)

**Interfaces:**
- Produces: `func (c *Client) SetPaneOption(ctx context.Context, pane, name, value string) error`

- [ ] **Step 1: Write the failing test**

Add to `internal/tmux/client_test.go`:

```go
func TestSetPaneOptionIssuesQuietPaneScopedArgs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	fake := writeFakeTmux(t, fmt.Sprintf(`printf '%%s\n' "$@" > %s`, argsFile))
	c := tmux.NewClient(fake)

	if err := c.SetPaneOption(context.Background(), "%3", "@remux_relaunch", "claude --resume abc-123"); err != nil {
		t.Fatalf("SetPaneOption: %v", err)
	}

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "set-option\n-pq\n-t\n%3\n@remux_relaunch\nclaude --resume abc-123\n"
	if string(got) != want {
		t.Errorf("tmux args =\n%q\nwant\n%q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tmux/ -run TestSetPaneOption -v`
Expected: FAIL — `c.SetPaneOption undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/tmux/client.go` (after the `ServerStartTime` method):

```go
// SetPaneOption sets a pane-scoped option (tmux set-option -p). The -q flag
// keeps it quiet: a pane that vanished between a hook firing and this call must
// not surface an error to the caller. Pass value="" to unset.
func (c *Client) SetPaneOption(ctx context.Context, pane, name, value string) error {
	_, err := c.Run(ctx, []string{"set-option", "-pq", "-t", pane, name, value})
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tmux/ -run TestSetPaneOption -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): add SetPaneOption write path (#46)"
```

---

### Task 2: `relaunch-stamp` subcommand (Claude + Codex presets)

**Files:**
- Create: `cmd/tmux-remux/relaunch_stamp.go`
- Create: `cmd/tmux-remux/relaunch_stamp_test.go`
- Modify: `cmd/tmux-remux/main.go:55-66` (register the command)

**Interfaces:**
- Consumes: `tmux.Client.SetPaneOption` (Task 1).
- Produces: `newRelaunchStampCmd() *cobra.Command`; `runRelaunchStamp(ctx, setter paneOptionSetter, r io.Reader, opts relaunchStampOpts) error`; `type relaunchStampOpts struct{ agent, cmdTemplate, idField, pane string; clear bool }`; `type paneOptionSetter interface{ SetPaneOption(ctx context.Context, pane, name, value string) error }`.

- [ ] **Step 1: Write the failing tests**

Create `cmd/tmux-remux/relaunch_stamp_test.go`:

```go
package main

import (
	"context"
	"strings"
	"testing"
)

type setCall struct{ pane, name, value string }

type fakeSetter struct {
	calls []setCall
	err   error
}

func (f *fakeSetter) SetPaneOption(_ context.Context, pane, name, value string) error {
	f.calls = append(f.calls, setCall{pane, name, value})
	return f.err
}

func run(t *testing.T, stdin string, opts relaunchStampOpts) *fakeSetter {
	t.Helper()
	f := &fakeSetter{}
	if err := runRelaunchStamp(context.Background(), f, strings.NewReader(stdin), opts); err != nil {
		t.Fatalf("runRelaunchStamp: %v", err)
	}
	return f
}

func TestRelaunchStampClaudePreset(t *testing.T) {
	f := run(t, `{"session_id":"abc-123"}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	want := setCall{"%3", "@remux_relaunch", "claude --resume abc-123"}
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("calls = %+v, want [%+v]", f.calls, want)
	}
}

func TestRelaunchStampCodexPresetPositional(t *testing.T) {
	f := run(t, `{"session_id":"t-9"}`, relaunchStampOpts{agent: "codex", pane: "%1"})
	if len(f.calls) != 1 || f.calls[0].value != "codex resume t-9" {
		t.Errorf("calls = %+v, want value \"codex resume t-9\"", f.calls)
	}
}

func TestRelaunchStampCmdEscapeHatch(t *testing.T) {
	f := run(t, `{"session_id":"z"}`, relaunchStampOpts{cmdTemplate: "cursor-agent --resume {id}", pane: "%1"})
	if len(f.calls) != 1 || f.calls[0].value != "cursor-agent --resume z" {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestRelaunchStampClearUnsetsWithoutStdin(t *testing.T) {
	f := run(t, "", relaunchStampOpts{pane: "%2", clear: true})
	want := setCall{"%2", "@remux_relaunch", ""}
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("calls = %+v, want [%+v]", f.calls, want)
	}
}

func TestRelaunchStampNoIDDoesNotClobber(t *testing.T) {
	f := run(t, `{}`, relaunchStampOpts{agent: "claude", pane: "%3"})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampNoPaneNoOp(t *testing.T) {
	f := run(t, `{"session_id":"x"}`, relaunchStampOpts{agent: "claude", pane: ""})
	if len(f.calls) != 0 {
		t.Errorf("expected no set call, got %+v", f.calls)
	}
}

func TestRelaunchStampUnknownAgentErrors(t *testing.T) {
	f := &fakeSetter{}
	err := runRelaunchStamp(context.Background(), f, strings.NewReader(`{"session_id":"x"}`),
		relaunchStampOpts{agent: "bogus", pane: "%1"})
	if err == nil {
		t.Fatal("expected error for unknown agent with no --cmd")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/tmux-remux/ -run TestRelaunchStamp -v`
Expected: FAIL — `undefined: runRelaunchStamp`, `relaunchStampOpts`, etc.

- [ ] **Step 3: Write the implementation**

Create `cmd/tmux-remux/relaunch_stamp.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

const relaunchOption = "@remux_relaunch"

// relaunchPreset maps an --agent name to how its resume command is built.
// "{id}" in cmdTemplate is replaced with the session id read from the hook's
// stdin JSON field idField.
type relaunchPreset struct {
	cmdTemplate string
	idField     string
}

// relaunchPresets: cursor is added later, gated on empirical verification
// (see plan Task 6).
var relaunchPresets = map[string]relaunchPreset{
	"claude": {cmdTemplate: "claude --resume {id}", idField: "session_id"},
	"codex":  {cmdTemplate: "codex resume {id}", idField: "session_id"},
}

// paneOptionSetter is the subset of *tmux.Client relaunch-stamp needs, so tests
// can assert the (pane, name, value) triple without a real tmux server.
type paneOptionSetter interface {
	SetPaneOption(ctx context.Context, pane, name, value string) error
}

type relaunchStampOpts struct {
	agent       string
	cmdTemplate string
	idField     string
	pane        string
	clear       bool
}

// newRelaunchStampCmd is an INTERNAL helper meant to be an agent start-hook
// target: it reads the hook payload from stdin and stamps @remux_relaunch on
// the current pane so restore reopens the agent session, not a bare shell.
func newRelaunchStampCmd() *cobra.Command {
	var o relaunchStampOpts
	c := &cobra.Command{
		Use:    "relaunch-stamp",
		Short:  "Stamp @remux_relaunch from an agent start hook (internal helper)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.pane == "" {
				o.pane = os.Getenv("TMUX_PANE")
			}
			return runRelaunchStamp(cmd.Context(), tmux.NewClient(""), os.Stdin, o)
		},
	}
	c.Flags().StringVar(&o.agent, "agent", "", "agent preset: claude|codex")
	c.Flags().StringVar(&o.cmdTemplate, "cmd", "", "resume command template with {id} (overrides --agent)")
	c.Flags().StringVar(&o.idField, "id-field", "", "stdin JSON field holding the session id (default session_id)")
	c.Flags().StringVar(&o.pane, "pane", "", "target pane id (default $TMUX_PANE)")
	c.Flags().BoolVar(&o.clear, "clear", false, "unset @remux_relaunch instead of stamping")
	return c
}

// runRelaunchStamp reads a hook JSON payload from r, derives the resume command,
// and stamps @remux_relaunch on opts.pane. It no-ops (returns nil) with no pane,
// or (stamp mode) when no session id is present — it must never fail a start
// hook. Only a wiring bug (unknown --agent and no --cmd) returns an error. The
// SetPaneOption error is intentionally swallowed: the pane may have vanished
// between the hook firing and this call (SetPaneOption already uses -q).
func runRelaunchStamp(ctx context.Context, setter paneOptionSetter, r io.Reader, opts relaunchStampOpts) error {
	if opts.pane == "" {
		return nil
	}
	if opts.clear {
		_ = setter.SetPaneOption(ctx, opts.pane, relaunchOption, "")
		return nil
	}

	tmpl, idField := opts.cmdTemplate, opts.idField
	if tmpl == "" {
		preset, ok := relaunchPresets[opts.agent]
		if !ok {
			return fmt.Errorf("unknown --agent %q (and no --cmd)", opts.agent)
		}
		tmpl = preset.cmdTemplate
		if idField == "" {
			idField = preset.idField
		}
	}
	if idField == "" {
		idField = "session_id"
	}

	id := parseSessionID(r, idField)
	if id == "" {
		return nil
	}
	value := strings.ReplaceAll(tmpl, "{id}", id)
	_ = setter.SetPaneOption(ctx, opts.pane, relaunchOption, value)
	return nil
}

// parseSessionID decodes the hook payload and returns the string value of
// field, or "" if the payload is unreadable/absent/non-string.
func parseSessionID(r io.Reader, field string) string {
	data, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	s, _ := payload[field].(string)
	return s
}
```

Register it in `cmd/tmux-remux/main.go` — add `newRelaunchStampCmd(),` to the `root.AddCommand(...)` list (after `newCatScrollbackCmd(),`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/tmux-remux/ -run TestRelaunchStamp -v`
Expected: PASS (all 7).

- [ ] **Step 5: Verify build + full suite**

Run: `go build ./... && go test ./...`
Expected: build clean, all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/tmux-remux/relaunch_stamp.go cmd/tmux-remux/relaunch_stamp_test.go cmd/tmux-remux/main.go
git commit -m "feat: relaunch-stamp subcommand for Claude/Codex resume-on-restore (#46)"
```

---

### Task 3: `install-hook codex` (config.toml append)

**Files:**
- Create: `internal/agenthook/codex.go`
- Create: `internal/agenthook/codex_test.go`
- Create: `cmd/tmux-remux/install_hook.go`
- Create: `cmd/tmux-remux/install_hook_test.go`
- Modify: `cmd/tmux-remux/main.go` (register `newInstallHookCmd()`)

**Interfaces:**
- Produces: `agenthook.InstallCodex(path, binary string) (changed bool, err error)`; `newInstallHookCmd() *cobra.Command`; `runInstallHook(agent, self, home string, w io.Writer) error`.

- [ ] **Step 1: Write the failing test for `InstallCodex`**

Create `internal/agenthook/codex_test.go`:

```go
package agenthook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCodexCreatesFileWithBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codex", "config.toml")
	changed, err := InstallCodex(path, "/usr/bin/tmux-remux")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed = false, want true on fresh install")
	}
	got, _ := os.ReadFile(path)
	body := string(got)
	if !strings.Contains(body, codexMarker) {
		t.Error("marker not written")
	}
	if !strings.Contains(body, `command = "/usr/bin/tmux-remux relaunch-stamp --agent codex"`) {
		t.Errorf("hook command missing:\n%s", body)
	}
}

func TestInstallCodexPreservesExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := InstallCodex(path, "/x")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), "model = \"gpt\"\n") {
		t.Errorf("existing content not preserved:\n%s", got)
	}
}

func TestInstallCodexIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := InstallCodex(path, "/x"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := InstallCodex(path, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("changed = true on second install, want false")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("file mutated on idempotent re-install")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agenthook/ -run TestInstallCodex -v`
Expected: FAIL — package/`InstallCodex` undefined.

- [ ] **Step 3: Write `InstallCodex`**

Create `internal/agenthook/codex.go`:

```go
// Package agenthook wires agent CLI start hooks to tmux-remux relaunch-stamp so
// panes restore their exact prior session. Each installer is idempotent and
// preserves the user's existing config.
package agenthook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// codexMarker guards our managed block in ~/.codex/config.toml. Codex auto-loads
// only that one global file (no drop-in hooks dir), so we marker-append rather
// than own a file — matching lazytmux's proven approach.
const codexMarker = "# tmux-remux-managed: codex resume-on-restore SessionStart hook"

// InstallCodex idempotently appends a SessionStart hook block to the Codex
// config at path, wiring `<binary> relaunch-stamp --agent codex`. Creates the
// file if absent, never rewrites existing content, no-ops (changed=false) when
// the marker is already present. binary should be an absolute path.
func InstallCodex(path, binary string) (changed bool, err error) {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(existing), codexMarker) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(codexBlock(binary)); err != nil {
		return false, err
	}
	return true, nil
}

func codexBlock(binary string) string {
	return fmt.Sprintf(`
%s
[[hooks.SessionStart]]
matcher = "startup|resume"

[[hooks.SessionStart.hooks]]
type = "command"
command = %q
timeout = 30
`, codexMarker, binary+" relaunch-stamp --agent codex")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agenthook/ -run TestInstallCodex -v`
Expected: PASS (all 3).

- [ ] **Step 5: Write the failing test for the command**

Create `cmd/tmux-remux/install_hook_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInstallHookCodexWiresAndPrintsTrustStep(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if err := runInstallHook("codex", "/usr/bin/tmux-remux", home, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Errorf("config.toml not created: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "Wired codex") {
		t.Errorf("missing wired message: %q", s)
	}
	if !strings.Contains(s, "Trust all") {
		t.Errorf("missing mandatory trust-step notice: %q", s)
	}
}

func TestRunInstallHookIdempotentReportsNoChange(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	_ = runInstallHook("codex", "/x", home, &out)
	out.Reset()
	if err := runInstallHook("codex", "/x", home, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already present") {
		t.Errorf("expected no-change message, got %q", out.String())
	}
}

func TestRunInstallHookUnknownAgentErrors(t *testing.T) {
	if err := runInstallHook("emacs", "/x", t.TempDir(), &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./cmd/tmux-remux/ -run TestRunInstallHook -v`
Expected: FAIL — `undefined: runInstallHook`.

- [ ] **Step 7: Write `install_hook.go`**

Create `cmd/tmux-remux/install_hook.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-remux/internal/agenthook"
)

// newInstallHookCmd wires an agent CLI's start hook to `relaunch-stamp` so its
// panes restore their prior session. Claude ships as a plugin instead; this
// covers the agents with no plugin path (Codex; Cursor added later).
func newInstallHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-hook <codex>",
		Short: "Wire an agent start hook for resume-on-restore",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve own path: %w", err)
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			return runInstallHook(args[0], self, home, cmd.OutOrStdout())
		},
	}
}

func runInstallHook(agent, self, home string, w io.Writer) error {
	switch agent {
	case "codex":
		path := filepath.Join(home, ".codex", "config.toml")
		changed, err := agenthook.InstallCodex(path, self)
		if err != nil {
			return err
		}
		reportHook(w, "codex", path, changed)
		fmt.Fprintln(w, `
IMPORTANT: run `+"`codex`"+`, open /hooks, and choose "Trust all" once per machine.
The hook will not run until you do — the trust hash cannot be pre-seeded.`)
		return nil
	default:
		return fmt.Errorf("unknown agent %q (want: codex)", agent)
	}
}

func reportHook(w io.Writer, agent, path string, changed bool) {
	if changed {
		fmt.Fprintf(w, "Wired %s resume-on-restore hook → %s\n", agent, path)
		return
	}
	fmt.Fprintf(w, "%s resume-on-restore hook already present → %s (no change)\n", agent, path)
}
```

Register it in `cmd/tmux-remux/main.go` — add `newInstallHookCmd(),` to `root.AddCommand(...)`.

- [ ] **Step 8: Run tests + build**

Run: `go test ./cmd/tmux-remux/ -run TestRunInstallHook -v && go build ./... && go test ./...`
Expected: all PASS, build clean.

- [ ] **Step 9: Commit**

```bash
git add internal/agenthook/ cmd/tmux-remux/install_hook.go cmd/tmux-remux/install_hook_test.go cmd/tmux-remux/main.go
git commit -m "feat: install-hook codex wiring (#46)"
```

---

### Task 4: Claude Code plugin

**Files:**
- Create: `claude-plugin/.claude-plugin/plugin.json`
- Create: `claude-plugin/hooks/hooks.json`
- Create: `claude-plugin/README.md`
- Create: `.claude-plugin/marketplace.json` (repo root)

**Interfaces:**
- Consumes: the `relaunch-stamp` subcommand (Task 2) via `tmux-remux relaunch-stamp --agent claude` / `--clear` (binary on `PATH`).

- [ ] **Step 1: Write `plugin.json`**

Create `claude-plugin/.claude-plugin/plugin.json`:

```json
{
  "name": "tmux-remux",
  "version": "0.1.0",
  "description": "Resume-on-restore: stamps @remux_relaunch so tmux-remux restores this Claude session instead of a bare shell"
}
```

- [ ] **Step 2: Write `hooks.json`**

Create `claude-plugin/hooks/hooks.json`:

```json
{
  "hooks": {
    "SessionStart": [
      { "matcher": "startup", "hooks": [{"type": "command", "command": "tmux-remux relaunch-stamp --agent claude"}] },
      { "matcher": "resume", "hooks": [{"type": "command", "command": "tmux-remux relaunch-stamp --agent claude"}] }
    ],
    "SessionEnd": [
      { "hooks": [{"type": "command", "command": "tmux-remux relaunch-stamp --clear"}] }
    ]
  }
}
```

- [ ] **Step 3: Write `marketplace.json`**

Create `.claude-plugin/marketplace.json` (repo root):

```json
{
  "name": "tmux-remux",
  "owner": { "name": "noamsto" },
  "plugins": [
    { "name": "tmux-remux", "source": "./claude-plugin", "description": "Resume-on-restore for Claude Code panes under tmux-remux" }
  ]
}
```

- [ ] **Step 4: Write the plugin README**

Create `claude-plugin/README.md`:

```markdown
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
```

- [ ] **Step 5: Validate the JSON files**

Run: `for f in claude-plugin/.claude-plugin/plugin.json claude-plugin/hooks/hooks.json .claude-plugin/marketplace.json; do jq . "$f" >/dev/null && echo "ok $f"; done`
Expected: `ok` for all three (valid JSON).

- [ ] **Step 6: Commit**

```bash
git add claude-plugin/ .claude-plugin/
git commit -m "feat: Claude Code plugin for resume-on-restore (#46)"
```

---

### Task 5: Documentation (README + subcommand table)

**Files:**
- Modify: `README.md` (Subcommands table + new section)

**Interfaces:** none (docs).

- [ ] **Step 1: Add the two subcommands to the Subcommands table**

In `README.md`, add these rows to the subcommand table, before the `version` row, matching existing style:

```markdown
| `tmux-remux relaunch-stamp` | Stamp `@remux_relaunch` from an agent start hook so restore reopens the session (internal; wired via the Claude plugin or `install-hook`) |
| `tmux-remux install-hook codex` | Wire Codex's `SessionStart` hook (`~/.codex/config.toml`) to `relaunch-stamp` |
```

- [ ] **Step 2: Add an "Agent resume-on-restore" section**

Add after the "Per-pane relaunch override" paragraph in `README.md`:

```markdown
### Agent resume-on-restore

tmux-remux can stamp `@remux_relaunch` automatically for agent CLIs, so a pane
running Claude Code, Codex, or Cursor restores as its exact prior session:

- **Claude Code** — install the bundled Claude Code plugin (`claude-plugin/`,
  see its README): a `SessionStart` hook stamps `claude --resume <id>`, a
  `SessionEnd` hook clears it. Remote Control reconnects for free on resume.
- **Codex** — `tmux-remux install-hook codex` appends a `SessionStart` hook to
  `~/.codex/config.toml`, then run `codex` → `/hooks` → "Trust all" once per
  machine (Codex requires manual trust; it cannot be pre-seeded). Note Codex's
  hook fires only after the first turn, so a brand-new Codex pane snapshotted
  before its first turn restores as a shell.

All three share one binary core (`relaunch-stamp`). The stamp is exec'd verbatim
on restore via the `@remux_relaunch` override.
```

- [ ] **Step 3: Verify the README renders**

Run: `rg -n "relaunch-stamp|install-hook codex|Agent resume-on-restore" README.md`
Expected: matches in the table and the new section.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: document agent resume-on-restore (#46)"
```

---

### Task 6: Cursor adapter — VERIFY-GATED

**Files (only if the gate passes):**
- Modify: `cmd/tmux-remux/relaunch_stamp.go` (add cursor preset + flag help)
- Modify: `cmd/tmux-remux/relaunch_stamp_test.go` (cursor preset test)
- Create: `internal/agenthook/cursor.go`
- Create: `internal/agenthook/cursor_test.go`
- Modify: `cmd/tmux-remux/install_hook.go` (add `cursor` case + `Use` string)
- Modify: `cmd/tmux-remux/install_hook_test.go` (cursor case test)
- Modify: `README.md` and `claude-plugin/README.md`-adjacent docs (mention cursor)

**Interfaces:**
- Produces: `agenthook.InstallCursor(path, binary string) (changed bool, err error)`; a `"cursor"` entry in `relaunchPresets`; a `"cursor"` case in `runInstallHook`.

- [ ] **Step 1: THE GATE — empirical verification (manual, blocking)**

On a machine with `cursor-agent` installed:

1. Add a temporary `sessionStart` hook that dumps stdin:
   `~/.cursor/hooks.json` → `{"version":1,"hooks":{"sessionStart":[{"command":"cat > /tmp/cursor-hook.json","timeout":10}]}}`
2. Start `cursor-agent` in a tmux pane; confirm `/tmp/cursor-hook.json` exists and read `session_id` from it (proves `sessionStart` fires under the CLI).
3. Run `cursor-agent --resume <that session_id>` and confirm it reloads that session, and that the id appears in `cursor-agent ls`.

- **If any step fails:** STOP. Do not implement the rest of this task. Comment on issue #46 with what failed, and leave the Cursor design note in the spec as a future follow-up (fallback: mint the id up front with `cursor-agent create-chat`). The Claude + Codex features shipped in Tasks 1-5 are complete and independent.
- **If all steps pass:** remove the temporary hook and continue to Step 2.

- [ ] **Step 2: Write the failing preset test**

Add to `cmd/tmux-remux/relaunch_stamp_test.go`:

```go
func TestRelaunchStampCursorPreset(t *testing.T) {
	f := run(t, `{"session_id":"c-77"}`, relaunchStampOpts{agent: "cursor", pane: "%4"})
	if len(f.calls) != 1 || f.calls[0].value != "cursor-agent --resume c-77" {
		t.Errorf("calls = %+v, want value \"cursor-agent --resume c-77\"", f.calls)
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./cmd/tmux-remux/ -run TestRelaunchStampCursorPreset -v`
Expected: FAIL — unknown agent "cursor" (errors, no call recorded).

- [ ] **Step 4: Add the cursor preset**

In `cmd/tmux-remux/relaunch_stamp.go`, add to `relaunchPresets`:

```go
	"cursor": {cmdTemplate: "cursor-agent --resume {id}", idField: "session_id"},
```

Update the `--agent` flag help string to `"agent preset: claude|codex|cursor"`.

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./cmd/tmux-remux/ -run TestRelaunchStampCursorPreset -v`
Expected: PASS.

- [ ] **Step 6: Write the failing `InstallCursor` test**

Create `internal/agenthook/cursor_test.go`:

```go
package agenthook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCursor(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return doc
}

func firstCommand(doc map[string]any, event string) string {
	hooks, _ := doc["hooks"].(map[string]any)
	list, _ := hooks[event].([]any)
	if len(list) == 0 {
		return ""
	}
	m, _ := list[0].(map[string]any)
	s, _ := m["command"].(string)
	return s
}

func TestInstallCursorCreatesV1WithBothHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cursor", "hooks.json")
	changed, err := InstallCursor(path, "/usr/bin/tmux-remux")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	doc := readCursor(t, path)
	if v, _ := doc["version"].(float64); v != 1 {
		t.Errorf("version = %v, want 1", doc["version"])
	}
	if got := firstCommand(doc, "sessionStart"); !strings.Contains(got, "relaunch-stamp --agent cursor") {
		t.Errorf("sessionStart command = %q", got)
	}
	if got := firstCommand(doc, "sessionEnd"); !strings.Contains(got, "relaunch-stamp --clear") {
		t.Errorf("sessionEnd command = %q", got)
	}
}

func TestInstallCursorPreservesExistingEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	seed := `{"version":1,"hooks":{"sessionStart":[{"command":"echo hi","timeout":5}]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallCursor(path, "/x"); err != nil {
		t.Fatal(err)
	}
	doc := readCursor(t, path)
	hooks, _ := doc["hooks"].(map[string]any)
	list, _ := hooks["sessionStart"].([]any)
	if len(list) != 2 {
		t.Fatalf("sessionStart len = %d, want 2 (existing + ours)", len(list))
	}
	if firstCommand(doc, "sessionStart") != "echo hi" {
		t.Error("existing entry not preserved as first element")
	}
}

func TestInstallCursorIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	if _, err := InstallCursor(path, "/x"); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	changed, err := InstallCursor(path, "/x")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("changed = true on re-install, want false")
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("file mutated on idempotent re-install")
	}
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `go test ./internal/agenthook/ -run TestInstallCursor -v`
Expected: FAIL — `InstallCursor` undefined.

- [ ] **Step 8: Write `InstallCursor`**

Create `internal/agenthook/cursor.go`:

```go
package agenthook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// cursorSentinel identifies our managed entries. Cursor's hooks.json is JSON
// (no comments), so idempotency keys on a command containing this substring —
// stable across absolute-path/whitespace changes, unlike full-string equality.
const cursorSentinel = "relaunch-stamp"

// InstallCursor idempotently merges sessionStart (stamp) and sessionEnd (clear)
// entries into the Cursor v1 hooks.json at path, preserving all existing
// content. No-ops (changed=false) when a sessionStart entry already references
// our sentinel. binary should be an absolute path.
func InstallCursor(path, binary string) (changed bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	doc := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return false, err
		}
	}
	if _, ok := doc["version"]; !ok {
		doc["version"] = 1
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		doc["hooks"] = hooks
	}

	if cursorHasSentinel(hooks["sessionStart"]) {
		return false, nil
	}
	hooks["sessionStart"] = appendCursorEntry(hooks["sessionStart"], binary+" relaunch-stamp --agent cursor")
	hooks["sessionEnd"] = appendCursorEntry(hooks["sessionEnd"], binary+" relaunch-stamp --clear")

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func cursorHasSentinel(v any) bool {
	list, _ := v.([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		if cmd, _ := m["command"].(string); strings.Contains(cmd, cursorSentinel) {
			return true
		}
	}
	return false
}

func appendCursorEntry(v any, command string) []any {
	list, _ := v.([]any)
	return append(list, map[string]any{"command": command, "timeout": 30})
}
```

- [ ] **Step 9: Run to verify it passes**

Run: `go test ./internal/agenthook/ -run TestInstallCursor -v`
Expected: PASS (all 3).

- [ ] **Step 10: Wire the cursor case into `install-hook`**

Add a failing test to `cmd/tmux-remux/install_hook_test.go`:

```go
func TestRunInstallHookCursorWires(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if err := runInstallHook("cursor", "/usr/bin/tmux-remux", home, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "hooks.json")); err != nil {
		t.Errorf("hooks.json not created: %v", err)
	}
	if !strings.Contains(out.String(), "Wired cursor") {
		t.Errorf("missing wired message: %q", out.String())
	}
}
```

Run: `go test ./cmd/tmux-remux/ -run TestRunInstallHookCursor -v` → FAIL (unknown agent). Then add the `cursor` case to `runInstallHook` in `cmd/tmux-remux/install_hook.go`, before the `default`:

```go
	case "cursor":
		path := filepath.Join(home, ".cursor", "hooks.json")
		changed, err := agenthook.InstallCursor(path, self)
		if err != nil {
			return err
		}
		reportHook(w, "cursor", path, changed)
		return nil
```

Update the command `Use` to `"install-hook <codex|cursor>"` and the `default` error to `want: codex, cursor`. Re-run the test → PASS.

- [ ] **Step 11: Update docs**

In `README.md`: add `| tmux-remux install-hook cursor | Wire Cursor's sessionStart/sessionEnd hooks (~/.cursor/hooks.json) |` to the table, and extend the "Agent resume-on-restore" section with a Cursor bullet noting `tmux-remux install-hook cursor`.

- [ ] **Step 12: Full suite + commit**

Run: `go build ./... && go test ./...`
Expected: all PASS.

```bash
git add cmd/tmux-remux/ internal/agenthook/ README.md
git commit -m "feat: Cursor resume-on-restore adapter (verified) (#46)"
```

---

## Notes for the implementer

- After all tasks: `go vet ./...` and `gofmt -l .` (expect no output) before opening the PR.
- Tasks 1-5 are independent of Task 6; ship them even if the Cursor gate fails.
- The lazytmux polling-stamp retirement is a **separate follow-up in the lazytmux repo** (see spec Open items) — not part of this plan.
