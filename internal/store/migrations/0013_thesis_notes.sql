-- 0013_thesis_notes.sql — Spec 11 D1.
--
-- Append-only prose observation log linked to holdings or watchlist
-- entries. Closes the loop between news/events and framework re-scoring:
-- observation → note (optionally factor-tagged) → next re-score sees the
-- flag (Spec 11 D7).
--
-- Append-only invariant: v1 allows UPDATE for typo correction within 24h
-- (Spec 11 risk note), but the row is never physically removed —
-- soft-delete via deleted_at.

CREATE TABLE thesis_notes (
    id                INTEGER PRIMARY KEY,

    -- Linkage (one of holding | watchlist)
    target_kind       TEXT    NOT NULL CHECK (target_kind IN ('holding', 'watchlist')),
    target_id         INTEGER NOT NULL,
    ticker            TEXT    NOT NULL,                                  -- denormalized for query speed

    -- Content
    observation_at    TEXT    NOT NULL,                                  -- ISO YYYY-MM-DD; when the observation occurred
    observation_text  TEXT    NOT NULL,

    -- Optional factor tagging (cascading: framework_id picks the question set)
    framework_id      TEXT,                                              -- 'jordi' | 'cowen' | 'percoco' | NULL
    factor_id         TEXT,                                              -- question_id within the framework | NULL
    factor_direction  TEXT    CHECK (factor_direction IN ('confirms', 'contradicts', 'neutral')),

    -- Source
    source_url        TEXT,
    source_kind       TEXT    CHECK (source_kind IN (
                        'news', 'earnings', 'youtube', 'twitter',
                        'manual', 'cowen_weekly', 'other'
                      )),

    -- Provenance
    created_at        INTEGER NOT NULL,                                  -- unix seconds
    updated_at        INTEGER NOT NULL,                                  -- unix seconds; bumped on edit
    deleted_at        INTEGER                                            -- unix seconds; soft-delete
);
CREATE INDEX idx_notes_target  ON thesis_notes (target_kind, target_id, observation_at DESC);
CREATE INDEX idx_notes_ticker  ON thesis_notes (ticker, observation_at DESC);
CREATE INDEX idx_notes_factor  ON thesis_notes (framework_id, factor_id, observation_at DESC);
CREATE INDEX idx_notes_active  ON thesis_notes (target_kind, target_id) WHERE deleted_at IS NULL;
