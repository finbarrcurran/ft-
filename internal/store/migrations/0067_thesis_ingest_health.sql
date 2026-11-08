-- SC-55 P4 — loud ingest failures for the Thesis Library.
-- thesis_ingest_failures: one row per repo file that could not be ingested (header does not parse, duplicate
-- ticker+version, unreadable, or a database error); resolved_at is set when the file parses again or is gone.
-- thesis_sync_state: a single row, shared by every Engine instance, counting consecutive failed sync passes
-- (fetch or clone failures) so the "N failed ticks in a row" alert survives restarts.
CREATE TABLE IF NOT EXISTS thesis_ingest_failures (
    id          INTEGER PRIMARY KEY,
    path        TEXT    NOT NULL UNIQUE,
    kind        TEXT    NOT NULL DEFAULT 'header',      -- header | duplicate | read | database
    error       TEXT    NOT NULL,
    first_seen  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    resolved_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_thesis_ingest_failures_open ON thesis_ingest_failures (resolved_at);

CREATE TABLE IF NOT EXISTS thesis_sync_state (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    consecutive_failures  INTEGER NOT NULL DEFAULT 0,
    first_failure_at      INTEGER,
    last_error            TEXT,
    last_attempt_at       INTEGER,
    last_ok_at            INTEGER
);
INSERT OR IGNORE INTO thesis_sync_state (id) VALUES (1);
