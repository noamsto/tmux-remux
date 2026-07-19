package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/noamsto/tmux-remux/internal/agenthook"
)

// InstallHookCmd wires an agent CLI's start hook to `relaunch-stamp` so its
// panes restore their prior session. Claude ships as a plugin instead; this
// covers the agents with no plugin path (Codex; Cursor added later).
type InstallHookCmd struct {
	Agent string `arg:"" help:"agent to wire (codex)"`
}

func (c InstallHookCmd) Run() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return runInstallHook(c.Agent, self, home, os.Stdout)
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
		_, _ = fmt.Fprintln(w, `
IMPORTANT: run `+"`codex`"+`, open /hooks, and choose "Trust all" once per machine.
The hook will not run until you do — the trust hash cannot be pre-seeded.`)
		return nil
	default:
		return fmt.Errorf("unknown agent %q (want: codex)", agent)
	}
}

func reportHook(w io.Writer, agent, path string, changed bool) {
	if changed {
		_, _ = fmt.Fprintf(w, "Wired %s resume-on-restore hook → %s\n", agent, path)
		return
	}
	_, _ = fmt.Fprintf(w, "%s resume-on-restore hook already present → %s (no change)\n", agent, path)
}
