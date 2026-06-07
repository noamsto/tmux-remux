# Restore race fix + snapshot retention safety net — design

Date: 2026-06-07
Status: approved (design), pending implementation

## Problem

On tmux server start, `restore --auto` races every writer of snapshots:

1. The `session-created[99]` hook fires `tmux-state save` the instant the first
   (fresh, single-session) session is created — at the same moment the
   config-load `run-shell -b 'tmux-state restore --auto'` runs.
2. The systemd user timer fires `save --reason=timer` within 60s of login.

`restore` selects `LatestSnapshot()`. If any post-start save commits before
restore reads, the "latest" snapshot **is the brand-new server**: restore
skips the already-running session and silently no-ops. The real pre-shutdown
snapshot is now second-latest and never consulted.

Two amplifiers turned this race from a glitch into data loss (observed
2026-06-07: sessions `agent-smith`, `lazytmux`, `mono` never restored and
unrecoverable by 09:10):

- **Count-based pruning.** `PruneSnapshots(keep=20)` runs after every save;
  with 60s timer saves, the pre-shutdown snapshot (the *oldest* row) is
  evicted ~20 minutes into the new server's life.
- **Zero observability.** Restore emits nothing on success, filtering, or
  failure, so a clobbered restore is indistinguishable from "I had nothing
  to restore".

## Fix 1: anchor restore to server start time

`restore` resolves the running server's start time and selects
`LatestSnapshotBefore(startTime)` instead of `LatestSnapshot()`. Restore's
semantics become: **"bring back the state from before this server existed."**
No snapshot written by the current server can ever be selected, so both
racers (hook save, timer save) are defined out of existence — no ordering
assumptions, no markers, no sleeps.

Mechanics:

- Server start time: `tmux display-message -p '#{start_time}'` (server-scoped
  format, epoch **seconds**; verified on tmux 3.x). Convert to millis to match
  `events.ts`. Add a `Client.ServerStartTime(ctx)` helper in `internal/tmux`.
- `LatestSnapshotBefore` already exists (`store.go`), is strictly `ts < ?`,
  and is used by the close-event picker. Pre-shutdown snapshots always satisfy
  `ts < start_time`; post-start saves never do.
- Applies to `restore` with and without `--auto` — consistent semantics. The
  interactive picker (`pick --kind=snapshot`) is unchanged and remains the way
  to restore an arbitrary snapshot.
- If the start-time query fails, restore returns the error (it cannot create
  sessions without a reachable server anyway); the error is logged (Fix 3).

Known limitation (accepted): after a quick start→kill→start cycle, the newest
pre-start snapshot may describe the short-lived intermediate server, not the
session-rich one before it. Same selection semantics as today minus the race;
day-thinned retention (Fix 2) plus `prefix + R` cover recovery.

## Fix 2: day-thinned snapshot retention

`PruneSnapshots` keeps the union of:

- the `SnapshotHistoryLimit` (default 20) newest snapshots, **and**
- the newest snapshot per UTC calendar day for the last 7 days
  (`date(ts/1000,'unixepoch')` grouping). UTC days keep retention deterministic across timezone changes; the safety-net guarantee is unchanged.

Bounded at ~27 rows, no schema change. A pre-shutdown snapshot now survives a
week of uptime instead of ~20 minutes, leaving ample time to notice a bad
restore and recover via `prefix + R`. Scrollback GC is unaffected: it already
reaps only scrollbacks unreferenced by surviving events.

## Fix 3: observability

- **Plan stats.** `restore.BuildPlan` additionally returns
  `PlanStats{SessionsKept, SessionsSkippedRunning, SessionsSkippedStale,
  SessionsSkippedIdle, WindowsSkippedIdle}` (pure, unit-testable).
- **Log file.** `state.log` next to `state.db` (`$XDG_DATA_HOME/tmux-state/`),
  plain `fmt.Fprintf` append; rotate to `state.log.old` past 1 MB. Restore
  logs: chosen snapshot (id, age), plan stats, actions applied, and any error.
  Top-level command errors (currently stderr-only, lost under `run-shell -b`)
  also land here.
- **Launch feedback.** After an auto-restore, when the plan created sessions
  or the filter skipped any:
  `tmux display-message "tmux-state: restored N sessions (M filtered)"`.

## Testing

- `internal/restore`: PlanStats unit tests alongside existing `plan_test.go`
  table cases (kept / skipped-running / skipped-stale / skipped-idle).
- `internal/store`: day-thinning prune test — seed snapshots across 10 days
  plus a burst of fresh ones; assert the per-day survivors and the limit.
- `internal/tmux`: `ServerStartTime` parse test (seconds → millis).
- Integration (`integration_test.go`): seed an old multi-session snapshot and
  a newer post-start snapshot; assert restore plans from the old one.

## Out of scope

- Smart-filter default changes (idle fish sessions vanishing silently) —
  separate discussion; Fix 3 makes the filtering visible.
- Server-generation tagging in the schema (rejected: migration cost for
  marginal benefit over day-thinning).
- lazytmux changes — none needed beyond bumping the pinned tmux-state once
  released.
