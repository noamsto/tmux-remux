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

	"github.com/noamsto/tmux-state/internal/applog"
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
		if log, lerr := applog.Open(loadConfig().LogPath); lerr == nil {
			log.Logf("error: %v (args: %v)", err, os.Args[1:])
			_ = log.Close()
		}
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
				return db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit, time.Now().UnixMilli())
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
				log, err := applog.Open(cfg.LogPath)
				if err != nil {
					return err
				}
				defer func() { _ = log.Close() }()

				t := tmux.NewClient("tmux")
				startMs, err := t.ServerStartTime(ctx)
				if err != nil {
					log.Logf("restore: server start time: %v", err)
					return err
				}
				// Anchor selection to before this server existed: snapshots
				// written by the current server's own save hooks (the
				// session-created hook and the systemd timer both race this
				// command at server birth) can never be selected.
				ev, err := db.LatestSnapshotBefore(ctx, startMs)
				if err != nil {
					log.Logf("restore: %v", err)
					return err
				}
				if ev == nil {
					log.Logf("restore: no snapshot before server start — nothing to do")
					return nil
				}

				var m snapshot.Manifest
				if err := json.Unmarshal([]byte(ev.ManifestJSON), &m); err != nil {
					log.Logf("restore: parse snapshot %d: %v", ev.ID, err)
					return err
				}

				f := filter.Filter{
					MaxSessionAge:       cfg.RestoreMaxSessionAge,
					MaxSnapshotAge:      cfg.RestoreMaxSnapshotAge,
					SkipIdleShells:      cfg.RestoreSkipIdleShells,
					SkipIdleWindows:     cfg.RestoreSkipIdleWindows,
					SkipRunningSessions: cfg.SkipRunningSessions,
				}
				age := time.Since(time.UnixMilli(ev.Ts)).Round(time.Second)
				if f.SkipSnapshot(ev.Ts) {
					log.Logf("restore: snapshot %d (age %s) older than max-snapshot-age — skipped", ev.ID, age)
					return nil
				}

				running := map[string]bool{}
				rows, err := t.ListSessions(ctx)
				if err != nil {
					log.Logf("restore: list sessions: %v (skip-running disabled this pass)", err)
				}
				for _, s := range rows {
					running[s.Name] = true
				}

				opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan, stats := restore.BuildPlan(m, f, running, opts)
				if err := restore.Apply(ctx, t, plan); err != nil {
					log.Logf("restore: snapshot %d (age %s): apply failed: %v", ev.ID, age, err)
					return err
				}
				log.Logf("restore: snapshot %d (age %s): %d sessions restored, skipped %d running / %d stale / %d idle (%d idle windows), %d actions",
					ev.ID, age, stats.SessionsKept, stats.SessionsSkippedRunning,
					stats.SessionsSkippedStale, stats.SessionsSkippedIdle,
					stats.WindowsSkippedIdle, len(plan))
				// Launch feedback: make a filtered-to-nothing restore visible
				// at the moment it happens. Best-effort — at server birth
				// there may be no attached client to display to.
				if auto && (stats.SessionsKept > 0 || stats.SessionsSkippedIdle > 0) {
					_, _ = t.Run(ctx, []string{"display-message",
						fmt.Sprintf("tmux-state: restored %d sessions (%d filtered)",
							stats.SessionsKept, stats.SessionsSkippedIdle)})
				}
				return nil
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
				ev, m, ok, err := restorableClose(ctx, db)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("nothing to undo — no recoverable close event")
				}
				t := tmux.NewClient("tmux")
				opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
				plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, opts)
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

// undoScanLimit bounds how far back undo --pop scans for a recoverable close
// event. Generous enough to step past a run of unrecoverable heads, bounded so
// a corrupt history can't turn undo into a full-table scan.
const undoScanLimit = 50

// restorableClose finds the most recent close event that can actually be
// restored: its lost entity resolves against a pre-close snapshot AND yields a
// non-empty restore manifest. It steps past events that can't (transient
// entities born and gone within one snapshot gap, lone panes whose SubManifest
// is empty) so a single unrecoverable head no longer blocks undo. ok is false
// when nothing in the scan window is recoverable.
func restorableClose(ctx context.Context, db *store.Store) (store.Event, snapshot.Manifest, bool, error) {
	evs, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: undoScanLimit})
	if err != nil {
		return store.Event{}, snapshot.Manifest{}, false, err
	}
	for _, ev := range evs {
		closeMan, err := closeevent.ParseManifest(ev.ManifestJSON)
		if err != nil {
			continue
		}
		snap, err := db.LatestSnapshotBefore(ctx, ev.Ts)
		if err != nil || snap == nil {
			continue
		}
		var prior snapshot.Manifest
		if err := json.Unmarshal([]byte(snap.ManifestJSON), &prior); err != nil {
			continue
		}
		item := closeevent.FindClosed(prior, closeMan, ev.Kind)
		if item == nil {
			continue
		}
		m := item.SubManifest(prior.Host, prior.SavedAt)
		if len(m.Sessions) == 0 {
			continue
		}
		return ev, m, true, nil
	}
	return store.Event{}, snapshot.Manifest{}, false, nil
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
				plan, _ := restore.BuildPlan(manifest, final.Filter(), runningSet, buildOpts)
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
				// The closed entity is already gone when the hook fires, so a
				// live query yields the true post-close survivor set. Errors
				// (last session closed, server gone) leave the index empty —
				// which is also the truth: nothing survived.
				t := tmux.NewClient("tmux")
				var post closeevent.IndexPost
				post.Windows, _ = t.ListWindows(ctx)
				post.Panes, _ = t.ListPanes(ctx)
				_, err := closeevent.Capture(ctx, db, closeevent.Args{
					Kind:      args[0],
					SessionID: session,
					WindowID:  window,
					PaneID:    pane,
					Host:      hostname(),
					Index:     post,
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
				if err := db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit, time.Now().UnixMilli()); err != nil {
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
