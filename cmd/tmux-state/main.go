// Package main is the tmux-state CLI entry point.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
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
const Version = "0.3.0"

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
		newCatScrollbackCmd(),
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

				opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan := restore.BuildPlan(m, f, running, opts)
				return restore.Apply(ctx, t, plan)
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
				ev := evs[0]
				closeMan, err := closeevent.ParseManifest(ev.ManifestJSON)
				if err != nil {
					return fmt.Errorf("parse close event: %w", err)
				}
				snap, err := db.LatestSnapshotBefore(ctx, ev.Ts)
				if err != nil {
					return fmt.Errorf("find pre-close snapshot: %w", err)
				}
				if snap == nil {
					return fmt.Errorf("no pre-close snapshot — nothing to undo against")
				}
				var prior snapshot.Manifest
				if err := json.Unmarshal([]byte(snap.ManifestJSON), &prior); err != nil {
					return fmt.Errorf("parse pre-close snapshot: %w", err)
				}
				item := closeevent.FindClosed(prior, closeMan, ev.Kind)
				if item == nil {
					return fmt.Errorf("could not identify closed entity in pre-close snapshot")
				}
				m := item.SubManifest(prior.Host, prior.SavedAt)
				t := tmux.NewClient("tmux")
				opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan := restore.BuildPlan(m, filter.Filter{}, nil, opts)
				if err := restore.Apply(ctx, t, plan); err != nil {
					return err
				}
				focusRestored(ctx, t, m)
				_, err = db.DB().ExecContext(ctx, "DELETE FROM events WHERE id = ?", ev.ID)
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
		Short: "Open an interactive picker over events",
		RunE: func(*cobra.Command, []string) error {
			return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
				opts := store.ListOpts{Limit: 50}
				mode := picker.ModeSnapshot
				switch kind {
				case "snapshot":
					opts.Kinds = []string{"snapshot"}
				case "close":
					opts.ExcludeKinds = []string{"snapshot"}
					mode = picker.ModeClose
				}
				evs, err := db.ListEvents(ctx, opts)
				if err != nil {
					return err
				}

				t := tmux.NewClient("tmux")
				runningSet := map[string]bool{}
				if sessions, err := t.ListSessions(ctx); err == nil {
					for _, s := range sessions {
						runningSet[s.Name] = true
					}
				}

				sb := scrollback.New(cfg.ScrollbackDir)
				m := picker.NewPickerModel(mode, evs, runningSet, sb)
				if mode == picker.ModeClose {
					m.SetCloseContexts(buildCloseContexts(ctx, db, evs))
				}
				m.Bootstrap()

				prog := tea.NewProgram(m)
				finalModel, err := prog.Run()
				if err != nil {
					return fmt.Errorf("picker: %w", err)
				}
				final, ok := finalModel.(picker.PickerModel)
				if !ok || final.SelectedID() == 0 {
					return nil // cancelled
				}

				manifest := final.SelectedManifest()
				buildOpts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan := restore.BuildPlan(manifest, final.Filter(), nil, buildOpts)
				if err := restore.Apply(ctx, t, plan); err != nil {
					return err
				}
				if mode == picker.ModeClose {
					focusRestored(ctx, t, manifest)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "snapshot", "snapshot|close")
	return cmd
}

// buildCloseContexts resolves each close event against its parent snapshot
// (most recent snapshot < event.Ts) to derive a short label + sub-manifest of
// the lost entity. Best-effort: events without a recoverable parent get an
// empty context, which the picker renders as the bare Kind name.
func buildCloseContexts(ctx context.Context, db *store.Store, evs []store.Event) map[int64]picker.CloseContext {
	out := make(map[int64]picker.CloseContext, len(evs))
	priorCache := map[int64]snapshot.Manifest{}
	for _, ev := range evs {
		closeMan, err := closeevent.ParseManifest(ev.ManifestJSON)
		if err != nil {
			continue
		}
		prior, ok := priorCache[ev.Ts]
		if !ok {
			snap, err := db.LatestSnapshotBefore(ctx, ev.Ts)
			if err != nil || snap == nil {
				priorCache[ev.Ts] = snapshot.Manifest{}
				continue
			}
			if err := json.Unmarshal([]byte(snap.ManifestJSON), &prior); err != nil {
				priorCache[ev.Ts] = snapshot.Manifest{}
				continue
			}
			priorCache[ev.Ts] = prior
		}
		item := closeevent.FindClosed(prior, closeMan, ev.Kind)
		if item == nil {
			continue
		}
		out[ev.ID] = picker.CloseContext{
			Label:       item.Describe(),
			SubManifest: item.SubManifest(prior.Host, prior.SavedAt),
		}
	}
	return out
}

// focusRestored selects the first restored session/window so the user
// immediately lands on what they un-closed, instead of staying on whatever
// session was attached when they pressed Enter.
func focusRestored(ctx context.Context, t *tmux.Client, m snapshot.Manifest) {
	if len(m.Sessions) == 0 {
		return
	}
	s := m.Sessions[0]
	if len(s.Windows) == 0 {
		_, _ = t.Run(ctx, []string{"switch-client", "-t", s.Name})
		return
	}
	target := fmt.Sprintf("%s:%d", s.Name, s.Windows[0].Index)
	_, _ = t.Run(ctx, []string{"switch-client", "-t", target})
	_, _ = t.Run(ctx, []string{"select-window", "-t", target})
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
					t := time.UnixMilli(ev.Ts).Format("2006-01-02 15:04:05")
					fmt.Printf("%d\t%s  %-15s  %s\n", ev.ID, t, ev.Kind, ev.Reason)
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

// resolveBuildOptions builds the BuildOptions consumed by restore.BuildPlan.
// Errors are silently swallowed in favor of reasonable defaults: an
// empty Self disables scrollback rendering in emitted startup commands,
// and /bin/sh is the ultimate shell fallback. Resolved once per restore.
func resolveBuildOptions(ctx context.Context, t restore.Runner, allowList []string) restore.BuildOptions {
	self, err := os.Executable()
	if err != nil {
		self = ""
	}
	shell, isBash := restore.DefaultShell(ctx, t, os.Getenv("SHELL"))
	return restore.BuildOptions{
		Self:         self,
		DefaultShell: shell,
		IsBash:       isBash,
		AllowList:    allowList,
	}
}
