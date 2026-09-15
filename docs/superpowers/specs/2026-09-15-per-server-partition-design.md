# Per-server state partition — design

Issue: [#130](https://github.com/noamsto/tmux-remux/issues/130)
Date: 2026-09-15

## Problem

Every tmux server that has remux triggers installed writes to the same
`~/.local/share/tmux-remux/state.db`. Nothing in the schema records which
server wrote a row, so `state.db` is a single shared lane that N servers
overwrite in turn.

Observed live on 2026-09-15 with three servers running at once: the live
server at `/run/user/1000/tmux-1000/default`, plus two ghosts left over from
early-September debugging (`-L norgb`, and a `/tmp/tmux-1000/default` from
before `TMUX_TMPDIR` moved to `/run/user/1000`). All three had the tmux 3.8
monitor-save hook installed, so all three ran `save --reason=timer` once a
minute.

### 1. The throttle is a single global key

`Saver.Save` reads and writes three `meta` rows — `last_save_ts`,
`last_save_fingerprint`, `last_save_structure_fingerprint`
(`internal/snapshot/save.go:74-139`). They are not keyed by anything.

Three servers firing on the same minute boundary land in the same second. The
first writer stamps `last_save_ts`; the other two then measure
`time.Since(prevTS) < MinSaveInterval`, come back `throttled`, and take the
`ScrollbackSkipped = true` branch. Structure still saves — that part is
deliberate and correct — but the pane text does not.

Measured over the retained history:

| socket | snapshots | captured scrollback |
|---|---|---|
| `/run/user/1000/tmux-1000/norgb` (ghost) | 8 | 5 |
| `/run/user/1000/tmux-1000/default` (live) | 8 | 1 |
| `/tmp/tmux-1000/default` (ghost) | 11 | 0 |

The live server lost the race almost every time, which is why every preview in
the close picker read `(scrollback skipped — saved within min_save_interval)`.

### 2. Close resolution reads the newest snapshot from any server

`resolveAtCapture` calls `db.LatestSnapshot(ctx)`
(`internal/closeevent/capture.go:155`), which is `ORDER BY ts DESC LIMIT 1`
across the whole table. A `pane-died` hook on the live server therefore
resolves against whichever server wrote last. When that is a ghost, the pane id
matches nothing and `linkResolvedScrollback` links nothing.

13 of the last 15 close events had zero rows in `event_scrollbacks`.

The same blindness applies to `LatestSnapshotBefore`
(`cmd/tmux-remux/main.go:209,470`), which drives restore and undo — so a
restore could in principle re-create another server's sessions.

### 3. Pruning is shared

`SnapshotHistoryLimit` is 20. At three servers x one save per minute the
window covers about seven minutes of wall clock, so undo history is destroyed
long before its intended horizon. `PruneCloseEvents` has the same problem
against `CloseEventLimit`.

## Approach

Partition every read and write by the tmux **socket path**.

### Server identity

The key is the raw socket path, e.g. `/run/user/1000/tmux-1000/default`.

Two alternatives were rejected:

- **Server PID.** Dies with the server. Restore exists precisely to run after
  a server has died and a new one has taken the socket, so a PID key would
  orphan the history restore needs.
- **Socket basename.** Collides. The incident had two live sockets both named
  `default`, in different directories.

The path is stored raw rather than hashed. A readable key is worth more in a
database debugged by hand than the bytes a hash saves.

Derivation, in `internal/tmux`:

```go
// SocketPath returns the tmux socket path implied by env.
func SocketPath(env []string) string
```

`TMUX`'s first comma-field when `TMUX` is set — always the case under a hook,
which is how most saves run. Otherwise tmux's own default-socket rule:
`$TMUX_TMPDIR/tmux-<uid>/default`, with `/tmp` as the `TMUX_TMPDIR` fallback.

That rule already exists, inlined in `withSynthesizedTmuxEnv`
(`internal/tmux/client.go:76-93`). `SocketPath` takes it over and
`withSynthesizedTmuxEnv` calls it, so there is one definition instead of two
that can drift.

`host` stays on `events` as a record and is **not** part of the key. One
`state.db` lives in one machine's XDG data directory, and sessions mirrored
from a remote host are already excluded from snapshots via `Manifest.Bridged`.

### Schema

`internal/store/migrations/0002_server_partition.sql`:

```sql
DELETE FROM events;
ALTER TABLE events ADD COLUMN server_key TEXT NOT NULL DEFAULT '';
DROP INDEX events_kind_ts;
CREATE INDEX events_server_kind_ts ON events(server_key, kind, ts DESC);
CREATE TABLE server_state (
    server_key TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    PRIMARY KEY (server_key, key)
) STRICT;
```

Existing rows carry no socket, and nothing in a stored manifest identifies one,
so they cannot be attributed after the fact. They are dropped rather than
guessed at.

Dropping every event cascades `event_scrollbacks`, the
`decrement_scrollback_refcount` trigger drives every blob to `refcount = 0`,
and the next gc deletes them. **This is a full reset**: no previewable
scrollback until the first save after upgrade.

The three throttle keys move from `meta` to `server_state`. `meta` stays for
genuinely global keys. There are none today; keeping the tables apart is what
stops the next global-looking key from silently becoming shared state again.

### Store API

The key binds at construction rather than threading through every signature:

```go
func Open(ctx context.Context, path, serverKey string) (*Store, error)
```

`Store` holds `serverKey`, and `InsertEvent`, `LatestSnapshot`,
`LatestSnapshotBefore`, `ListEvents`, `PruneSnapshots`, `PruneCloseEvents`,
`GetMeta` and `SetMeta` each add `WHERE server_key = ?` internally.

Call sites do not change and cannot forget — the error is defined out of
existence rather than detected. The cost is that a test wanting two lanes opens
two `Store`s, which is an honest model of what two servers are.

`GetMeta`/`SetMeta` read and write `server_state`. If a genuinely global key
ever appears it gets `GetGlobalMeta`/`SetGlobalMeta` against `meta`, named so
the distinction is visible at the call site.

Two things stay deliberately unpartitioned:

- **The scrollback tables.** Blobs are content-addressed, so two servers that
  capture identical pane text legitimately share one row and one file;
  `refcount` already models that. Partitioning them would store the same bytes
  twice and break the refcount invariant.
- **Cross-lane operations**, which gc needs and a lane-bound `Store` cannot
  express. These get their own explicitly global names — `ListServerLanes(ctx)`
  and `DeleteServerLane(ctx, serverKey)` — rather than an escape hatch on the
  scoped methods. Every other method stays scoped, and the naming is what tells
  a reader which kind they are calling. The store exposes the two primitives
  only; which lanes qualify is gc's decision, so no filesystem knowledge leaks
  into a package that otherwise speaks nothing but SQL.

### Visibility

Hard partition. The picker shows only the current server's entries; a dead
server's closed windows are invisible and not restorable. This matches what the
close picker is for — undoing what just happened in front of you — and removes
any path to restoring into the wrong server.

### Lane reaping

Per-server pruning means N servers cost N x (20 snapshots + 50 close events).
Bounded, but a lane belonging to a socket that will never come back never
shrinks to zero.

`gc` gains one step: `ListServerLanes` reports every lane with its newest
timestamp, gc selects the ones whose socket path is **absent from disk** *and*
whose newest event is older than `RestoreMaxSnapshotAge`, and `DeleteServerLane`
removes each. The existing zero-refcount sweep then collects whatever blobs that
orphans, so scrollback cleanup needs no new machinery — which is why lane
reaping must run before the sweep, not after.

A socket file that exists with no server behind it is deliberately left alone.
That is the normal state between a server dying and restore running, and
reaping it would delete exactly what restore needs.

## Testing

| Test | Asserts |
|---|---|
| `store` two-lane | inserts, latest, prune and meta in lane A are invisible to lane B |
| `save` same-second | two servers saving inside one second both capture scrollback |
| `closeevent` cross-lane | a close in lane A with a newer snapshot in lane B resolves against A's snapshot |
| `tmux.SocketPath` | `TMUX` set; `TMUX` unset with `TMUX_TMPDIR`; both unset |
| migration | a pre-migration fixture ends with no events and `user_version = 2` |
| gc lane reaping | absent socket + old events reaped; absent socket + recent events kept; present socket never reaped |

The same-second save test is the regression test for the reported incident and
should be written first.

## Out of scope

The picker drops tmux colour directives instead of rendering them:
`snapshot.StripFormat` (`internal/snapshot/manifest.go:19-25`) deletes every
`#[fg=...]` run, so a window name's dim grey idle marker is painted at full row
colour. Real, unrelated, and tracked separately.
