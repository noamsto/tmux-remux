-- events.parent_event_id self-references events(id) ON DELETE SET NULL with
-- no supporting index, so without this index SQLite full-scans events once
-- per deleted row to resolve the FK action (and disables its truncate
-- optimization) — quadratic on a bulk delete. Created before the DELETE
-- below so that statement benefits, and kept afterward since DeleteServerLane
-- deletes in bulk too.
CREATE INDEX events_parent ON events(parent_event_id);

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
