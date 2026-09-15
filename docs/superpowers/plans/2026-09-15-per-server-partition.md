# Per-server state partition — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Partition every event, throttle key and prune in `state.db` by tmux socket path, so two tmux servers sharing one database cannot overwrite each other's snapshots, scrollback captures or history.

**Architecture:** `store.Store` binds a `serverKey` (the tmux socket path) at `Open` time and every statement filters on it internally, so no call site can forget. A migration adds `events.server_key` and a `server_state` table, and drops all pre-migration rows — they carry no socket and cannot be attributed. Cross-lane operations that gc needs get explicitly global names.

**Tech Stack:** Go, `modernc.org/sqlite`, SQLite migrations driven by `PRAGMA user_version`, standard library testing.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-09-15-per-server-partition-design.md`. Issue: [#130](https://github.com/noamsto/tmux-remux/issues/130).
- Branch: `fix/130-partition-state-by-tmux-server`. Worktree: `~/Data/git/.worktrees/noamsto/tmux-remux/fix-130-partition-state-by-tmux-server`.
- All `go` and `git commit` commands run under `nix develop -c` — the pre-commit hooks cannot find the Go toolchain otherwise.
- Server key is the raw socket path, never hashed, never a basename.
- The scrollback tables (`scrollbacks`, `event_scrollbacks`) stay unpartitioned. Blobs are content-addressed and shared by refcount.
- Migrations are numbered `NNNN_name.sql` in `internal/store/migrations/` and must be exactly `current + 1`, or `migrate` errors with "migration gap".
- Comments explain a non-obvious WHY only. No comment restates what the code does.

---

### Task 1: `tmux.SocketPath`

The default-socket rule is currently inlined in `withSynthesizedTmuxEnv`. Nothing else can reach it, and Task 6 needs it. Extract it, leaving one definition.

**Files:**
- Modify: `internal/tmux/client.go:75-93`
- Test: `internal/tmux/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func SocketPath(env []string) string` in package `tmux`.

- [x] **Step 1: Write the failing test**

Append to `internal/tmux/client_test.go`:

```go
func TestSocketPath(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want string
	}{
		{
			name: "TMUX set wins over everything",
			env:  []string{"TMUX_TMPDIR=/ignored", "TMUX=/run/user/1000/tmux-1000/default,660951,87"},
			want: "/run/user/1000/tmux-1000/default",
		},
		{
			name: "TMUX without the pid,session suffix",
			env:  []string{"TMUX=/run/user/1000/tmux-1000/norgb"},
			want: "/run/user/1000/tmux-1000/norgb",
		},
		{
			name: "TMUX unset falls back to TMUX_TMPDIR",
			env:  []string{"TMUX_TMPDIR=/run/user/1000"},
			want: fmt.Sprintf("/run/user/1000/tmux-%d/default", os.Getuid()),
		},
		{
			name: "neither set falls back to /tmp",
			env:  []string{"HOME=/home/x"},
			want: fmt.Sprintf("/tmp/tmux-%d/default", os.Getuid()),
		},
		{
			name: "empty TMUX is treated as unset",
			env:  []string{"TMUX=", "TMUX_TMPDIR=/run/user/1000"},
			want: fmt.Sprintf("/run/user/1000/tmux-%d/default", os.Getuid()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tmux.SocketPath(tt.env); got != tt.want {
				t.Errorf("SocketPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

Add `"fmt"` and `"os"` to that file's imports if absent.

- [x] **Step 2: Run the test to verify it fails**

```bash
cd ~/Data/git/.worktrees/noamsto/tmux-remux/fix-130-partition-state-by-tmux-server
nix develop -c go test ./internal/tmux/ -run TestSocketPath -v
```

Expected: FAIL, `undefined: tmux.SocketPath`.

- [x] **Step 3: Implement**

Replace `withSynthesizedTmuxEnv` in `internal/tmux/client.go` (currently lines 75-93) with:

```go
// SocketPath returns the tmux socket path implied by env: the first
// comma-separated field of TMUX when set — every hook-invoked process has it
// — otherwise tmux's own default-socket rule, $TMUX_TMPDIR/tmux-<uid>/default
// with /tmp as the TMUX_TMPDIR fallback.
func SocketPath(env []string) string {
	tmpdir := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "TMUX="); ok && v != "" {
			socket, _, _ := strings.Cut(v, ",")
			if socket != "" {
				return socket
			}
		}
		if v, ok := strings.CutPrefix(e, "TMUX_TMPDIR="); ok {
			tmpdir = v
		}
	}
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	return fmt.Sprintf("%s/tmux-%d/default", tmpdir, os.Getuid())
}

// withSynthesizedTmuxEnv returns env unchanged when TMUX is already set,
// otherwise appends a synthesized TMUX=<socket>,0,0 entry. The pid/session-id
// components are dummies — tmux only checks that TMUX is non-empty and that
// the socket path resolves to a running server.
func withSynthesizedTmuxEnv(env []string) []string {
	for _, e := range env {
		if strings.HasPrefix(e, "TMUX=") {
			return env
		}
	}
	return append(env, fmt.Sprintf("TMUX=%s,0,0", SocketPath(env)))
}
```

- [x] **Step 4: Run the tests to verify they pass**

```bash
nix develop -c go test ./internal/tmux/ -v
```

Expected: PASS, including the pre-existing `withSynthesizedTmuxEnv` tests.

- [x] **Step 5: Commit**

```bash
git add internal/tmux/client.go internal/tmux/client_test.go
nix develop -c git commit -m "refactor(tmux): extract SocketPath from withSynthesizedTmuxEnv (#130)"
```

---

### Task 2: Migration and scoped event reads

Adds the column, the index, the `server_state` table, and binds the key to `Store`. Includes the mechanical update of all 55 existing `store.Open` call sites, because the signature change breaks the build until they are done.

**Files:**
- Create: `internal/store/migrations/0002_server_partition.sql`
- Modify: `internal/store/store.go:20-42` (struct + `Open`), `:122-187` (`InsertEvent`, `LatestSnapshotBefore`, `LatestSnapshot`), `:188-234` (`ListEvents`)
- Modify (mechanical): every `store.Open` call site listed by the Step 5 grep
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func Open(ctx context.Context, path, serverKey string) (*Store, error)`; `func (s *Store) ServerKey() string`.

- [x] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:

```go
func TestEventsAreScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/run/user/1000/tmux-1000/default")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/tmp/tmux-1000/default")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	ev := func(ts int64) store.Event {
		return store.Event{Ts: ts, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}
	}
	if _, err := a.InsertEvent(ctx, ev(1000)); err != nil {
		t.Fatalf("insert into a: %v", err)
	}
	// Newer, and in the other lane: the bug this partition exists to stop is
	// lane a resolving a close against this row.
	if _, err := b.InsertEvent(ctx, ev(2000)); err != nil {
		t.Fatalf("insert into b: %v", err)
	}

	snap, err := a.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil || snap.Ts != 1000 {
		t.Fatalf("lane a LatestSnapshot = %+v, want ts 1000", snap)
	}

	before, err := a.LatestSnapshotBefore(ctx, 3000)
	if err != nil {
		t.Fatalf("LatestSnapshotBefore: %v", err)
	}
	if before == nil || before.Ts != 1000 {
		t.Fatalf("lane a LatestSnapshotBefore = %+v, want ts 1000", before)
	}

	evs, err := a.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("lane a ListEvents returned %d events, want 1", len(evs))
	}
}
```

Update the existing `TestOpenAppliesMigrations` assertion in the same file from `want 1` to `want 2`:

```go
	if version != 2 {
		t.Errorf("user_version = %d, want 2", version)
	}
```

- [x] **Step 2: Run the test to verify it fails**

```bash
nix develop -c go test ./internal/store/ -run TestEventsAreScopedByServerKey
```

Expected: FAIL to build — `too many arguments in call to store.Open`.

- [x] **Step 3: Write the migration**

Create `internal/store/migrations/0002_server_partition.sql`:

```sql
-- Pre-migration rows carry no socket, and nothing in a stored manifest
-- identifies one, so they cannot be attributed to a server after the fact.
-- Dropping them cascades event_scrollbacks, whose trigger zeroes every blob's
-- refcount; the next gc collects the files.
DELETE FROM events;

ALTER TABLE events ADD COLUMN server_key TEXT NOT NULL DEFAULT '';

DROP INDEX events_kind_ts;
DROP INDEX events_ts;
CREATE INDEX events_server_kind_ts ON events(server_key, kind, ts DESC);
CREATE INDEX events_server_ts      ON events(server_key, ts DESC);

CREATE TABLE server_state (
    server_key TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    PRIMARY KEY (server_key, key)
) STRICT;
```

- [x] **Step 4: Bind the key to Store and scope the reads**

In `internal/store/store.go`, change the struct and `Open`:

```go
// Store wraps a *sql.DB connection to the tmux-remux SQLite database, scoped
// to one tmux server. Every method filters on serverKey, so a second server
// sharing the file cannot read or overwrite this one's rows. The scrollback
// tables are the deliberate exception — blobs are content-addressed and
// shared across servers by refcount.
type Store struct {
	db        *sql.DB
	serverKey string
}

// Open opens (or creates) the SQLite database at path, runs any pending
// migrations, and returns a *Store scoped to serverKey — the tmux socket
// path, from [tmux.SocketPath].
func Open(ctx context.Context, path, serverKey string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	s := &Store{db: db, serverKey: serverKey}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// ServerKey returns the tmux socket path this Store is scoped to.
func (s *Store) ServerKey() string { return s.serverKey }
```

`InsertEvent`:

```go
func (s *Store) InsertEvent(ctx context.Context, ev Event) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO events (ts, kind, scope, reason, host, parent_event_id, manifest_json, server_key)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, ev.Ts, ev.Kind, ev.Scope, ev.Reason, ev.Host, ev.ParentEventID, ev.ManifestJSON, s.serverKey)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	return res.LastInsertId()
}
```

`LatestSnapshotBefore` — add the clause and the leading arg:

```go
	row := s.db.QueryRowContext(ctx, `
		SELECT id, ts, kind, scope, reason, host, parent_event_id, manifest_json
		FROM events
		WHERE server_key = ? AND kind = 'snapshot' AND ts < ?
		ORDER BY ts DESC, id DESC
		LIMIT 1
	`, s.serverKey, ts)
```

`LatestSnapshot`:

```go
	row := s.db.QueryRowContext(ctx, `
		SELECT id, ts, kind, scope, reason, host, parent_event_id, manifest_json
		FROM events
		WHERE server_key = ? AND kind = 'snapshot'
		ORDER BY ts DESC, id DESC
		LIMIT 1
	`, s.serverKey)
```

`ListEvents` — seed the clause and arg slices so the server filter is always first, and drop the now-dead emptiness check:

```go
	var b strings.Builder
	b.WriteString(`SELECT id, ts, kind, scope, reason, host, parent_event_id, manifest_json FROM events`)
	clauses := []string{"server_key = ?"}
	args := []any{s.serverKey}
	if len(opts.Kinds) > 0 {
		placeholders := make([]string, len(opts.Kinds))
		for i, k := range opts.Kinds {
			placeholders[i] = "?"
			args = append(args, k)
		}
		clauses = append(clauses, "kind IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(opts.ExcludeKinds) > 0 {
		placeholders := make([]string, len(opts.ExcludeKinds))
		for i, k := range opts.ExcludeKinds {
			placeholders[i] = "?"
			args = append(args, k)
		}
		clauses = append(clauses, "kind NOT IN ("+strings.Join(placeholders, ",")+")")
	}
	b.WriteString(" WHERE ")
	b.WriteString(strings.Join(clauses, " AND "))
	b.WriteString(" ORDER BY ts DESC, id DESC")
```

The rest of `ListEvents` is unchanged.

- [x] **Step 5: Update every existing call site mechanically**

All pre-existing callers are tests plus the one production site in `withStore`. Give them a fixed literal; Task 6 replaces the production one.

```bash
grep -rln 'store\.Open(' --include='*.go' . | xargs sed -i -E \
  's/store\.Open\((ctx|context\.Background\(\)), (.*)\)$/store.Open(\1, \2, "\/tmp\/tmux-test\/default")/'
nix develop -c gofmt -l .
```

Expected: `gofmt -l` prints nothing. Then confirm none were missed:

```bash
grep -rn 'store\.Open(' --include='*.go' . | grep -v 'tmux-test' | grep -v 'func Open'
```

Expected: exactly one line, `cmd/tmux-remux/main.go`, which the sed also rewrote — leave it for now.

- [x] **Step 6: Run the tests to verify they pass**

```bash
nix develop -c go test ./internal/store/ -v
```

Expected: PASS, including `TestEventsAreScopedByServerKey` and `TestOpenAppliesMigrations` at `user_version = 2`.

- [x] **Step 7: Add the close-resolution regression test**

`internal/closeevent` needs no code change — `resolveAtCapture` calls
`db.LatestSnapshot`, which Step 4 just scoped. Pin that, because it is the
second half of the reported incident: a close on the live server resolved
against a ghost server's manifest and linked no scrollback.

Append to `internal/closeevent/capture_test.go`:

```go
// TestCaptureResolvesWithinItsOwnServer pins the second half of the
// 2026-09-15 incident: a close event must resolve against its own server's
// latest snapshot, never a newer one written by a different tmux server.
func TestCaptureResolvesWithinItsOwnServer(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "t.db")

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	aManifest := snapshot.Manifest{
		V: 1, Host: "h", SavedAt: 1000,
		Sessions: []snapshot.Session{{
			Name: "alpha",
			Windows: []snapshot.Window{{
				Index: 1, Name: "w1", ID: "@1",
				Panes: []snapshot.Pane{{Index: 1, ID: "%1", Cwd: "/x", Command: "nvim"}},
			}},
		}},
	}
	aJSON, err := json.Marshal(aManifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.InsertEvent(ctx, store.Event{
		Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(aJSON),
	}); err != nil {
		t.Fatalf("insert a snapshot: %v", err)
	}

	// Lane b's snapshot is newer and shares no pane ids. Unscoped,
	// LatestSnapshot would return this and resolution would find nothing.
	bJSON, err := json.Marshal(snapshot.Manifest{V: 1, Host: "h", SavedAt: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.InsertEvent(ctx, store.Event{
		Ts: 5000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: string(bJSON),
	}); err != nil {
		t.Fatalf("insert b snapshot: %v", err)
	}

	snap, err := a.LatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if snap == nil || snap.Ts != 1000 {
		t.Fatalf("lane a LatestSnapshot = %+v, want the ts-1000 snapshot", snap)
	}
}
```

Add `"encoding/json"`, `"path/filepath"` and the `snapshot` import to that
file if absent.

- [x] **Step 8: Run it**

```bash
nix develop -c go test ./internal/closeevent/ -run TestCaptureResolvesWithinItsOwnServer -v
```

Expected: PASS.

- [x] **Step 9: Commit**

```bash
git add internal/store cmd integration_test.go internal/closeevent
nix develop -c git commit -m "feat(store): partition events by tmux server key (#130)"
```

---

### Task 3: Scope the prune methods

Every prune subquery is a second place the lane must appear. A `NOT IN (SELECT ... LIMIT 20)` that is not itself scoped lets lane B's newest 20 protect lane A's rows from deletion — and, worse, lets lane A's `MIN(ts)` floor delete lane B's close events.

**Files:**
- Modify: `internal/store/store.go:241-320` (`PruneSnapshots`, `PruneUnresolvableCloseEvents`, `PruneCloseEvents`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `Open(ctx, path, serverKey)`, `InsertEvent` from Task 2.
- Produces: no signature changes.

- [x] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:

```go
func TestPruneIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()
	now := time.Now().UnixMilli()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	// Same day so the per-day retention floor cannot rescue anything, and
	// older than a week so it does not apply at all.
	base := now - 30*24*int64(time.Hour/time.Millisecond)
	for i := 0; i < 5; i++ {
		snap := store.Event{Ts: base + int64(i), Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}
		if _, err := a.InsertEvent(ctx, snap); err != nil {
			t.Fatalf("insert a snapshot: %v", err)
		}
		if _, err := b.InsertEvent(ctx, snap); err != nil {
			t.Fatalf("insert b snapshot: %v", err)
		}
	}

	if err := a.PruneSnapshots(ctx, 2, now); err != nil {
		t.Fatalf("PruneSnapshots: %v", err)
	}

	aEvs, err := a.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents a: %v", err)
	}
	if len(aEvs) != 2 {
		t.Errorf("lane a kept %d snapshots, want 2", len(aEvs))
	}
	bEvs, err := b.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents b: %v", err)
	}
	if len(bEvs) != 5 {
		t.Errorf("lane b kept %d snapshots, want 5 — pruning lane a must not touch lane b", len(bEvs))
	}
}

func TestPruneCloseEventsIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	// Lane a's snapshot floor sits above every one of lane b's closes. Unscoped,
	// PruneUnresolvableCloseEvents would delete all of lane b's history.
	if _, err := a.InsertEvent(ctx, store.Event{Ts: 9000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert a snapshot: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := b.InsertEvent(ctx, store.Event{Ts: int64(100 + i), Kind: "pane-died", Scope: "pane", Host: "h", ManifestJSON: "{}"}); err != nil {
			t.Fatalf("insert b close: %v", err)
		}
	}

	if _, err := a.PruneCloseEvents(ctx, 50); err != nil {
		t.Fatalf("PruneCloseEvents: %v", err)
	}

	bEvs, err := b.ListEvents(ctx, store.ListOpts{})
	if err != nil {
		t.Fatalf("ListEvents b: %v", err)
	}
	if len(bEvs) != 3 {
		t.Errorf("lane b kept %d close events, want 3", len(bEvs))
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

```bash
nix develop -c go test ./internal/store/ -run 'TestPrune.*ScopedByServerKey' -v
```

Expected: FAIL. `TestPruneIsScopedByServerKey` reports lane b kept 2, not 5; `TestPruneCloseEventsIsScopedByServerKey` reports lane b kept 0, not 3.

- [x] **Step 3: Implement**

`PruneSnapshots` body:

```go
	weekAgo := nowMs - 7*24*int64(time.Hour/time.Millisecond)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE kind = 'snapshot'
		  AND server_key = ?
		  AND id NOT IN (
		      SELECT id FROM events
		      WHERE kind = 'snapshot' AND server_key = ?
		      ORDER BY ts DESC
		      LIMIT ?
		  )
		  AND id NOT IN (
		      SELECT id FROM events
		      WHERE kind = 'snapshot' AND server_key = ? AND ts >= ?
		        AND ts IN (
		            SELECT max(ts)
		            FROM events
		            WHERE kind = 'snapshot' AND server_key = ? AND ts >= ?
		            GROUP BY date(ts/1000, 'unixepoch')
		        )
		  )
	`, s.serverKey, s.serverKey, keep, s.serverKey, weekAgo, s.serverKey, weekAgo)
```

`PruneUnresolvableCloseEvents` body:

```go
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE kind != 'snapshot'
		  AND server_key = ?
		  AND ts <= (SELECT MIN(ts) FROM events WHERE kind = 'snapshot' AND server_key = ?)
	`, s.serverKey, s.serverKey)
```

`PruneCloseEvents` first statement:

```go
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE kind != 'snapshot'
		  AND server_key = ?
		  AND id NOT IN (
		      SELECT id FROM events
		      WHERE kind != 'snapshot' AND server_key = ?
		      ORDER BY ts DESC
		      LIMIT ?
		  )
	`, s.serverKey, s.serverKey, keep)
```

- [x] **Step 4: Run the tests to verify they pass**

```bash
nix develop -c go test ./internal/store/ -v
```

Expected: PASS, all pre-existing prune tests included.

- [x] **Step 5: Commit**

```bash
git add internal/store
nix develop -c git commit -m "fix(store): scope prune subqueries to the server key (#130)"
```

---

### Task 4: Per-server throttle state

Moves the three save-throttle keys off the global `meta` table. This is the fix for the reported incident, and `internal/snapshot/save.go` needs no edit — it calls `s.db.GetMeta`, which is now scoped by construction.

**Files:**
- Modify: `internal/store/store.go:360-385` (`SetMeta`, `GetMeta`)
- Test: `internal/store/store_test.go`, `internal/snapshot/save_test.go`

**Interfaces:**
- Consumes: `Open(ctx, path, serverKey)` from Task 2.
- Produces: no signature changes. `GetMeta`/`SetMeta` now read and write `server_state`.

- [x] **Step 1: Write the failing tests**

Append to `internal/store/store_test.go`:

```go
func TestMetaIsScopedByServerKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	if err := a.SetMeta(ctx, "last_save_ts", "1000"); err != nil {
		t.Fatalf("SetMeta a: %v", err)
	}
	if err := b.SetMeta(ctx, "last_save_ts", "2000"); err != nil {
		t.Fatalf("SetMeta b: %v", err)
	}

	got, err := a.GetMeta(ctx, "last_save_ts")
	if err != nil {
		t.Fatalf("GetMeta a: %v", err)
	}
	if got != "1000" {
		t.Errorf("lane a last_save_ts = %q, want %q", got, "1000")
	}
}
```

Append to `internal/snapshot/save_test.go` — the regression test for the reported incident:

```go
// TestSaveThrottleDoesNotCrossServers reproduces the 2026-09-15 incident: two
// tmux servers sharing one state.db saved inside the same second, and the
// second one measured the first one's last_save_ts, came back throttled, and
// dropped its scrollback.
func TestSaveThrottleDoesNotCrossServers(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()
	sb := scrollback.New(filepath.Join(dir, "scrollbacks"))

	newSaver := func(serverKey, sessionName string) (*snapshot.Saver, *store.Store) {
		t.Helper()
		db, err := store.Open(ctx, dbPath, serverKey)
		if err != nil {
			t.Fatalf("Open %s: %v", serverKey, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		cc := &captureClient{
			fakeClient: &fakeClient{
				sessions: []tmux.SessionRow{{Name: sessionName, LastAttached: 100}},
				windows:  []tmux.WindowRow{{Session: sessionName, Index: 1, Name: "w1", Layout: "L"}},
				panes:    []tmux.PaneRow{{Session: sessionName, WindowIndex: 1, PaneIndex: 1, Cwd: "/x", Command: "nvim", PID: 1, LastUsed: 1}},
			},
			captured: map[string][]byte{sessionName + ":1.1": []byte("hello " + sessionName)},
		}
		return snapshot.NewSaver(db, sb, cc, snapshot.SaverOptions{
			Host: "test", CaptureScrollback: true, MinSaveInterval: 30 * time.Second,
		}), db
	}

	saverA, dbA := newSaver("/sock/a", "alpha")
	saverB, dbB := newSaver("/sock/b", "bravo")

	if err := saverA.Save(ctx, "timer"); err != nil {
		t.Fatalf("save a: %v", err)
	}
	// No sleep: B saves inside A's MinSaveInterval, exactly as the three
	// same-second timer hooks did.
	if err := saverB.Save(ctx, "timer"); err != nil {
		t.Fatalf("save b: %v", err)
	}

	for _, tc := range []struct {
		name string
		db   *store.Store
	}{{"a", dbA}, {"b", dbB}} {
		snap, err := tc.db.LatestSnapshot(ctx)
		if err != nil {
			t.Fatalf("LatestSnapshot %s: %v", tc.name, err)
		}
		if snap == nil {
			t.Fatalf("lane %s has no snapshot", tc.name)
		}
		var m snapshot.Manifest
		if err := json.Unmarshal([]byte(snap.ManifestJSON), &m); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.name, err)
		}
		if m.ScrollbackSkipped {
			t.Errorf("lane %s skipped scrollback — the other server's save throttled it", tc.name)
		}
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

```bash
nix develop -c go test ./internal/store/ -run TestMetaIsScopedByServerKey -v
nix develop -c go test ./internal/snapshot/ -run TestSaveThrottleDoesNotCrossServers -v
```

Expected: `TestMetaIsScopedByServerKey` FAILs with `lane a last_save_ts = "2000"`. `TestSaveThrottleDoesNotCrossServers` FAILs with `lane b skipped scrollback`.

- [x] **Step 3: Implement**

Replace `SetMeta` and `GetMeta` in `internal/store/store.go`:

```go
// SetMeta upserts a key/value for this Store's server. Keys live in
// server_state, not meta: a value like last_save_ts describes one tmux
// server's history and sharing it lets a second server's save throttle this
// one's.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_state (server_key, key, value) VALUES (?, ?, ?)
		ON CONFLICT(server_key, key) DO UPDATE SET value = excluded.value
	`, s.serverKey, key, value)
	if err != nil {
		return fmt.Errorf("set meta: %w", err)
	}
	return nil
}

