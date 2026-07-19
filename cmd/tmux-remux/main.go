// Package main is the tmux-remux CLI entry point.
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
	"github.com/alecthomas/kong"

	"github.com/noamsto/tmux-remux/internal/applog"
	"github.com/noamsto/tmux-remux/internal/closeevent"
	"github.com/noamsto/tmux-remux/internal/config"
	"github.com/noamsto/tmux-remux/internal/filter"
	"github.com/noamsto/tmux-remux/internal/lockfile"
	"github.com/noamsto/tmux-remux/internal/picker"
	"github.com/noamsto/tmux-remux/internal/restore"
	"github.com/noamsto/tmux-remux/internal/scrollback"
	"github.com/noamsto/tmux-remux/internal/snapshot"
	"github.com/noamsto/tmux-remux/internal/store"
	"github.com/noamsto/tmux-remux/internal/tmux"
)

// Version is the released version. Bumped on tagged releases.
const Version = "0.4.0"

var hostname = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	return h
})

// CLI is the full command grammar; kong parses os.Args into it. Field order is
// the order subcommands appear in --help.
type CLI struct {
	Version       VersionCmd       `cmd:"" help:"Print version"`
	Save          SaveCmd          `cmd:"" help:"Save a snapshot of the current tmux server"`
	Restore       RestoreCmd       `cmd:"" help:"Restore the latest snapshot through the smart filter"`
	Undo          UndoCmd          `cmd:"" help:"Restore the most recent close event"`
	Pick          PickCmd          `cmd:"" help:"Open an interactive picker over events"`
	CaptureEvent  CaptureEventCmd  `cmd:"" name:"capture-event" help:"Record a close event (called from tmux hooks)"`
	List          ListCmd          `cmd:"" help:"List events"`
	Prune         PruneCmd         `cmd:"" help:"Apply retention limits to events"`
	GC            GCCmd            `cmd:"" name:"gc" help:"Reap orphan scrollback files"`
	CatScrollback CatScrollbackCmd `cmd:"" name:"cat-scrollback" hidden:"" help:"Stream stored scrollback to stdout (internal helper)"`
	RelaunchStamp RelaunchStampCmd `cmd:"" name:"relaunch-stamp" hidden:"" help:"Stamp @remux_relaunch from an agent start hook (internal helper)"`
	InstallHook   InstallHookCmd   `cmd:"" name:"install-hook" help:"Wire an agent start hook for resume-on-restore"`
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("tmux-remux"),
		kong.Description("Fast, smart tmux state persistence"),
		kong.UsageOnError(),
	)
	if err := kctx.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tmux-remux: error:", err)
		if log, lerr := applog.Open(loadConfig().LogPath); lerr == nil {
			log.Logf("error: %v (args: %v)", err, os.Args[1:])
			_ = log.Close()
		}
		os.Exit(1)
	}
}

// withStore opens the DB after ensuring storage directories exist, takes an
// exclusive flock on cfg.LockPath to serialize writers, runs fn, and closes
// the DB. Used by every subcommand's Run.
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

// VersionCmd prints the version.
type VersionCmd struct{}

func (VersionCmd) Run() error {
	fmt.Println(Version)
	return nil
}

// SaveCmd saves a snapshot of the current tmux server.
type SaveCmd struct {
	Reason string `default:"manual" help:"reason for save (e.g. 'timer', 'hook:session-created')"`
}

func (c SaveCmd) Run() error {
	return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
		sb := scrollback.New(cfg.ScrollbackDir)
		t := tmux.NewClient("tmux", cfg.DecorationOptions...)
		saver := snapshot.NewSaver(db, sb, t, snapshot.SaverOptions{
			Host:              hostname(),
			CaptureScrollback: cfg.CaptureScrollback,
			MinSaveInterval:   cfg.MinSaveInterval,
		})
		if err := saver.Save(ctx, c.Reason); err != nil {
			return err
		}
		return db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit, time.Now().UnixMilli())
	})
}

// RestoreCmd restores the latest snapshot through the smart filter.
type RestoreCmd struct {
	Auto bool `help:"respect restore_mode=off"`
}

func (c RestoreCmd) Run() error {
	return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
		if cfg.RestoreMode == config.RestoreOff && c.Auto {
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
		if c.Auto && (stats.SessionsKept > 0 || stats.SessionsSkippedIdle > 0) {
			_, _ = t.Run(ctx, []string{"display-message",
				fmt.Sprintf("tmux-remux: restored %d sessions (%d filtered)",
					stats.SessionsKept, stats.SessionsSkippedIdle)})
		}
		return nil
	})
}

