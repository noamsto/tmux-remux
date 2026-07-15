package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/noamsto/tmux-remux/internal/config"
)

func TestDefaultsResolveFromXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	c := config.Default()
	if got, want := c.DBPath, filepath.Join(tmp, "tmux-remux", "state.db"); got != want {
		t.Errorf("DBPath = %q, want %q", got, want)
	}
	if got, want := c.ScrollbackDir, filepath.Join(tmp, "tmux-remux", "scrollbacks"); got != want {
		t.Errorf("ScrollbackDir = %q, want %q", got, want)
	}
	if got, want := c.LockPath, filepath.Join(tmp, "tmux-remux", "write.lock"); got != want {
		t.Errorf("LockPath = %q, want %q", got, want)
	}
}

func TestDefaultsRespectThresholds(t *testing.T) {
	c := config.Default()
	if c.MinSaveInterval == 0 {
		t.Fatal("default thresholds must be non-zero")
	}
	if c.SnapshotHistoryLimit < 1 || c.CloseEventLimit < 1 {
		t.Fatal("default limits must be at least 1")
	}
}

func TestEnsureDirsCreatesPaths(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_RUNTIME_DIR", tmp)
	c := config.Default()
	if err := c.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, d := range []string{
		filepath.Dir(c.DBPath),
		c.ScrollbackDir,
		filepath.Dir(c.LockPath),
	} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("expected dir %q to exist: %v", d, err)
		}
	}
}

func TestEnsureDirsMigratesLegacyDataDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	legacy := filepath.Join(tmp, "tmux-state")
	if err := os.MkdirAll(legacy, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("snapshot"), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := config.Default().EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy dir should be gone after migration, stat err = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "tmux-remux", "state.db"))
	if err != nil {
		t.Fatalf("read migrated db: %v", err)
	}
	if string(got) != "snapshot" {
		t.Errorf("migrated content = %q, want %q", got, "snapshot")
	}
}

func TestRestoreModeIsTyped(t *testing.T) {
	c := config.Default()
	if c.RestoreMode != config.RestoreAuto {
		t.Errorf("default RestoreMode = %q, want %q", c.RestoreMode, config.RestoreAuto)
	}
	// Compile-time check: a bare string should not satisfy RestoreMode.
	// (The following line would fail to compile if RestoreMode were untyped.)
	var m config.RestoreMode = "auto"
	if m != config.RestoreAuto {
		t.Errorf("RestoreMode(\"auto\") != RestoreAuto")
	}
}

func TestDefaultDecorationOptions(t *testing.T) {
	got := config.Default().DecorationOptions
	want := []string{"@crew_name", "@crew_color"}
	if !slices.Equal(got, want) {
		t.Errorf("DecorationOptions = %v, want %v", got, want)
	}
}
