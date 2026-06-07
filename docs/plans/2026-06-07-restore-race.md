# Restore Race Fix + Retention Safety Net Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `restore --auto` immune to the save/restore race at tmux server start, keep pre-shutdown snapshots alive for a week, and make restore observable (log file + launch message).

**Architecture:** Restore anchors snapshot selection to the tmux server's start time (`LatestSnapshotBefore`), so no snapshot written by the current server can ever be selected. `PruneSnapshots` keeps the N newest plus the newest-per-local-day for 7 days. `BuildPlan` returns `PlanStats` consumed by a new `internal/applog` file logger and a `display-message` summary.

**Tech Stack:** Go 1.x, modernc SQLite (`internal/store`), cobra CLI, table tests with `go-cmp`. Spec: `docs/specs/2026-06-07-restore-race-design.md`.

**Verify commands:** `go build ./...` and `go test ./...` from the repo root (run inside `nix develop` if `go` is not on PATH).

---

### Task 1: `Client.ServerStartTime` (internal/tmux)

**Files:**
- Modify: `internal/tmux/client.go` (add method after `CapturePane`, ~line 145)
- Test: `internal/tmux/client_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tmux/client_test.go` (the `writeFakeTmux` helper already exists at the bottom of the file):

```go
// TestServerStartTimeParsesSecondsToMillis verifies #{start_time} (epoch
// seconds) is converted to the millisecond scale used by events.ts.
func TestServerStartTimeParsesSecondsToMillis(t *testing.T) {
	fake := writeFakeTmux(t, `echo 1780811351`)
	c := tmux.NewClient(fake)
	got, err := c.ServerStartTime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != 1780811351000 {
		t.Errorf("ServerStartTime = %d, want 1780811351000", got)
	}
}

func TestServerStartTimeRejectsGarbage(t *testing.T) {
	fake := writeFakeTmux(t, `echo not-a-number`)
	c := tmux.NewClient(fake)
	if _, err := c.ServerStartTime(context.Background()); err == nil {
		t.Error("expected parse error for non-numeric start_time")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tmux/ -run TestServerStartTime -v`
Expected: FAIL — `c.ServerStartTime undefined`

- [ ] **Step 3: Implement `ServerStartTime`**

In `internal/tmux/client.go`, add `"strconv"` to the imports and append after `CapturePane`:

```go
// ServerStartTime returns the running server's start time in Unix
// milliseconds, via the server-scoped #{start_time} format (epoch seconds).
// Restore uses it to anchor snapshot selection to "before this server
// existed", so snapshots written by the current server's own save hooks can
// never be selected (the save/restore race at server birth).
func (c *Client) ServerStartTime(ctx context.Context) (int64, error) {
	out, err := c.Run(ctx, []string{"display-message", "-p", "#{start_time}"})
	if err != nil {
		return 0, err
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse start_time %q: %w", strings.TrimSpace(out), err)
	}
	return secs * 1000, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tmux/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
git commit -m "feat(tmux): add ServerStartTime client query"
```

---

### Task 2: characterization test for `LatestSnapshotBefore` selection

`LatestSnapshotBefore` already exists (`internal/store/store.go:136`, strict `ts < ?`) but has no direct store-level test. The race fix rides entirely on its semantics, so pin them.

**Files:**
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write the test**

Append to `internal/store/store_test.go`:

```go
// TestLatestSnapshotBeforeIgnoresNewerSnapshots pins the semantics restore
// relies on: a snapshot written at/after the anchor (server start) is never
// selected, only the newest strictly-older one.
func TestLatestSnapshotBeforeIgnoresNewerSnapshots(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, ts := range []int64{100, 200} { // 100 = pre-boot, 200 = post-start save
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	ev, err := db.LatestSnapshotBefore(ctx, 150)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || ev.Ts != 100 {
		t.Fatalf("LatestSnapshotBefore(150) = %+v, want Ts=100", ev)
	}

	ev, err = db.LatestSnapshotBefore(ctx, 100) // strict <: equal ts excluded
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Errorf("LatestSnapshotBefore(100) = %+v, want nil", ev)
	}
}
```

- [ ] **Step 2: Run it — expected to pass (characterization, not new behavior)**

