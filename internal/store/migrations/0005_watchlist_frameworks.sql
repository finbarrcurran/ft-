-- 0005_watchlist_frameworks.sql — Spec 4 D1.
--
-- Two new tables:
--   * `watchlist` — what Fin is considering owning (separate from holdings)
--   * `framework_scores` — append-only history of Jordi/Cowen scoring sessions
--     keyed by (target_kind, target_id) so both holdings and watchlist entries
--     can be scored with the same UI.
--
-- SQLite-isms:
--   * UNIQUE constraint includes deleted_at so soft-delete + re-add of the same
--     ticker is allowed (NULL!=NULL in SQLite's index semantics).
--   * No foreign keys to holdings — promoted_holding_id is a soft pointer that
--     can dangle if the holding is later hard-deleted (we soft-delete so this
--     is never an issue in practice).

CREATE TABLE watchlist (
    id                    INTEGER PRIMARY KEY,
    user_id               INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ticker                TEXT    NOT NULL,
    kind                  TEXT    NOT NULL CHECK (kind IN ('stock', 'crypto')),
    company_name          TEXT,
    sector                TEXT,
    current_price         REAL,
    target_entry_low      REAL,
    target_entry_high     REAL,
    thesis_link           TEXT,
    note                  TEXT,
    added_at              INTEGER NOT NULL,                       -- unix seconds
    promoted_holding_id   INTEGER,                                 -- nullable soft pointer
    deleted_at            INTEGER,                                 -- nullable
    updated_at            INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_watchlist_ticker_active
    ON watchlist (user_id, kind, ticker)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_watchlist_added       ON watchlist (user_id, added_at DESC);
CREATE INDEX idx_watchlist_deleted_at  ON watchlist (deleted_at);

CREATE TABLE framework_scores (
    id              INTEGER PRIMARY KEY,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_kind     TEXT    NOT NULL CHECK (target_kind IN ('holding', 'watchlist')),
    target_id       INTEGER NOT NULL,
    framework_id    TEXT    NOT NULL,                              -- 'jordi' | 'cowen'
    scored_at       INTEGER NOT NULL,                              -- unix seconds
    total_score     INTEGER NOT NULL,
    max_score       INTEGER NOT NULL,
    passes          INTEGER NOT NULL DEFAULT 0,                    -- 0/1 (no native bool)
    scores_json     TEXT    NOT NULL,                              -- {question_id: {score, note}}
    tags_json       TEXT,                                          -- {tag_key: value}
    reviewer_note   TEXT
);
CREATE INDEX idx_framework_scores_target
    ON framework_scores (target_kind, target_id, scored_at DESC);
CREATE INDEX idx_framework_scores_user
    ON framework_scores (user_id, scored_at DESC);