// GetMeta returns the value for key in this Store's server, or "" if absent.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM server_state WHERE server_key = ? AND key = ?`, s.serverKey, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get meta: %w", err)
	}
	return v, nil
}
```

- [x] **Step 4: Run the tests to verify they pass**

```bash
nix develop -c go test ./internal/store/ ./internal/snapshot/ -v
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/store internal/snapshot
nix develop -c git commit -m "fix(store): move save throttle state into server_state (#130)"
```

---

### Task 5: Lane reaping in gc

Per-server pruning bounds each lane but never empties a lane whose socket will not come back. The filesystem check lives in gc, not in the store, so the store stays SQL-only.

**Files:**
- Modify: `internal/store/store.go` (append the two cross-lane methods)
- Modify: `cmd/tmux-remux/main.go:850-875` (`GCCmd.Run`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `Open(ctx, path, serverKey)`, `ServerKey()` from Task 2.
- Produces: `type ServerLane struct { Key string; NewestTs int64 }`; `func (s *Store) ListServerLanes(ctx context.Context) ([]ServerLane, error)`; `func (s *Store) DeleteServerLane(ctx context.Context, serverKey string) (int64, error)`.

- [x] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`:

```go
func TestListAndDeleteServerLanes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	a, err := store.Open(ctx, dbPath, "/sock/a")
	if err != nil {
		t.Fatalf("Open lane a: %v", err)
	}
	defer a.Close()
	b, err := store.Open(ctx, dbPath, "/sock/b")
	if err != nil {
		t.Fatalf("Open lane b: %v", err)
	}
	defer b.Close()

	if _, err := a.InsertEvent(ctx, store.Event{Ts: 1000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert a: %v", err)
	}
	if _, err := b.InsertEvent(ctx, store.Event{Ts: 2000, Kind: "snapshot", Scope: "server", Host: "h", ManifestJSON: "{}"}); err != nil {
		t.Fatalf("insert b: %v", err)
	}
	if err := b.SetMeta(ctx, "last_save_ts", "2000"); err != nil {
		t.Fatalf("SetMeta b: %v", err)
	}

	lanes, err := a.ListServerLanes(ctx)
	if err != nil {
		t.Fatalf("ListServerLanes: %v", err)
	}
	want := map[string]int64{"/sock/a": 1000, "/sock/b": 2000}
	if len(lanes) != len(want) {
		t.Fatalf("ListServerLanes returned %d lanes, want %d", len(lanes), len(want))
	}
	for _, lane := range lanes {
		if ts, ok := want[lane.Key]; !ok || ts != lane.NewestTs {
			t.Errorf("lane %q newest = %d, want %d", lane.Key, lane.NewestTs, want[lane.Key])
		}
	}

	// Reaping from lane a must clear lane b's events and its server_state.
	n, err := a.DeleteServerLane(ctx, "/sock/b")
	if err != nil {
		t.Fatalf("DeleteServerLane: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteServerLane removed %d events, want 1", n)
	}
	got, err := b.GetMeta(ctx, "last_save_ts")
	if err != nil {
		t.Fatalf("GetMeta b: %v", err)
	}
	if got != "" {
		t.Errorf("lane b last_save_ts = %q after reaping, want empty", got)
	}
	if evs, err := a.ListEvents(ctx, store.ListOpts{}); err != nil || len(evs) != 1 {
		t.Errorf("lane a has %d events (err %v), want 1 — reaping b must not touch a", len(evs), err)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

```bash
nix develop -c go test ./internal/store/ -run TestListAndDeleteServerLanes
```

Expected: FAIL to build — `a.ListServerLanes undefined`.

- [x] **Step 3: Implement the store methods**

Append to `internal/store/store.go`:

```go
// ServerLane is one partition of the events table, keyed by tmux socket path.
type ServerLane struct {
	Key      string
	NewestTs int64
}