Run: `go test ./internal/store/ -run TestLatestSnapshotBefore -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/store/store_test.go
git commit -m "test(store): pin LatestSnapshotBefore strict-anchor semantics"
```

---

### Task 3: `filter.SessionSkipReason`

`PlanStats` needs to distinguish *why* a session was skipped; `SkipSession` only says *that*. Add a reason method; `SkipSession` becomes a wrapper (existing callers unchanged).

**Files:**
- Modify: `internal/filter/filter.go:38-50`
- Test: `internal/filter/filter_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/filter/filter_test.go` (match the package's existing test style; it tests `Filter` directly with `Now` injected):

```go
func TestSessionSkipReason(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	f := Filter{
		Now:                 now,
		MaxSessionAge:       time.Hour,
		SkipRunningSessions: true,
	}
	fresh := snapshot.Session{Name: "fresh", LastAttached: now.Unix() - 60}
	stale := snapshot.Session{Name: "stale", LastAttached: now.Unix() - 7200}
	running := map[string]bool{"fresh": true}

	if got := f.SessionSkipReason(fresh, running); got != "running" {
		t.Errorf("running session: reason = %q, want \"running\"", got)
	}
	if got := f.SessionSkipReason(stale, nil); got != "stale" {
		t.Errorf("stale session: reason = %q, want \"stale\"", got)
	}
	if got := f.SessionSkipReason(fresh, nil); got != "" {
		t.Errorf("kept session: reason = %q, want \"\"", got)
	}
}
```

Note: if `filter_test.go` is an external test package (`package filter_test`), prefix `Filter`/references with `filter.` to match the file's existing imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/filter/ -run TestSessionSkipReason -v`
Expected: FAIL — `SessionSkipReason undefined`

- [ ] **Step 3: Implement — replace `SkipSession` body**

In `internal/filter/filter.go`, replace the existing `SkipSession` (lines 36-50) with:

```go
// SessionSkipReason reports why the session would be filtered out:
// "running", "stale", or "" when it should be kept.
func (f Filter) SessionSkipReason(s snapshot.Session, running map[string]bool) string {
	if f.SkipRunningSessions && running[s.Name] {
		return "running"
	}
	if f.MaxSessionAge > 0 {
		la := time.Unix(s.LastAttached, 0)
		if f.now().Sub(la) > f.MaxSessionAge {
			return "stale"
		}
	}
	return ""
}

// SkipSession returns true if the session should be filtered out (already
// running or stale).
func (f Filter) SkipSession(s snapshot.Session, running map[string]bool) bool {
	return f.SessionSkipReason(s, running) != ""
}
```

- [ ] **Step 4: Run package tests**

Run: `go test ./internal/filter/ -v`
Expected: all PASS (existing `SkipSession` tests still green via the wrapper)

- [ ] **Step 5: Commit**

```bash
git add internal/filter/filter.go internal/filter/filter_test.go
git commit -m "feat(filter): expose session skip reason for restore stats"
```

---

### Task 4: `PlanStats` from `BuildPlan`

**Files:**
- Modify: `internal/restore/plan.go:88-158` (`BuildPlan`)
- Modify: `cmd/tmux-state/main.go:168` (restore), `:215` (undo), `:278` (pick) — callers
- Test: `internal/restore/plan_test.go` (new test + update every existing `BuildPlan` call to `plan, _ :=`)

- [ ] **Step 1: Write the failing test**

Append to `internal/restore/plan_test.go`:

```go
func TestBuildPlanStatsCountsKeptAndSkipped(t *testing.T) {
	m := snapshot.Manifest{
		Sessions: []snapshot.Session{
			{Name: "kept", Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/a", Command: "nvim", ChildCount: 1}},
			}}},
			{Name: "running", Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/b", Command: "nvim", ChildCount: 1}},
			}}},
			{Name: "idle", Windows: []snapshot.Window{{
				Index: 1, Layout: "L",
				Panes: []snapshot.Pane{{Index: 1, Cwd: "/c", Command: "fish", ChildCount: 0}},
			}}},
		},
	}
	f := filter.Filter{
		SkipIdleShells:      true,
		SkipIdleWindows:     true,
		SkipRunningSessions: true,
	}
	running := map[string]bool{"running": true}

	_, stats := restore.BuildPlan(m, f, running, defaultOpts)
	want := restore.PlanStats{
		SessionsKept:           1,
		SessionsSkippedRunning: 1,
		SessionsSkippedIdle:    1,
		WindowsSkippedIdle:     1,
	}
	if diff := cmp.Diff(want, stats); diff != "" {
		t.Errorf("stats mismatch (-want +got):\n%s", diff)
	}
}
```

Also update **every existing** `restore.BuildPlan(...)` call in `plan_test.go` from `plan := ...` to `plan, _ := ...` — exactly five call sites, at lines 32, 56, 81, 108, and 124.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/restore/ -run TestBuildPlanStats -v`
Expected: FAIL — compile error (BuildPlan returns 1 value, `PlanStats` undefined)

- [ ] **Step 3: Implement `PlanStats` in `plan.go`**

Add above `BuildPlan`:

```go
// PlanStats summarizes what BuildPlan kept and filtered, for restore logging
// and the post-restore display-message. "Idle" sessions are ones the smart
// filter dropped entirely because every window was idle plain shells.
type PlanStats struct {
	SessionsKept           int
	SessionsSkippedRunning int
	SessionsSkippedStale   int
	SessionsSkippedIdle    int
	WindowsSkippedIdle     int
}
```

Change the `BuildPlan` signature and loop (replacing the current session loop body — the `startupFor` closure and action construction stay identical):

```go
// BuildPlan builds an ordered slice of Actions to restore the manifest,
// honoring the filter and the allow-list of commands. The returned PlanStats
// reports what was kept vs filtered, per reason.
func BuildPlan(m snapshot.Manifest, f filter.Filter, runningSessions map[string]bool, opts BuildOptions) ([]Action, PlanStats) {
```

and inside:

```go
	var plan []Action
	var stats PlanStats
	for _, sess := range m.Sessions {
		switch f.SessionSkipReason(sess, runningSessions) {
		case "running":
			stats.SessionsSkippedRunning++
			continue
		case "stale":
			stats.SessionsSkippedStale++
			continue
		}
		var sessionStarted bool
		for _, win := range sess.Windows {
			if f.SkipWindow(win) {
				stats.WindowsSkippedIdle++
				continue
			}
			var firstPane *snapshot.Pane
			var keptPanes []snapshot.Pane
			for i := range win.Panes {
				p := win.Panes[i]
				if f.SkipPane(p) {
					continue
				}
				if firstPane == nil {
					firstPane = &p
				}
				keptPanes = append(keptPanes, p)
			}
			if firstPane == nil {
				stats.WindowsSkippedIdle++
				continue
			}
			if !sessionStarted {
				plan = append(plan, CreateSession{Name: sess.Name, Cwd: firstPane.Cwd})
				sessionStarted = true
			}
			plan = append(plan, CreateWindow{
				Session:        sess.Name,
				Index:          win.Index,
				Name:           win.Name,
				Cwd:            firstPane.Cwd,
				StartupCommand: startupFor(*firstPane),
			})
			for _, p := range keptPanes[1:] {
				plan = append(plan, SplitPane{
					Target:         fmt.Sprintf("%s:%d", sess.Name, win.Index),
					Cwd:            p.Cwd,
					StartupCommand: startupFor(p),
				})
			}
			plan = append(plan, SetLayout{
				Window: fmt.Sprintf("%s:%d", sess.Name, win.Index),
				Layout: win.Layout,
			})
		}
		if sessionStarted {
			stats.SessionsKept++
		} else {
			stats.SessionsSkippedIdle++
		}
	}
	return plan, stats
```

- [ ] **Step 4: Update the three callers in `cmd/tmux-state/main.go`**

- Restore cmd (~line 168): `plan, stats := restore.BuildPlan(m, f, running, opts)` — `stats` is used in Task 7; until then write `plan, _ :=` so the build stays green.
- Undo cmd (~line 215): `plan, _ := restore.BuildPlan(m, filter.Filter{}, nil, opts)`
- Pick cmd (~line 278): `plan, _ := restore.BuildPlan(manifest, final.Filter(), runningSet, buildOpts)`

- [ ] **Step 5: Build + run full test suite**

Run: `go build ./... && go test ./...`
Expected: PASS everywhere

- [ ] **Step 6: Commit**

```bash
git add internal/restore/plan.go internal/restore/plan_test.go cmd/tmux-state/main.go
git commit -m "feat(restore): BuildPlan returns PlanStats for observability"
```

---

### Task 5: day-thinned `PruneSnapshots`

**Files:**
- Modify: `internal/store/store.go:232-248` (`PruneSnapshots`)
- Modify: `cmd/tmux-state/main.go:117` (save cmd) and `~:437` (prune cmd) — callers
- Test: `internal/store/store_test.go` (new test + update `TestPruneSnapshotsKeepsNewest`)

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go` (add `"time"` to imports if absent):

```go
// TestPruneSnapshotsKeepsNewestPerDayWithinWeek verifies the retention
// safety net: besides the keep-N-newest window, the newest snapshot of each
// UTC day in the last 7 days survives — so a pre-shutdown snapshot is not
// evicted by a burst of fresh post-boot saves (the 2026-06-07 data loss).
// UTC-day grouping keeps the test deterministic in any host timezone.
func TestPruneSnapshotsKeepsNewestPerDayWithinWeek(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const day = int64(24 * time.Hour / time.Millisecond)
	now := int64(1780000000000) // fixed anchor
	insert := func(ts int64) {
		t.Helper()
		if _, err := db.InsertEvent(ctx, store.Event{
			Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Two snapshots on each of day-3 and day-2 (the later one per day must
	// survive), one on day-10 (outside the week — must be pruned), and a
	// burst of 4 fresh snapshots that fills the keep-N window.
	insert(now - 10*day)
	insert(now - 3*day)
	insert(now - 3*day + 3_600_000)
	insert(now - 2*day)
	insert(now - 2*day + 3_600_000)
	for i := int64(0); i < 4; i++ {
		insert(now - 3000 + i*1000)
	}

	if err := db.PruneSnapshots(ctx, 3, now); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListEvents(ctx, store.ListOpts{Kinds: []string{"snapshot"}, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, ev := range all { // ListEvents returns ts DESC
		got = append(got, ev.Ts)
	}
	want := []int64{
		now, now - 1000, now - 2000, // 3 newest
		now - 2*day + 3_600_000, // newest of day-2
		now - 3*day + 3_600_000, // newest of day-3
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("survivors mismatch (-want +got):\n%s", diff)
	}
}
```

Note: `store_test.go` may not import `go-cmp` yet — add `"github.com/google/go-cmp/cmp"` (already in go.mod) or compare with a plain loop matching the file's existing style.

Also update the existing `TestPruneSnapshotsKeepsNewest` call (line 204) to the new signature — its rows live at ts 1..10 (1970), far outside any 7-day window, so behavior is unchanged:

```go
	if err := db.PruneSnapshots(ctx, 3, time.Now().UnixMilli()); err != nil {
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestPruneSnapshots -v`
Expected: FAIL — compile error (PruneSnapshots takes 2 args)

- [ ] **Step 3: Implement day-thinning**

Replace `PruneSnapshots` in `internal/store/store.go`:

```go
// PruneSnapshots deletes snapshot events beyond the keep newest, except that
// the newest snapshot of each UTC calendar day in the 7 days before nowMs
// also survives. The per-day floor is the retention safety net: with 60s
// timer saves, keep-N alone evicts the pre-shutdown snapshot ~N minutes into
// a new server's life — exactly when a clobbered auto-restore most needs it.
func (s *Store) PruneSnapshots(ctx context.Context, keep int, nowMs int64) error {
	weekAgo := nowMs - 7*24*int64(time.Hour/time.Millisecond)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE kind = 'snapshot'
		  AND id NOT IN (
		      SELECT id FROM events
		      WHERE kind = 'snapshot'
		      ORDER BY ts DESC
		      LIMIT ?
		  )
		  AND id NOT IN (
		      SELECT id FROM events
		      WHERE kind = 'snapshot' AND ts >= ?
		        AND ts IN (
		            SELECT max(ts)
		            FROM events
		            WHERE kind = 'snapshot' AND ts >= ?
		            GROUP BY date(ts/1000, 'unixepoch')
		        )
		  )
	`, keep, weekAgo, weekAgo)
	if err != nil {
		return fmt.Errorf("prune snapshots: %w", err)
	}
	return nil
}
```

(The `ts IN (SELECT max(ts) ... GROUP BY day)` shape avoids SQLite's bare-column-with-max ambiguity; on a same-ts tie both rows survive, which is fine for a safety net.) Add `"time"` to store.go imports if absent.

- [ ] **Step 4: Update callers in `cmd/tmux-state/main.go`**

Save cmd line 117 and prune cmd (~line 437, plus the second `PruneSnapshots` reference if `newPruneCmd` has one):

```go
return db.PruneSnapshots(ctx, cfg.SnapshotHistoryLimit, time.Now().UnixMilli())
```

Add `"time"` to main.go imports if absent.

- [ ] **Step 5: Build + run full test suite**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go cmd/tmux-state/main.go
git commit -m "feat(store): day-thinned snapshot retention (7-day floor)"
```

