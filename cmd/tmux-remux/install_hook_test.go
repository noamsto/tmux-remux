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