// ListServerLanes returns every server_key present in events with its newest
// event timestamp. Cross-lane: unlike every other read, it is not scoped to
// this Store's server. Used by gc to find lanes whose server is gone.
func (s *Store) ListServerLanes(ctx context.Context) ([]ServerLane, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_key, MAX(ts) FROM events GROUP BY server_key`)
	if err != nil {
		return nil, fmt.Errorf("list server lanes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ServerLane
	for rows.Next() {
		var lane ServerLane
		if err := rows.Scan(&lane.Key, &lane.NewestTs); err != nil {
			return nil, fmt.Errorf("scan server lane: %w", err)
		}
		out = append(out, lane)
	}
	return out, rows.Err()
}

// DeleteServerLane removes every event and every server_state row belonging to
// serverKey, returning the event count. Cross-lane, like ListServerLanes.
// Orphaned scrollback blobs are left to the caller's zero-refcount sweep.
func (s *Store) DeleteServerLane(ctx context.Context, serverKey string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE server_key = ?`, serverKey)
	if err != nil {
		return 0, fmt.Errorf("delete server lane events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete server lane events: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM server_state WHERE server_key = ?`, serverKey); err != nil {
		return 0, fmt.Errorf("delete server lane state: %w", err)
	}
	return n, nil
}
```

- [x] **Step 4: Run the test to verify it passes**

```bash
nix develop -c go test ./internal/store/ -run TestListAndDeleteServerLanes -v
```

Expected: PASS.

- [x] **Step 5: Write the failing test for the reaping decision**

The three reaping rules are the part worth testing, and they are untestable
buried in a loop that calls `os.Stat`. They go in a pure helper.

Create `cmd/tmux-remux/gc_test.go`:

```go
package main