---

### Task 6: `internal/applog` + `Config.LogPath`

**Files:**
- Create: `internal/applog/applog.go`
- Create: `internal/applog/applog_test.go`
- Modify: `internal/config/config.go` (add `LogPath` field + default)

- [ ] **Step 1: Write the failing tests**

Create `internal/applog/applog_test.go`:

```go
package applog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/noamsto/tmux-state/internal/applog"
)

func TestLogfAppendsTimestampedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	l, err := applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Logf("restore: %d sessions", 3)
	l.Logf("second line")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), data)
	}
	if !strings.HasSuffix(lines[0], "restore: 3 sessions") {
		t.Errorf("line 0 = %q, want suffix \"restore: 3 sessions\"", lines[0])
	}
	// RFC3339 timestamps start with the year.
	if !strings.HasPrefix(lines[0], "20") {
		t.Errorf("line 0 = %q, want RFC3339 timestamp prefix", lines[0])
	}
}

func TestOpenRotatesOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.log")
	big := strings.Repeat("x", 1<<20+1)
	if err := os.WriteFile(path, []byte(big), 0o640); err != nil {
		t.Fatal(err)
	}

	l, err := applog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Errorf("expected rotated file at %s.old: %v", path, err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 0 {
		t.Errorf("fresh log size = %d, want 0", st.Size())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/applog/ -v`
