// Package main is the tmux-state CLI entry point.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-state/internal/config"
	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/restore"
	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
	"github.com/noamsto/tmux-state/internal/tmux"
)

// Version is the released version. Bumped on tagged releases.
const Version = "0.1.0"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tmux-state: error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tmux-state",
		Short:         "Fast, smart tmux state persistence",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newVersionCmd(),
		newSaveCmd(),
		newRestoreCmd(),
		newUndoCmd(),
		newPickCmd(),
		newCaptureEventCmd(),
		newIndexUpdateCmd(),
		newListCmd(),
		newPruneCmd(),
		newGCCmd(),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(*cobra.Command, []string) {
			fmt.Println(Version)
		},
	}
}

// newSaveCmd returns the save subcommand.
func newSaveCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a snapshot of the current tmux server",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := signalCtx()
			defer cancel()
			cfg := loadConfig()
			if err := cfg.EnsureDirs(); err != nil {
				return err
			}
			db, err := store.Open(ctx, cfg.DBPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			sb := scrollback.New(cfg.ScrollbackDir)
			t := tmux.NewClient("tmux")
			host, _ := os.Hostname()
			saver := snapshot.NewSaver(db, sb, t, snapshot.SaverOptions{
				Host:              host,
				CaptureScrollback: cfg.CaptureScrollback,
				MinSaveInterval:   cfg.MinSaveInterval,
			})
			if err := saver.Save(ctx, reason); err != nil {
				return err
			}
			return db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "manual", "reason for save (e.g. 'timer', 'hook:session-created')")
	return cmd
}

// newRestoreCmd returns the restore subcommand.
func newRestoreCmd() *cobra.Command {
	var auto bool
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore the latest snapshot through the smart filter",
		RunE: func(*cobra.Command, []string) error {
			ctx, cancel := signalCtx()
			defer cancel()
			cfg := loadConfig()
			if cfg.RestoreMode == config.RestoreOff && auto {
				return nil
			}
			db, err := store.Open(ctx, cfg.DBPath)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			ev, err := db.LatestSnapshot(ctx)
			if err != nil {
				return err
			}
			if ev == nil {
				return nil
			}

			var m snapshot.Manifest
			if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
				return err
			}

			f := filter.Filter{
				MaxSessionAge:      cfg.RestoreMaxSessionAge,
				MaxSnapshotAge:     cfg.RestoreMaxSnapshotAge,
				SkipIdleShells:     cfg.RestoreSkipIdleShells,
				SkipIdleWindows:    cfg.RestoreSkipIdleWindows,
				DedupRunningServer: cfg.DedupRunningServer,
			}
			if f.SkipSnapshot(ev.Ts) {
				return nil
			}

			t := tmux.NewClient("tmux")
			running := map[string]bool{}
			rows, _ := t.ListSessions(ctx)
			for _, s := range rows {
				running[s.Name] = true
			}

			plan := restore.BuildPlan(m, f, running, cfg.CommandAllowList)
			sb := scrollback.New(cfg.ScrollbackDir)
			return restore.ApplyWithScrollback(ctx, t, sb, plan)
		},
	}
	cmd.Flags().BoolVar(&auto, "auto", false, "respect restore_mode=off")
	return cmd
}

// newUndoCmd returns the undo subcommand. Wired in a later task.
func newUndoCmd() *cobra.Command {
	return &cobra.Command{Use: "undo", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newPickCmd returns the pick subcommand. Wired in a later task.
func newPickCmd() *cobra.Command {
	return &cobra.Command{Use: "pick", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newCaptureEventCmd returns the capture-event subcommand. Wired in a later task.
func newCaptureEventCmd() *cobra.Command {
	return &cobra.Command{Use: "capture-event", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newIndexUpdateCmd returns the index-update subcommand. Wired in a later task.
func newIndexUpdateCmd() *cobra.Command {
	return &cobra.Command{Use: "index-update", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newListCmd returns the list subcommand. Wired in a later task.
func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newPruneCmd returns the prune subcommand. Wired in a later task.
func newPruneCmd() *cobra.Command {
	return &cobra.Command{Use: "prune", RunE: func(*cobra.Command, []string) error { return nil }}
}

// newGCCmd returns the gc subcommand. Wired in a later task.
func newGCCmd() *cobra.Command {
	return &cobra.Command{Use: "gc", RunE: func(*cobra.Command, []string) error { return nil }}
}

func signalCtx() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func loadConfig() config.Config { return config.Default() }