import (
	"testing"

	"github.com/noamsto/tmux-remux/internal/store"
)

func TestLanesToReap(t *testing.T) {
	const self = "/sock/self"
	const cutoff = 1000
	present := map[string]bool{"/sock/alive": true}
	exists := func(path string) bool { return present[path] }

	lanes := []store.ServerLane{
		{Key: self, NewestTs: 1},
		{Key: "/sock/alive", NewestTs: 1},
		{Key: "/sock/recent", NewestTs: 2000},
		{Key: "/sock/ghost", NewestTs: 1},
	}

	got := lanesToReap(lanes, self, cutoff, exists)
	want := []string{"/sock/ghost"}
	if len(got) != len(want) || (len(got) == 1 && got[0] != want[0]) {
		t.Errorf("lanesToReap() = %v, want %v", got, want)
	}
}
```

- [x] **Step 6: Run it to verify it fails**

```bash
nix develop -c go test ./cmd/tmux-remux/ -run TestLanesToReap
```

Expected: FAIL to build — `undefined: lanesToReap`.

- [x] **Step 7: Implement the helper and wire gc**

Add to `cmd/tmux-remux/main.go`:

```go
// lanesToReap returns the server keys whose events gc should delete: not this
// server's own lane, no events newer than cutoff, and no socket file left on
// disk. A socket that still exists with no server behind it is the normal
// state between a server dying and restore running, so reaping it would
// delete exactly what restore needs.
func lanesToReap(lanes []store.ServerLane, self string, cutoff int64, exists func(string) bool) []string {
	var out []string
	for _, lane := range lanes {
		if lane.Key == self || lane.NewestTs > cutoff || exists(lane.Key) {
			continue
		}
		out = append(out, lane.Key)
	}
	return out
}
```

In `GCCmd.Run`, insert this after `sb := scrollback.New(cfg.ScrollbackDir)` and **before** the `db.ScrollbacksWithZeroRef` call, so the existing sweep collects the blobs reaping orphans:

```go
		lanes, err := db.ListServerLanes(ctx)
		if err != nil {
			return err
		}
		cutoff := time.Now().Add(-cfg.RestoreMaxSnapshotAge).UnixMilli()
		socketExists := func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
		for _, key := range lanesToReap(lanes, db.ServerKey(), cutoff, socketExists) {
			n, err := db.DeleteServerLane(ctx, key)
			if err != nil {
				log.Logf("gc: reap lane %s: %v", key, err)
				continue
			}
			log.Logf("gc: reaped lane %s (%d events)", key, n)
		}