Expected: FAIL — package does not exist

- [ ] **Step 3: Implement the package**

Create `internal/applog/applog.go`:

```go
// Package applog provides the append-only operations log (state.log next to
// state.db). Restore decisions and command errors land here — run-shell -b
// discards stderr, so without a file the auto-restore path is a black box.
package applog

import (
	"fmt"
	"os"
	"time"
)

// maxSize is the rotation threshold: past it, Open moves the file aside to
// <path>.old and starts fresh. One generation of history is enough for
// diagnosing "what happened at the last server start".
const maxSize = 1 << 20 // 1 MB

// Logger appends timestamped lines to a single log file. Not safe for
// concurrent use within a process; cross-process appends are line-buffered
// single writes, which is sufficient for this log's diagnostic purpose.
type Logger struct {
	f *os.File
}

// Open opens (creating if needed) the log at path, rotating an oversized
// file to path+".old" first.
func Open(path string) (*Logger, error) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open log %q: %w", path, err)
	}
	return &Logger{f: f}, nil
}

// Logf appends one RFC3339-timestamped line. Write errors are dropped — the
// log must never break the operation it documents.
func (l *Logger) Logf(format string, a ...any) {
	_, _ = fmt.Fprintf(l.f, time.Now().Format(time.RFC3339)+" "+format+"\n", a...)
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	return l.f.Close()
}
```