// UndoCmd restores the most recent close event.
type UndoCmd struct {
	Pop bool `help:"restore most recent close event and remove it from history"`
}

func (c UndoCmd) Run() error {
	if !c.Pop {
		return fmt.Errorf("only --pop is supported in v0.1.0")
	}
	return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
		ev, item, prior, ok, err := restorableClose(ctx, db)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("nothing to undo — no recoverable close event")
		}
		t := tmux.NewClient("tmux")
		opts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)
		plan, m := buildRestorePlan(ctx, t, item, prior, opts)
		if err := restore.Apply(ctx, t, plan); err != nil {
			return err
		}
		focusRestored(ctx, t, m)
		_, err = db.DB().ExecContext(ctx, "DELETE FROM events WHERE id = ?", ev.ID)
		return err
	})
}

// undoScanLimit bounds how far back undo --pop scans for a recoverable close
// event. Generous enough to step past a run of unrecoverable heads, bounded so
// a corrupt history can't turn undo into a full-table scan.
const undoScanLimit = 50

// restorableClose finds the most recent close event that can actually be
// restored: its lost entity resolves against a pre-close snapshot AND yields a
// non-empty restore manifest. It steps past events that can't (entities born
// and gone within one snapshot gap) so a single unrecoverable head no longer
// blocks undo. It returns the resolved ClosedItem and the prior snapshot so the
// caller can build the restore plan; ok is false when nothing in the scan
// window is recoverable.
func restorableClose(ctx context.Context, db *store.Store) (store.Event, *closeevent.ClosedItem, snapshot.Manifest, bool, error) {
	evs, err := db.ListEvents(ctx, store.ListOpts{ExcludeKinds: []string{"snapshot"}, Limit: undoScanLimit})
	if err != nil {
		return store.Event{}, nil, snapshot.Manifest{}, false, err
	}
	for _, ev := range evs {
		item, prior, ok := resolveEvent(ctx, db, ev)
		if !ok {
			continue
		}
		// Defense-in-depth: every item FindClosed returns now yields a
		// non-empty sub-manifest, but guard against a future resolver that
		// can't build a restore plan rather than popping an un-restorable head.
		if len(item.SubManifest(prior.Host, prior.SavedAt).Sessions) == 0 {
			continue
		}
		return ev, item, prior, true, nil
	}
	return store.Event{}, nil, snapshot.Manifest{}, false, nil
}

// resolveEvent resolves a close event to its lost entity against the most
// recent pre-close snapshot. ok is false when the event isn't a recoverable
// close: unparsable, no prior snapshot, or the entity was never captured.
func resolveEvent(ctx context.Context, db *store.Store, ev store.Event) (*closeevent.ClosedItem, snapshot.Manifest, bool) {
	closeMan, err := closeevent.ParseManifest(ev.ManifestJSON)
	if err != nil {
		return nil, snapshot.Manifest{}, false
	}
	snap, err := db.LatestSnapshotBefore(ctx, ev.Ts)
	if err != nil || snap == nil {
		return nil, snapshot.Manifest{}, false
	}
	var prior snapshot.Manifest
	if err := json.Unmarshal([]byte(snap.ManifestJSON), &prior); err != nil {
		return nil, snapshot.Manifest{}, false
	}
	item := closeevent.FindClosed(prior, closeMan, ev.Kind)
	if item == nil {
		return nil, snapshot.Manifest{}, false
	}
	return item, prior, true
}

// buildRestorePlan turns a resolved close into a restore plan. A lost pane is
// split back into its live parent window (or the window is recreated if it's
// gone); a window or session is rebuilt via BuildPlan. Returns the plan and the
// sub-manifest used for post-restore focus.
func buildRestorePlan(ctx context.Context, t *tmux.Client, item *closeevent.ClosedItem, prior snapshot.Manifest, opts restore.BuildOptions) ([]restore.Action, snapshot.Manifest) {
	m := item.SubManifest(prior.Host, prior.SavedAt)
	if item.Pane != nil {
		return restore.BuildPaneRestore(*item.Pane, *item.Window, item.SessionName, windowLive(ctx, t, item.Window.ID), opts), m
	}
	if live, err := t.ListWindows(ctx); err == nil {
		reindexIntoLiveSessions(&m, live)
	}
	plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, opts)
	return plan, m
}

