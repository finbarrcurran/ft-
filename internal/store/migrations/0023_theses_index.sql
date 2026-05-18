-- Spec 15 — Thesis Library
--
-- Indexes every locked thesis MD file in the cross_sector_research GitHub
-- repo so FT can render a sortable/filterable table without re-parsing
-- markdown on every request. Populated by the theses sync engine
-- (internal/theses/sync.go) which pulls the repo periodically (every 5 min
-- via cron) and re-parses changed files.
--
-- GitHub remains the source of truth. This table is a pure cache:
--   - Deletes are safe (next sync rebuilds rows from the filesystem)
--   - The `markdown_content` + `rendered_html` columns are denormalised so
--     the Theses tab can render a thesis inline without a second fetch
--   - `revision_needed` is computed by the sync engine, not a trigger,
--     because it depends on stock_holdings.earnings_date which changes
--     independently

CREATE TABLE IF NOT EXISTS theses_index (
    id                  INTEGER PRIMARY KEY,
    ticker              TEXT    NOT NULL,
    company_name        TEXT,
    adapter             TEXT    NOT NULL,        -- 'pharma' | 'ai_infra_semi' | 'hydrocarbons' | 'energy_power' | 'defense' | 'mining_metals' | 'industrial_electrical' | 'cloud_infra'
    sub_type            TEXT,                    -- e.g. 'metabolic-obesity', 'diversified-immunology'
    score               INTEGER,                 -- 0-16 (NULL if score not parseable)
    max_score           INTEGER NOT NULL DEFAULT 16,
    version             INTEGER NOT NULL DEFAULT 1,
    status              TEXT    NOT NULL DEFAULT 'locked',  -- 'locked' | 'draft' | 'superseded'
    locked_date         TEXT,                    -- ISO YYYY-MM-DD parsed from MD header
    github_path         TEXT    NOT NULL,        -- 'theses/pharma/LLY_v1_locked.md'
    github_url          TEXT    NOT NULL,        -- canonical https blob URL
    markdown_content    TEXT    NOT NULL,        -- raw MD body
    rendered_html       TEXT    NOT NULL,        -- pre-rendered HTML (goldmark + bluemonday)
    file_sha            TEXT    NOT NULL,        -- git blob SHA — used to skip unchanged files on sync
    -- Spec 15 Phase 2 — earnings revision trigger
    next_earnings_date  TEXT,                    -- ISO YYYY-MM-DD, joined from stock_holdings.earnings_date
    earnings_urgency    TEXT    NOT NULL DEFAULT 'none',  -- 'none' | 'amber' (≤14d) | 'red' (≤3d) | 'revision_needed' (past earnings && earnings > locked_date)
    -- Bookkeeping
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    UNIQUE (ticker, version)
);

CREATE INDEX IF NOT EXISTS idx_theses_index_ticker          ON theses_index(ticker);
CREATE INDEX IF NOT EXISTS idx_theses_index_adapter         ON theses_index(adapter);
CREATE INDEX IF NOT EXISTS idx_theses_index_score           ON theses_index(score);
CREATE INDEX IF NOT EXISTS idx_theses_index_earnings_urgency ON theses_index(earnings_urgency);
