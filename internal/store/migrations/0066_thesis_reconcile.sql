-- SC-55 P2 — repo-vs-database reconciliation of the Thesis Library. One row per run (after every sync and
-- nightly); the short history is pruned by the code (latest 90 kept). `detail` is a JSON array of issues
-- ({type, ticker, version, path, note}); `status` is ok | mismatch | aborted (aborted = the sync stopped
-- without changing anything: missing theses/ directory, or the archive safety stop).
CREATE TABLE IF NOT EXISTS thesis_reconcile_runs (
    id          INTEGER PRIMARY KEY,
    ran_at      INTEGER NOT NULL,
    trigger     TEXT    NOT NULL,                       -- sync | nightly
    status      TEXT    NOT NULL CHECK (status IN ('ok', 'mismatch', 'aborted')),
    files       INTEGER NOT NULL DEFAULT 0,             -- valid locked files in the repo
    rows        INTEGER NOT NULL DEFAULT 0,             -- all theses_index rows
    live        INTEGER NOT NULL DEFAULT 0,
    superseded  INTEGER NOT NULL DEFAULT 0,
    archived    INTEGER NOT NULL DEFAULT 0,
    mismatches  INTEGER NOT NULL DEFAULT 0,
    detail      TEXT    NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_thesis_reconcile_ran ON thesis_reconcile_runs (ran_at DESC);