// reindexIntoLiveSessions reassigns window indices in m that are already
// occupied in the live server. Restoring a single window into a session that's
// still alive (the common undo / close-pick case) otherwise pins the window's
// stored index — almost always taken, since closing a window renumbers the
// rest — and tmux fails new-window with "index in use", silently dropping the
// restore. Colliding windows move to a free slot past the session's live max;
// windows whose session isn't live are left alone, since CreateSession rebuilds
// those from scratch with their indices free.
func reindexIntoLiveSessions(m *snapshot.Manifest, live []tmux.WindowRow) {
	used := map[string]map[int]bool{}
	for _, w := range live {
		if used[w.Session] == nil {
			used[w.Session] = map[int]bool{}
		}
		used[w.Session][w.Index] = true
	}
	for si := range m.Sessions {
		occ := used[m.Sessions[si].Name]
		if occ == nil {
			continue
		}
		for wi := range m.Sessions[si].Windows {
			idx := m.Sessions[si].Windows[wi].Index
			if !occ[idx] {
				occ[idx] = true
				continue
			}
			next := 0
			for k := range occ {
				if k > next {
					next = k
				}
			}
			next++
			m.Sessions[si].Windows[wi].Index = next
			occ[next] = true
		}
	}
}

// eventByID returns the event with the given id from evs, or a zero Event
// (which resolveEvent rejects) when absent.
func eventByID(evs []store.Event, id int64) store.Event {
	for _, ev := range evs {
		if ev.ID == id {
			return ev
		}
	}
	return store.Event{}
}

// windowLive reports whether a window with the given id is currently open.
func windowLive(ctx context.Context, t *tmux.Client, windowID string) bool {
	windows, err := t.ListWindows(ctx)
	if err != nil {
		return false
	}
	for _, w := range windows {
		if w.ID == windowID {
			return true
		}
	}
	return false
}

// PickCmd opens an interactive picker over events.
type PickCmd struct {
	Kind string `default:"snapshot" enum:"snapshot,close" help:"snapshot|close"`
}

func (c PickCmd) Run() error {
	return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
		opts := store.ListOpts{Limit: 50}
		mode := picker.ModeSnapshot
		switch c.Kind {
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

		buildOpts := resolveBuildOptions(ctx, t, cfg.CommandAllowList)

		// Close mode restores one lost entity (the same split-or-recreate
		// path as undo); snapshot mode replays a whole snapshot.
		if mode == picker.ModeClose {
			item, prior, ok := resolveEvent(ctx, db, eventByID(evs, final.SelectedID()))
			if !ok {
				return nil
			}
			plan, m := buildRestorePlan(ctx, t, item, prior, buildOpts)
			if err := restore.Apply(ctx, t, plan); err != nil {
				return err
			}
			focusRestored(ctx, t, m)
			return nil
		}

		manifest := final.SelectedManifest()
		plan, _ := restore.BuildPlan(manifest, final.Filter(), runningSet, buildOpts)
		return restore.Apply(ctx, t, plan)
	})
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

// CaptureEventCmd records a close event (called from tmux hooks).
type CaptureEventCmd struct {
	Kind    string `arg:"" help:"event kind"`
	Session string `help:"tmux session id ($N)"`
	Window  string `help:"tmux window id (@N)"`
	Pane    string `help:"tmux pane id (%N)"`
}

func (c CaptureEventCmd) Run() error {
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
			Kind:      c.Kind,
			SessionID: c.Session,
			WindowID:  c.Window,
			PaneID:    c.Pane,
			Host:      hostname(),
			Index:     post,
		})
		return err
	})
}

// ListCmd lists events.
type ListCmd struct {
	JSON bool `name:"json" help:"emit one JSON object per line (newline-delimited)"`
}

func (c ListCmd) Run() error {
	return withStore(func(ctx context.Context, _ config.Config, db *store.Store) error {
		evs, err := db.ListEvents(ctx, store.ListOpts{Limit: 100})
		if err != nil {
			return err
		}
		if c.JSON {
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
}

// PruneCmd applies retention limits to events.
type PruneCmd struct{}

func (PruneCmd) Run() error {
	return withStore(func(ctx context.Context, cfg config.Config, db *store.Store) error {
		if err := db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit, time.Now().UnixMilli()); err != nil {
			return err
		}
		return db.PruneCloseEvents(ctx, cfg.CloseEventLimit)
	})
}

// GCCmd reaps orphan scrollback files.
type GCCmd struct{}

func (GCCmd) Run() error {
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