- [ ] **Step 4: Add `LogPath` to config**

In `internal/config/config.go`: add to the `Config` struct's storage-paths block (after `LockPath`):

```go
	LogPath       string
```

and in `Default()` (after the `LockPath:` line):

```go
		LogPath:       filepath.Join(root, "state.log"),
```

`EnsureDirs` already creates `filepath.Dir(c.DBPath)`, which is the same directory — no change needed there.

- [ ] **Step 5: Run tests**

Run: `go test ./internal/applog/ ./internal/config/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/applog/ internal/config/config.go
git commit -m "feat(applog): size-capped operations log + config path"
```

---

### Task 7: wire restore — anchor + logging + launch message

**Files:**
- Modify: `cmd/tmux-state/main.go:125-175` (`newRestoreCmd`), `main.go:36-41` (`main`)

No new unit test: every moving part (start-time query, anchored selection, plan stats, logger) is unit-tested in Tasks 1-6; this task is glue. Verification is the build, the full suite, and Task 8's manual scenario.

- [ ] **Step 1: Rewrite `newRestoreCmd`**

Replace the `RunE` body (current `main.go:131-171`) with:

```go
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
				rows, _ := t.ListSessions(ctx)
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
```

Add `"github.com/noamsto/tmux-state/internal/applog"` and (if absent) `"time"` to main.go imports.