```

`os`, `time` and the `store` import are already present in that file.

- [x] **Step 8: Run the full suite**

```bash
nix develop -c go build ./... && nix develop -c go test ./...
```

Expected: PASS.

- [x] **Step 9: Commit**

```bash
git add internal/store cmd/tmux-remux/main.go cmd/tmux-remux/gc_test.go
nix develop -c git commit -m "feat(gc): reap event lanes whose tmux socket is gone (#130)"
```

---

### Task 6: Wire the production server key

Until this task, `withStore` passes the test literal the Task 2 sed left behind. This replaces it with the real socket path.

`withStore` cannot be called from a test — it takes a signal context, loads the real config and takes the lockfile. The testable seam is the key derivation itself, so that gets its own function and `withStore` calls it.

**Files:**
- Modify: `cmd/tmux-remux/main.go:78-96` (`withStore`, plus the new `serverKey`)
- Test: `cmd/tmux-remux/server_key_test.go`

**Interfaces:**
- Consumes: `tmux.SocketPath` (Task 1), `store.Open(ctx, path, serverKey)` (Task 2).
- Produces: `func serverKey() string` in package `main`.

- [x] **Step 1: Write the failing test**

Create `cmd/tmux-remux/server_key_test.go`:

```go
package main

import (
	"fmt"
	"os"
	"testing"
)

