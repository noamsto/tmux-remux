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
