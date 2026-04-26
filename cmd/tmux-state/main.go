// Package main is the tmux-state CLI entry point.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"

	"github.com/spf13/cobra"

	"github.com/noamsto/tmux-state/internal/closeevent"
	"github.com/noamsto/tmux-state/internal/config"
	"github.com/noamsto/tmux-state/internal/filter"
	"github.com/noamsto/tmux-state/internal/lockfile"
	"github.com/noamsto/tmux-state/internal/picker"
	"github.com/noamsto/tmux-state/internal/restore"
	"github.com/noamsto/tmux-state/internal/scrollback"
	"github.com/noamsto/tmux-state/internal/snapshot"
	"github.com/noamsto/tmux-state/internal/store"
	"github.com/noamsto/tmux-state/internal/tmux"
)

// Version is the released version. Bumped on tagged releases.
const Version = "0.1.0"

var hostname = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	return h
})

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

// withStore opens the DB after ensuring storage directories exist, takes an
// exclusive flock on cfg.LockPath to serialize writers, runs fn, and closes
// the DB. Used by every subcommand RunE.
func withStore(fn func(ctx context.Context, cfg config.Config, db *store.Store) error) error {
	ctx, cancel := signalCtx()
	defer cancel()
	cfg := loadConfig()
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	lock, err := lockfile.Acquire(cfg.LockPath)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	db, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	return fn(ctx, cfg, db)
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
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				sb := scrollback.New(cfg.ScrollbackDir)
				t := tmux.NewClient("tmux")
				saver := snapshot.NewSaver(db, sb, t, snapshot.SaverOptions{
					Host:              hostname(),
					CaptureScrollback: cfg.CaptureScrollback,
					MinSaveInterval:   cfg.MinSaveInterval,
				})
				if err := saver.Save(ctx, reason); err != nil {
					return err
				}
				return db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit)
			})
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
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				if cfg.RestoreMode == config.RestoreOff && auto {
					return nil
				}
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
			})
		},
	}
	cmd.Flags().BoolVar(&auto, "auto", false, "respect restore_mode=off")
	return cmd
}

// newUndoCmd returns the undo subcommand.
func newUndoCmd() *cobra.Command {
	var pop bool
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Restore the most recent close event",
		RunE: func(*cobra.Command, []string) error {
			if !pop {
				return fmt.Errorf("only --pop is supported in v0.1.0")
			}
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				evs, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: 1})
				if err != nil || len(evs) == 0 {
					return err
				}
				var wrapped struct {
					Index json.RawMessage `json:"index"`
				}
				if err := json.Unmarshal([]byte(evs[0].ManifestJSON), &wrapped); err != nil {
					return err
				}
				var m snapshot.Manifest
				if len(wrapped.Index) > 0 {
					_ = json.Unmarshal(wrapped.Index, &m)
				}
				t := tmux.NewClient("tmux")
				plan := restore.BuildPlan(m, filter.Filter{}, nil, cfg.CommandAllowList)
				sb := scrollback.New(cfg.ScrollbackDir)
				if err := restore.ApplyWithScrollback(ctx, t, sb, plan); err != nil {
					return err
				}
				_, err = db.DB().ExecContext(ctx, "DELETE FROM events WHERE id = ?", evs[0].ID)
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&pop, "pop", false, "restore most recent close event and remove it from history")
	return cmd
}

