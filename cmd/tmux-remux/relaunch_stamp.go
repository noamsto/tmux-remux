package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/tmux"
)

const relaunchOption = "@remux_relaunch"

// relaunchOwnerOption binds @remux_relaunch to the process that owns it, so
// snapshot.Build can drop a stamp that outlived its agent.
const relaunchOwnerOption = "@remux_relaunch_owner"

// sessionIDPattern bounds hook-supplied session ids to a shell-safe charset.
// The id is interpolated into a resume command that restore later exec's
// verbatim via /bin/sh -c (see restore.BuildStartupCommand OverrideCmd), so an
// id carrying shell metacharacters (spaces, ;, $, backticks) must never reach
// the @remux_relaunch pane option. Agent session ids are UUIDs in practice.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

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
	PaneProcess(ctx context.Context, pane string) (int, string, error)
}

// isShellComm reports whether a process comm names an interactive shell: a
// built-in idle shell, tmux's default-shell, or the user's login shell. A
// login shell's leading "-" and a nix wrapper's ".X-wrapped" name are
// normalised first.
func isShellComm(comm, defaultShell, loginShell string) bool {
	name := strings.TrimPrefix(comm, "-")
	if inner, ok := strings.CutPrefix(name, "."); ok {
		if base, ok := strings.CutSuffix(inner, "-wrapped"); ok {
			name = base
		}
	}
	if filter.IsShell(name) {
		return true
	}
	for _, sh := range []string{defaultShell, loginShell} {
		if sh != "" && name == filepath.Base(sh) {
			return true
		}
	}
	return false
}

// relaunchOwner returns the pid and start time of the process that owns a
// stamp written by self inside the pane whose process is panePID, or (0, 0)
// when there is no durable owner. The walk from self up to panePID also proves
// the hook runs inside that pane.
//
// When the pane process is not a shell, the agent is the pane process
// (`tmux new-window claude`, `exec claude`), so it owns the stamp however deep
// the hook's own helpers nest beneath it. When the pane process is a shell, the
// agent was started from it, possibly via nested shells (`bash`, `nix shell`)
// that outlive it, so the owner is the first non-shell process on the way down
// from the pane to self. If there is none, relaunch-stamp was run by hand at a
// prompt and nothing durable owns the stamp; self being the pane process is
// likewise transient. A long-lived non-shell launcher between the pane shell
// and the agent (nvim's :terminal, `nix develop`'s nix) still becomes the
// owner and keeps the stamp while it lives.
func relaunchOwner(self, panePID int, info func(int) (snapshot.Proc, error), isShell func(string) bool) (pid int, start int64) {
	if self == panePID {
		return 0, 0
	}
	chain := paneChain(self, panePID, info)
	if chain == nil {
		return 0, 0
	}
	pane, err := info(panePID)
	if err != nil {
		return 0, 0
	}
	owner := panePID
	if isShell(pane.Comm) {
		owner = 0
		for i := len(chain) - 1; i > 0; i-- {
			if !isShell(chain[i].comm) {
				owner = chain[i].pid
				break
			}
		}
		if owner == 0 {
			return 0, 0
		}
	}
	p, err := info(owner)
	if err != nil {
		return 0, 0
	}
	return owner, p.Start
}

type chainProc struct {
	pid  int
	comm string
}

// paneChain returns self's ancestors from self up to the child of panePID, or
// nil when the chain breaks, reaches init, or runs too deep.
func paneChain(self, panePID int, info func(int) (snapshot.Proc, error)) []chainProc {
	var chain []chainProc
	pid := self
	for range 64 {
		p, err := info(pid)
		if err != nil || p.PPID <= 1 {
			return nil
		}
		chain = append(chain, chainProc{pid: pid, comm: p.Comm})
		if p.PPID == panePID {
			return chain
		}
		pid = p.PPID
	}
	return nil
}

type relaunchStampOpts struct {
	agent       string
	cmdTemplate string
	idField     string
	pane        string
	clear       bool
	// owner resolves the stamp's owner from the pane's process and tmux
	// default-shell; nil or a 0 pid leaves the stamp owner-less.
	owner func(panePID int, defaultShell string) (int, int64)
}

// RelaunchStampCmd is an INTERNAL helper meant to be an agent start-hook target:
// it reads the hook payload from stdin and stamps @remux_relaunch on the current
// pane so restore reopens the agent session, not a bare shell.
type RelaunchStampCmd struct {
	Agent   string `help:"agent preset: claude|codex"`
	Cmd     string `help:"resume command template with {id} (overrides --agent)"`
	IDField string `name:"id-field" help:"stdin JSON field holding the session id (default session_id)"`
	Pane    string `help:"target pane id (default $TMUX_PANE)"`
	Clear   bool   `help:"unset @remux_relaunch instead of stamping"`
}

func (c RelaunchStampCmd) Run() error {
	if c.Pane == "" {
		c.Pane = os.Getenv("TMUX_PANE")
	}
	ctx, cancel := signalCtx()
	defer cancel()
	return runRelaunchStamp(ctx, tmux.NewClient(""), os.Stdin, relaunchStampOpts{
		agent:       c.Agent,
		cmdTemplate: c.Cmd,
		idField:     c.IDField,
		pane:        c.Pane,
		clear:       c.Clear,
		owner: func(panePID int, defaultShell string) (int, int64) {
			return relaunchOwner(os.Getpid(), panePID, snapshot.ProcInfo, func(comm string) bool {
				return isShellComm(comm, defaultShell, os.Getenv("SHELL"))
			})
		},
	})
}

// runRelaunchStamp reads a hook JSON payload from r, derives the resume command,
// and stamps @remux_relaunch on opts.pane, preceded by @remux_relaunch_owner
// binding it to its owner (empty when the owner can't be resolved). The owner
// is written first so a capture between the two writes sees a record bound to
// a different stamp, which counts as owner-less rather than stale. It no-ops
// (returns nil) with no pane, or (stamp mode) when no session id is present —
// it must never fail a start hook. Only a wiring bug (unknown --agent and no
// --cmd) returns an error. The SetPaneOption error is intentionally swallowed:
// the pane may have vanished between the hook firing and this call
// (SetPaneOption already uses -q).
func runRelaunchStamp(ctx context.Context, setter paneOptionSetter, r io.Reader, opts relaunchStampOpts) error {
	if opts.pane == "" {
		return nil
	}
	if opts.clear {
		_ = setter.SetPaneOption(ctx, opts.pane, relaunchOwnerOption, "")
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
	if !sessionIDPattern.MatchString(id) {
		// Empty or shell-unsafe id: never stamp. Restore exec's this value
		// verbatim, so a malformed id is dropped rather than quoted.
		return nil
	}
	value := strings.ReplaceAll(tmpl, "{id}", id)
	_ = setter.SetPaneOption(ctx, opts.pane, relaunchOwnerOption, relaunchOwnerValue(ctx, setter, opts, value))
	_ = setter.SetPaneOption(ctx, opts.pane, relaunchOption, value)
	return nil
}

// relaunchOwnerValue returns the @remux_relaunch_owner record for stamp, or ""
// when no owner resolves.
func relaunchOwnerValue(ctx context.Context, setter paneOptionSetter, opts relaunchStampOpts, stamp string) string {
	if opts.owner == nil {
		return ""
	}
	panePID, defaultShell, err := setter.PaneProcess(ctx, opts.pane)
	if err != nil {
		return ""
	}
	pid, start := opts.owner(panePID, defaultShell)
	if pid == 0 {
		return ""
	}
	return snapshot.FormatRelaunchOwner(pid, start, stamp)
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
