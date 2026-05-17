-- 0021_holding_theses.sql — Spec 14 D1.
--
-- Per-holding long-form thesis. One row per (holding_kind, holding_id);
-- markdown body + version + status + append-only history. Mirrors the
-- shape of sector_scorecards but keyed on the holding instead of a
-- sector code.
--
-- The existing thesis_link column on stock_holdings + crypto_holdings
-- (Spec 10) and the thesis_notes table (Spec 11) stay as-is. v1
-- distinction:
--   thesis_link   — external URL (Notion, Google Doc) — DEPRECATED but kept
--   thesis_notes  — short observations linked to factors
--   holding_theses (this) — the in-app long-form prose

CREATE TABLE holding_theses (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    holding_kind        TEXT NOT NULL CHECK (holding_kind IN ('stock', 'crypto')),
    holding_id          INTEGER NOT NULL,
    ticker              TEXT NOT NULL,                                   -- denormalized for query speed
    current_version     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'locked', 'needs-review')),
    markdown_current    TEXT NOT NULL,
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(holding_kind, holding_id)
);

CREATE INDEX idx_holding_theses_ticker ON holding_theses(ticker);

CREATE TABLE holding_thesis_versions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    thesis_id       INTEGER NOT NULL REFERENCES holding_theses(id),
    version         TEXT NOT NULL,
    markdown        TEXT NOT NULL,
    changelog_note  TEXT,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(thesis_id, version)
);

CREATE INDEX idx_holding_thesis_versions ON holding_thesis_versions(thesis_id, created_at DESC);
