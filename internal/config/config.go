// Package config provides the runtime config struct and defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RestoreMode controls how tmux-remux behaves when a tmux server starts.
// See [RestoreAuto], [RestoreInteractive], [RestoreOff].
type RestoreMode string

// Restore mode constants.
const (
	RestoreAuto        RestoreMode = "auto"
	RestoreInteractive RestoreMode = "interactive"
	RestoreOff         RestoreMode = "off"
)

// Config holds runtime settings for tmux-remux. Construct via [Default].
type Config struct {
	// Storage paths
	DBPath        string
	ScrollbackDir string
	LockPath      string
	LogPath       string

	// Save behavior
	MinSaveInterval      time.Duration
	SnapshotHistoryLimit int
	CloseEventLimit      int
	CaptureScrollback    bool

	// Restore behavior
	RestoreMode            RestoreMode
	RestoreMaxSessionAge   time.Duration
	RestoreMaxSnapshotAge  time.Duration
	RestoreSkipIdleShells  bool
	RestoreSkipIdleWindows bool
	SkipRunningSessions    bool

	// Allow-list of commands to relaunch on restore
	CommandAllowList []string

	// DecorationOptions is the allow-list of tmux window options snapshotted
	// and re-applied verbatim on restore. Restores persona decoration (agent
	// codename, tint) that a fan-out orchestrator stamped but that nothing
	// re-derives after a server restart.
	DecorationOptions []string
}

// Default returns a Config with XDG-resolved paths and sensible thresholds.
func Default() Config {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/tmp"
	}
	root := filepath.Join(dataHome, "tmux-remux")
	rt := filepath.Join(runtimeDir, "tmux-remux")

	return Config{
		DBPath:        filepath.Join(root, "state.db"),
		ScrollbackDir: filepath.Join(root, "scrollbacks"),
		LockPath:      filepath.Join(rt, "write.lock"),
		LogPath:       filepath.Join(root, "state.log"),

		MinSaveInterval:      30 * time.Second,
		SnapshotHistoryLimit: 20,
		CloseEventLimit:      50,
		CaptureScrollback:    true,

		RestoreMode:            RestoreAuto,
		RestoreMaxSessionAge:   14 * 24 * time.Hour,
		RestoreMaxSnapshotAge:  30 * 24 * time.Hour,
		RestoreSkipIdleShells:  true,
		RestoreSkipIdleWindows: true,
		SkipRunningSessions:    true,

		CommandAllowList: []string{
			"nvim", "vim", "vi",
			"htop", "btop", "top",
			"less", "more", "tail", "head", "watch",
			"lazygit", "lazydocker",
			"k9s", "kubectl",
			"ssh", "mosh",
		},

		DecorationOptions: []string{"@crew_name", "@crew_color"},
	}
}

// legacyDataDir is the pre-rename storage directory name. On the first run after
// the tmux-state → tmux-remux rename, EnsureDirs relocates it so existing
// snapshots and scrollbacks survive the rename.
const legacyDataDir = "tmux-state"

// EnsureDirs creates the directories required for the config's paths, first
// migrating a legacy tmux-state data directory into place if one exists.
func (c Config) EnsureDirs() error {
	if err := c.migrateLegacyDataDir(); err != nil {
		return err
	}
	for _, d := range []string{
		filepath.Dir(c.DBPath),
		c.ScrollbackDir,
		filepath.Dir(c.LockPath),
		filepath.Dir(c.LogPath),
	} {
		// 0o700: session names, cwds, and command lines live in state.db, which
		// sqlite creates group-readable (0644); a private dir is what keeps them
		// off other local group members.
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("mkdir %q: %w", d, err)
		}
	}
	return nil
}

// migrateLegacyDataDir renames a leftover tmux-state data directory to the
// tmux-remux path. It is a no-op once the new directory exists (already
// migrated or a fresh install) or when no legacy directory is present. The
// runtime lock directory is ephemeral and needs no migration.
func (c Config) migrateLegacyDataDir() error {
	dataRoot := filepath.Dir(c.DBPath)
	legacyRoot := filepath.Join(filepath.Dir(dataRoot), legacyDataDir)
	if legacyRoot == dataRoot {
		return nil
	}
	if _, err := os.Stat(dataRoot); err == nil {
		return nil
	}
	if _, err := os.Stat(legacyRoot); err != nil {
		return nil
	}
	if err := os.Rename(legacyRoot, dataRoot); err != nil {
		return fmt.Errorf("migrate legacy data dir %q → %q: %w", legacyRoot, dataRoot, err)
	}
	return nil
}
