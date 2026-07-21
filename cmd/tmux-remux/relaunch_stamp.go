package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/noamsto/tmux-remux/internal/tmux"
)

const relaunchOption = "@remux_relaunch"

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
}

type relaunchStampOpts struct {
	agent       string
	cmdTemplate string
	idField     string
	pane        string
	clear       bool
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
	})
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
	if !sessionIDPattern.MatchString(id) {
		// Empty or shell-unsafe id: never stamp. Restore exec's this value
		// verbatim, so a malformed id is dropped rather than quoted.
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