- [ ] **Step 2: Log top-level command errors**

Replace `main()` (lines 36-41) so errors lost under `run-shell -b` still land in the file:

```go
func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tmux-state: error:", err)
		if log, lerr := applog.Open(config.Default().LogPath); lerr == nil {
			log.Logf("error: %v (args: %v)", err, os.Args[1:])
			_ = log.Close()
		}
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Build + full test suite + lint**

Run: `go build ./... && go test ./...`
Expected: PASS
Run: `golangci-lint run` (available in the dev shell; pre-commit runs it anyway)
Expected: clean

- [ ] **Step 4: Commit**

```bash
git add cmd/tmux-state/main.go
git commit -m "feat(restore): anchor to server start, log decisions, launch message"
```

---

### Task 8: end-to-end manual verification (scratch socket)

Reproduces the production failure shape: a DB whose *latest* snapshot is a fresh single-session server and whose *older* snapshot holds the real state. Pre-fix, restore picks the fresh one and no-ops; post-fix, it must anchor past it.

**Files:** none (manual scenario; uses a scratch socket + scratch `XDG_DATA_HOME`, no effect on the real server or DB)

- [ ] **Step 1: Build the binary**

```bash
go build -o /tmp/tmux-state-test ./cmd/tmux-state
```

- [ ] **Step 2: Seed a "pre-shutdown" snapshot on a scratch server**

```bash
export XDG_DATA_HOME=/tmp/ts-test-data
export XDG_RUNTIME_DIR=/tmp/ts-test-rt
SOCK=/tmp/ts-test-sock
tmux -S $SOCK -f /dev/null new-session -d -s work -x 80 -y 24 'nvim'
tmux -S $SOCK new-window -t work -n editor 'nvim'
TMUX=$SOCK,0,0 /tmp/tmux-state-test save --reason=seed
tmux -S $SOCK kill-server
```

- [ ] **Step 3: Start a "fresh boot" server and let a post-start save land first**

```bash
sleep 1   # ensure the new server's start_time is after the seed snapshot
tmux -S $SOCK -f /dev/null new-session -d -s fresh -x 80 -y 24
TMUX=$SOCK,0,0 /tmp/tmux-state-test save --reason=hook:session-created
```

The DB's latest snapshot is now the fresh single-session server — the exact race outcome from 2026-06-07.

- [ ] **Step 4: Run restore and verify it anchors past the fresh snapshot**

```bash
TMUX=$SOCK,0,0 /tmp/tmux-state-test restore --auto
tmux -S $SOCK list-sessions
cat /tmp/ts-test-data/tmux-state/state.log
```

Expected: `list-sessions` shows **both** `fresh` and `work` (with its `editor` window running nvim), and `state.log` has a `restore: snapshot N (age …): 1 sessions restored …` line naming the *seed* snapshot, not the session-created one.

- [ ] **Step 5: Clean up**

```bash
tmux -S $SOCK kill-server
gtrash put /tmp/ts-test-data /tmp/ts-test-rt /tmp/tmux-state-test
```

- [ ] **Step 6: Update README**

In `README.md`, find the restore/persistence section and adjust it to state: restore selects the newest snapshot **from before the current server started**; retention keeps the `snapshot_history_limit` newest plus the newest snapshot per day for 7 days; operational decisions are logged to `$XDG_DATA_HOME/tmux-state/state.log`. Match the README's existing tone and brevity.

```bash
git add README.md
git commit -m "docs: restore anchoring, day-thinned retention, state.log"
```

---

### Out of scope (per spec)

- Smart-filter default changes (idle fish sessions vanishing) — visible now via stats/log, separate discussion.
- Version bump + release + lazytmux pin update — release flow, after this lands.
