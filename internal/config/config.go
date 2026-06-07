// Package config provides the runtime config struct and defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RestoreMode controls how tmux-state behaves when a tmux server starts.
// See [RestoreAuto], [RestoreInteractive], [RestoreOff].
type RestoreMode string

// Restore mode constants.
const (
	RestoreAuto        RestoreMode = "auto"
	RestoreInteractive RestoreMode = "interactive"
	RestoreOff         RestoreMode = "off"
)

// Config holds runtime settings for tmux-state. Construct via [Default].
type Config struct {
	// Storage paths
	DBPath        string
	ScrollbackDir string
	LockPath      string

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
	root := filepath.Join(dataHome, "tmux-state")
	rt := filepath.Join(runtimeDir, "tmux-state")

	return Config{
		DBPath:        filepath.Join(root, "state.db"),
		ScrollbackDir: filepath.Join(root, "scrollbacks"),
		LockPath:      filepath.Join(rt, "write.lock"),

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
	}
}

// EnsureDirs creates the directories required for the config's paths.
func (c Config) EnsureDirs() error {
	for _, d := range []string{
		filepath.Dir(c.DBPath),
		c.ScrollbackDir,
		filepath.Dir(c.LockPath),
	} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("mkdir %q: %w", d, err)
		}
	}
	return nil
}
