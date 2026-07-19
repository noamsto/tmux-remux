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