// newPickCmd returns the pick subcommand.
func newPickCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "pick",
		Short: "Open an fzf picker over events",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				opts := store.ListOpts{Limit: 50}
				switch kind {
				case "snapshot":
					opts.Kinds = []string{"snapshot"}
				case "close":
					opts.ExcludeKinds = []string{"snapshot"}
				}
				evs, err := db.ListEvents(ctx, opts)
				if err != nil {
					return err
				}
				items := make([]picker.Item, 0, len(evs))
				for _, ev := range evs {
					items = append(items, picker.Item{Key: fmt.Sprint(ev.ID), Display: picker.FormatRow(ev)})
				}
				selected, err := picker.Pick(ctx, "fzf", items)
				if err != nil || selected == "" {
					return err
				}

				id, _ := strconv.ParseInt(selected, 10, 64)
				var ev store.Event
				row := db.DB().QueryRowContext(ctx, `SELECT id, ts, kind, scope, reason, host, parent_event_id, manifest_json FROM events WHERE id=?`, id)
				if err := row.Scan(&ev.ID, &ev.Ts, &ev.Kind, &ev.Scope, &ev.Reason, &ev.Host, &ev.ParentEventID, &ev.ManifestJSON); err != nil {
					return err
				}

				var m snapshot.Manifest
				if ev.Kind == "snapshot" {
					if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
						return err
					}
				} else {
					var wrapped struct {
						Index json.RawMessage `json:"index"`
					}
					_ = json.Unmarshal([]byte(ev.ManifestJSON), &wrapped)
					if len(wrapped.Index) > 0 {
						_ = json.Unmarshal(wrapped.Index, &m)
					}
				}
				t := tmux.NewClient("tmux")
				plan := restore.BuildPlan(m, filter.Filter{}, nil, cfg.CommandAllowList)
				sb := scrollback.New(cfg.ScrollbackDir)
				return restore.ApplyWithScrollback(ctx, t, sb, plan)
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "snapshot", "snapshot|close")
	return cmd
}

// newCaptureEventCmd returns the capture-event subcommand.
func newCaptureEventCmd() *cobra.Command {
	var session, window, pane string
	cmd := &cobra.Command{
		Use:   "capture-event KIND",
		Short: "Record a close event (called from tmux hooks)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return withStore(func(ctx context.Context, _ config.Config, db *store.Store) error {
				_, err := closeevent.Capture(ctx, db, closeevent.Args{
					Kind:      args[0],
					SessionID: session,
					WindowID:  window,
					PaneID:    pane,
					Host:      hostname(),
				})
				return err
			})
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "tmux session id ($N)")
	cmd.Flags().StringVar(&window, "window", "", "tmux window id (@N)")
	cmd.Flags().StringVar(&pane, "pane", "", "tmux pane id (%N)")
	return cmd
}

// newIndexUpdateCmd returns the index-update subcommand.
func newIndexUpdateCmd() *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "index-update",
		Short: "Update the live index for a session (called from structure-change hooks)",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, _ config.Config, db *store.Store) error {
				t := tmux.NewClient("tmux")
				ws, _ := t.ListWindows(ctx)
				ps, _ := t.ListPanes(ctx)
				payload := struct {
					Windows []tmux.WindowRow `json:"windows"`
					Panes   []tmux.PaneRow   `json:"panes"`
				}{ws, ps}
				data, _ := json.Marshal(payload)
				return closeevent.UpsertIndex(ctx, db.DB(), sessionID, string(data))
			})
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "tmux session id ($N)")
	return cmd
}

// newListCmd returns the list subcommand.
func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List events",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, _ config.Config, db *store.Store) error {
				evs, err := db.ListEvents(ctx, store.ListOpts{Limit: 100})
				if err != nil {
					return err
				}
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					for _, ev := range evs {
						if err := enc.Encode(ev); err != nil {
							return err
						}
					}
					return nil
				}
				for _, ev := range evs {
					fmt.Println(picker.FormatRow(ev))
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit one JSON object per line (newline-delimited)")
	return cmd
}

// newPruneCmd returns the prune subcommand.
func newPruneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prune",
		Short: "Apply retention limits to events",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				if err := db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit); err != nil {
					return err
				}
				return db.PruneCloseEvents(ctx, cfg.CloseEventLimit)
			})
		},
	}
}

// newGCCmd returns the gc subcommand.
func newGCCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Reap orphan scrollback files",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				sb := scrollback.New(cfg.ScrollbackDir)
				orphans, err := db.ScrollbacksWithZeroRef(ctx)
				if err != nil {
					return err
				}
				for _, sha := range orphans {
					if err := sb.Delete(ctx, sha); err != nil {
						continue
					}
					_ = db.DeleteScrollback(ctx, sha)
				}
				return nil
			})
		},
	}
}

func signalCtx() (context.Context, func()) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func loadConfig() config.Config { return config.Default() }