// TestServerKeyFollowsTmuxEnv pins the production key derivation: the store
// partition must be the socket this process's tmux calls target. A constant
// here puts a hook's close events and a timer's snapshots in different lanes
// on one server, which is the bug this branch exists to fix.
func TestServerKeyFollowsTmuxEnv(t *testing.T) {
	t.Setenv("TMUX", "/run/user/1000/tmux-1000/default,123,4")
	if got := serverKey(); got != "/run/user/1000/tmux-1000/default" {
		t.Errorf("serverKey() = %q, want the socket from TMUX", got)
	}
}

func TestServerKeyFallsBackToDefaultSocket(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", "/run/user/1000")
	want := fmt.Sprintf("/run/user/1000/tmux-%d/default", os.Getuid())
	if got := serverKey(); got != want {
		t.Errorf("serverKey() = %q, want %q", got, want)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

```bash
nix develop -c go test ./cmd/tmux-remux/ -run TestServerKey -v
```

Expected: FAIL to build — `undefined: serverKey`.

- [x] **Step 3: Implement**

Add to `cmd/tmux-remux/main.go`, above `withStore`:

```go
// serverKey returns the store partition for the tmux server this process
// targets. Every command must derive it the same way, or a hook's close
// events and a timer's snapshots land in different lanes on one server.
func serverKey() string {
	return tmux.SocketPath(os.Environ())
}
```

And in `withStore`, replace the literal the Task 2 sed left behind:

```go
	db, err := store.Open(ctx, cfg.DBPath, serverKey())
```

Confirm `github.com/noamsto/tmux-remux/internal/tmux` is in that file's import block; add it if not.

- [x] **Step 4: Verify the literal is gone and everything passes**

```bash
grep -rn 'tmux-test/default' cmd/ internal/ integration_test.go | grep -v '_test.go'
```

Expected: no output — the literal survives only in tests.

```bash
nix develop -c go build ./... && nix develop -c go test ./...
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add cmd/tmux-remux/main.go cmd/tmux-remux/server_key_test.go
nix develop -c git commit -m "feat(cli): key the store by the tmux socket path (#130)"
```

---

### Task 7: Manual verification against the real server

The bug was found in live data, so the fix gets checked there too. This task changes no code.

**Files:** none.

**Interfaces:** none.

- [ ] **Step 1: Build and point it at a scratch database**

```bash
nix develop -c go build -o /tmp/remux-130 ./cmd/tmux-remux
mkdir -p /tmp/remux-130-data
XDG_DATA_HOME=/tmp/remux-130-data /tmp/remux-130 save --reason=manual
```

- [ ] **Step 2: Confirm the lane is the live socket and scrollback was captured**

```bash
sqlite3 /tmp/remux-130-data/tmux-remux/state.db \
  "SELECT server_key, kind, json_extract(manifest_json,'\$.scrollback_skipped') FROM events;"
```

Expected: one row, `server_key` = the value of `$TMUX`'s first field (`/run/user/1000/tmux-1000/default`), and the skipped column empty or 0.

- [ ] **Step 3: Prove a second lane cannot throttle the first**

```bash
tmux -L remux130 -f /dev/null new-session -d -s probe
XDG_DATA_HOME=/tmp/remux-130-data TMUX=/run/user/$UID/tmux-$UID/remux130,0,0 /tmp/remux-130 save --reason=manual
XDG_DATA_HOME=/tmp/remux-130-data /tmp/remux-130 save --reason=manual
sqlite3 /tmp/remux-130-data/tmux-remux/state.db \
  "SELECT server_key, count(*), sum(coalesce(json_extract(manifest_json,'\$.scrollback_skipped'),0)) FROM events GROUP BY server_key;"
```

Expected: two rows, one per socket, and a `0` in the skipped column for both — the second save is not throttled by the first server's timestamp. Before this branch, the third command's save would have been throttled.

- [ ] **Step 4: Clean up**

```bash
tmux -L remux130 kill-server
gtrash put /tmp/remux-130-data /tmp/remux-130
```

- [ ] **Step 5: Push and open the PR**

```bash
git push -u origin fix/130-partition-state-by-tmux-server
gh pr create --assignee @me --fill
```
