CREATE TABLE events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    kind            TEXT    NOT NULL,
    scope           TEXT    NOT NULL,
    reason          TEXT,
    host            TEXT    NOT NULL,
    parent_event_id INTEGER REFERENCES events(id) ON DELETE SET NULL,
    manifest_json   TEXT    NOT NULL
) STRICT;

CREATE INDEX events_kind_ts ON events(kind, ts DESC);
CREATE INDEX events_ts      ON events(ts DESC);

CREATE TABLE scrollbacks (
    sha256        TEXT PRIMARY KEY,
    bytes         INTEGER NOT NULL,
    refcount      INTEGER NOT NULL DEFAULT 0,
    last_used_ts  INTEGER NOT NULL
) STRICT;

CREATE TABLE event_scrollbacks (
    event_id       INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    pane_key       TEXT    NOT NULL,
    scrollback_sha TEXT    NOT NULL REFERENCES scrollbacks(sha256),
    PRIMARY KEY (event_id, pane_key)
) STRICT;

CREATE INDEX event_scrollbacks_sha ON event_scrollbacks(scrollback_sha);

CREATE TABLE live_index (
    session_id  TEXT NOT NULL PRIMARY KEY,
    payload     TEXT NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE TRIGGER decrement_scrollback_refcount
AFTER DELETE ON event_scrollbacks
BEGIN
    UPDATE scrollbacks
    SET refcount = refcount - 1
    WHERE sha256 = OLD.scrollback_sha;
END;
