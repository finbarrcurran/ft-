-- 0031_crypto_theses.sql — Spec 9l D9.
--
-- Crypto Thesis Framework — Repository + Scoring Engine schema.
-- Parallels Spec 9g (sector_scorecards) and Spec 15 (theses_index), with
-- crypto-specific additions: structured pillar score blob (supports both
-- 9-Q /18 alts and 6-pillar /12 BTC), holding horizon, BTC-beta, primary
-- + secondary adapter pattern, VETO state, cascade dependencies.
--
-- Eight tables, all created in this single migration:
--   1. crypto_adapters              — one row per adapter type (8 in Phase 1)
--   2. crypto_adapter_versions      — append-only adapter version history
--   3. crypto_theses                — one row per locked per-coin thesis
--   4. crypto_thesis_history        — append-only rescore audit
--   5. crypto_thesis_dependencies   — cascade graph edges
--   6. cascade_events               — append-only cascade trigger audit
--   7. crypto_allocation_current    — single-row allocation panel state
--   8. crypto_allocation_history    — append-only allocation audit
--
-- Architectural notes
--   - Pillar scores live in `pillar_scores_json` (JSON TEXT). Schema differs
--     for alts (Q1..Q9) vs BTC (M1..M6); scorecard_type column gates the
--     interpretation. Derived/query-critical fields (total_score, band,
--     pillar_pass_gate_failed, active_veto, q5_*) are materialised as
--     columns so we can index and filter without JSON parsing.
--   - Q5 is stored as structured triple (mechanism enum + annual_usd +
--     fdv_usd) plus a computed accrual_pct, per Spec 9l Decision #6
--     "named mechanism + $ figure required". No free text Q5.
--   - Markdown body is stored both raw (markdown_current) and pre-rendered
--     (rendered_html) following the theses_index pattern — saves a render
--     pass on every Repository view request.
--   - All timestamps use INTEGER Unix epoch (strftime('%s','now')) to match
--     the rest of FT's schema convention.

------------------------------------------------------------------------
-- 1. crypto_adapters
------------------------------------------------------------------------
CREATE TABLE crypto_adapters (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    slug                TEXT NOT NULL UNIQUE,           -- 'btc' | 'l1' | 'l2' | 'defi' | 'infra' | 'depin' | 'rwa' | 'speculative'
    display_name        TEXT NOT NULL,                  -- 'Bitcoin (Monetary Asset)' etc.
    short_description   TEXT NOT NULL,
    adapter_type        TEXT NOT NULL CHECK (adapter_type IN ('btc','l1','l2','defi','infra','depin','rwa','speculative')),
    scorecard_type      TEXT NOT NULL CHECK (scorecard_type IN ('alt_18','monetary_12')),
    current_version     TEXT NOT NULL,                  -- e.g. 'v1', 'v1.1'
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','locked','needs-review')),
    markdown_current    TEXT NOT NULL,                  -- full adapter MD body
    rendered_html       TEXT NOT NULL DEFAULT '',       -- pre-rendered (goldmark + bluemonday)
    primary_data_sources TEXT NOT NULL DEFAULT '[]',    -- JSON array of source slugs
    kill_criteria_json  TEXT NOT NULL DEFAULT '[]',     -- JSON array of adapter-specific VETO rules
    is_doctrine         INTEGER NOT NULL DEFAULT 0,     -- 1 = doctrine (no UI edit)
    github_path         TEXT,                           -- 'crypto/adapters/BTC_adapter_v1.md' when synced from repo
    github_url          TEXT,
    file_sha            TEXT,                           -- git blob SHA for change detection
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    locked_at           INTEGER
);

CREATE INDEX idx_crypto_adapters_slug   ON crypto_adapters(slug);
CREATE INDEX idx_crypto_adapters_status ON crypto_adapters(status);

------------------------------------------------------------------------
-- 2. crypto_adapter_versions  (append-only history)
------------------------------------------------------------------------
CREATE TABLE crypto_adapter_versions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    adapter_id      INTEGER NOT NULL REFERENCES crypto_adapters(id),
    version         TEXT NOT NULL,
    markdown        TEXT NOT NULL,
    changelog_note  TEXT,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(adapter_id, version)
);

CREATE INDEX idx_crypto_adapter_versions ON crypto_adapter_versions(adapter_id, created_at DESC);

------------------------------------------------------------------------
-- 3. crypto_theses
------------------------------------------------------------------------
CREATE TABLE crypto_theses (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    coin_symbol                 TEXT NOT NULL,                       -- 'BTC', 'ETH', 'LUNC'
    coin_name                   TEXT NOT NULL,                       -- 'Bitcoin', 'Ethereum'
    coingecko_id                TEXT,                                -- 'bitcoin', 'ethereum' — null until matched
    -- Adapter assignment
    primary_adapter_id          INTEGER NOT NULL REFERENCES crypto_adapters(id),
    secondary_adapter_id        INTEGER REFERENCES crypto_adapters(id),  -- nullable, hybrid coins only
    scorecard_type              TEXT NOT NULL CHECK (scorecard_type IN ('alt_18','monetary_12')),
    -- Pillar scores
    pillar_scores_json          TEXT NOT NULL,                       -- {"Q1":2,"Q2":2,...} or {"M1":2,"M2":1,...}
    total_score                 INTEGER NOT NULL,                    -- denormalised sum for query speed
    max_score                   INTEGER NOT NULL,                    -- 18 for alts, 12 for BTC
    band                        TEXT NOT NULL CHECK (band IN ('strong','accumulate','hold','trim','exit')),
    pillar_pass_gate_failed     INTEGER NOT NULL DEFAULT 0,          -- 1 if Q1/Q2/Q6/Q9 < 1 or any pillar = 0
    -- Q5 structured (alts only; null for BTC)
    q5_mechanism                TEXT CHECK (q5_mechanism IN ('fee_burn','fee_share','buyback','staking_yield','governance_only','none','other') OR q5_mechanism IS NULL),
    q5_annual_usd               REAL,
    q5_fdv_usd                  REAL,
    q5_accrual_pct              REAL,                                -- computed: q5_annual_usd / q5_fdv_usd * 100
    -- Q9 structured (alts only)
    q9_team_note                TEXT,
    -- VETO state
    active_veto                 TEXT,                                -- null if no VETO; otherwise reason slug
    active_veto_reason          TEXT,                                -- free text detail
    veto_tripped_at             INTEGER,
    -- Q7 catalyst
    catalyst_date               TEXT,                                -- ISO YYYY-MM-DD; nullable
    catalyst_note               TEXT,
    -- Horizon, beta, tags
    holding_horizon             TEXT NOT NULL CHECK (holding_horizon IN ('never_sell','cycle','multi_year','medium','trade','tbd')),
    btc_beta                    TEXT NOT NULL CHECK (btc_beta IN ('high','medium','low','inverse')),
    secondary_tags_json         TEXT NOT NULL DEFAULT '[]',          -- JSON array of layer/narrative tags
    -- Liquidity pre-filter
    liquidity_passed            INTEGER NOT NULL DEFAULT 0,          -- 1 if listed on Kraken/Coinbase/Binance
    liquidity_venues_json       TEXT NOT NULL DEFAULT '[]',          -- JSON array of venues found
    liquidity_checked_at        INTEGER,
    -- Status + workflow
    status                      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','locked','needs-review','watching','invalidated','forked')),
    version                     TEXT NOT NULL DEFAULT 'v1',
    -- Markdown body
    markdown_current            TEXT NOT NULL,
    rendered_html               TEXT NOT NULL DEFAULT '',
    github_path                 TEXT,
    github_url                  TEXT,
    file_sha                    TEXT,
    -- Cadence bookkeeping
    locked_at                   INTEGER,
    last_reviewed_at            INTEGER,
    next_review_at              INTEGER,                              -- computed from horizon override rules
    -- Audit
    created_at                  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at                  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(coin_symbol, version)
);

CREATE INDEX idx_crypto_theses_symbol          ON crypto_theses(coin_symbol);
CREATE INDEX idx_crypto_theses_status          ON crypto_theses(status);
CREATE INDEX idx_crypto_theses_band            ON crypto_theses(band);
CREATE INDEX idx_crypto_theses_horizon         ON crypto_theses(holding_horizon);
CREATE INDEX idx_crypto_theses_btc_beta        ON crypto_theses(btc_beta);
CREATE INDEX idx_crypto_theses_primary_adapter ON crypto_theses(primary_adapter_id);
CREATE INDEX idx_crypto_theses_next_review     ON crypto_theses(next_review_at);
CREATE INDEX idx_crypto_theses_veto            ON crypto_theses(active_veto) WHERE active_veto IS NOT NULL;

------------------------------------------------------------------------
-- 4. crypto_thesis_history  (append-only rescore audit)
------------------------------------------------------------------------
CREATE TABLE crypto_thesis_history (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    thesis_id           INTEGER NOT NULL REFERENCES crypto_theses(id),
    event_type          TEXT NOT NULL CHECK (event_type IN (
        'initial_lock',
        'monthly_rescore',
        'event_rescore',
        'q7_decay',
        'override',
        'fork_rescore',
        'veto_tripped',
        'veto_resolved',
        'cascade_flagged'
    )),
    event_reason        TEXT,                            -- free text e.g. 'unlock_cliff_q2_drop'
    pillar_scores_json  TEXT NOT NULL,                   -- snapshot at this point
    total_score         INTEGER NOT NULL,
    band                TEXT NOT NULL CHECK (band IN ('strong','accumulate','hold','trim','exit')),
    delta               INTEGER NOT NULL DEFAULT 0,      -- score delta vs previous
    recommended_action  TEXT CHECK (recommended_action IN ('none','trim_25','trim_50','exit','log_only','override') OR recommended_action IS NULL),
    action_taken        TEXT,                            -- what the user actually did
    override_reason     TEXT,
    triggered_by        TEXT NOT NULL CHECK (triggered_by IN ('system','user','cron','event')),
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX idx_crypto_thesis_history_thesis ON crypto_thesis_history(thesis_id, created_at DESC);
CREATE INDEX idx_crypto_thesis_history_event  ON crypto_thesis_history(event_type);

------------------------------------------------------------------------
-- 5. crypto_thesis_dependencies  (cascade graph)
------------------------------------------------------------------------
CREATE TABLE crypto_thesis_dependencies (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_thesis_id    INTEGER NOT NULL REFERENCES crypto_theses(id),
    child_thesis_id     INTEGER NOT NULL REFERENCES crypto_theses(id),
    dependency_type     TEXT NOT NULL CHECK (dependency_type IN (
        'platform_parent',
        'protocol_host',
        'oracle_dependency',
        'narrative_correlated',
        'btc_beta_implicit'
    )),
    cascade_strength    TEXT NOT NULL CHECK (cascade_strength IN ('strong','moderate','weak')),
    note                TEXT,
    created_by          TEXT NOT NULL CHECK (created_by IN ('system','user')),
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(parent_thesis_id, child_thesis_id, dependency_type),
    CHECK (parent_thesis_id != child_thesis_id)
);

CREATE INDEX idx_crypto_deps_parent ON crypto_thesis_dependencies(parent_thesis_id);
CREATE INDEX idx_crypto_deps_child  ON crypto_thesis_dependencies(child_thesis_id);
CREATE INDEX idx_crypto_deps_type   ON crypto_thesis_dependencies(dependency_type);

------------------------------------------------------------------------
-- 6. cascade_events  (append-only cascade audit)
------------------------------------------------------------------------
CREATE TABLE cascade_events (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    triggering_thesis_id    INTEGER NOT NULL REFERENCES crypto_theses(id),
    affected_thesis_id      INTEGER NOT NULL REFERENCES crypto_theses(id),
    dependency_type         TEXT NOT NULL CHECK (dependency_type IN (
        'platform_parent',
        'protocol_host',
        'oracle_dependency',
        'narrative_correlated',
        'btc_beta_implicit'
    )),
    trigger_reason          TEXT NOT NULL,                  -- e.g. 'parent_band_strong_to_trim'
    action                  TEXT NOT NULL CHECK (action IN ('flagged_needs_review','notification_only')),
    priority                TEXT NOT NULL CHECK (priority IN ('high','medium','low')),
    resolved_at             INTEGER,                        -- null until user marks resolved
    resolution_note         TEXT,
    created_at              INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX idx_cascade_events_affected   ON cascade_events(affected_thesis_id);
CREATE INDEX idx_cascade_events_triggering ON cascade_events(triggering_thesis_id);
CREATE INDEX idx_cascade_events_unresolved ON cascade_events(resolved_at) WHERE resolved_at IS NULL;

------------------------------------------------------------------------
-- 7. crypto_allocation_current  (single-row state — Spec 9l Decision #21)
------------------------------------------------------------------------
CREATE TABLE crypto_allocation_current (
    id              INTEGER PRIMARY KEY CHECK (id = 1),    -- single-row pattern
    pct_stocks      REAL NOT NULL,
    pct_btc         REAL NOT NULL,
    pct_eth         REAL NOT NULL,
    pct_alts        REAL NOT NULL,
    pct_cash        REAL NOT NULL,
    note            TEXT,
    updated_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    CHECK (pct_stocks >= 0 AND pct_stocks <= 100),
    CHECK (pct_btc    >= 0 AND pct_btc    <= 100),
    CHECK (pct_eth    >= 0 AND pct_eth    <= 100),
    CHECK (pct_alts   >= 0 AND pct_alts   <= 100),
    CHECK (pct_cash   >= 0 AND pct_cash   <= 100),
    CHECK (ABS((pct_stocks + pct_btc + pct_eth + pct_alts + pct_cash) - 100) < 0.01)
);

-- Seed a sensible default: 60/10/5/5/20 stocks/btc/eth/alts/cash
INSERT INTO crypto_allocation_current (id, pct_stocks, pct_btc, pct_eth, pct_alts, pct_cash, note)
VALUES (1, 60, 10, 5, 5, 20, 'Default allocation seeded at migration; adjust on Crypto Theses tab');

------------------------------------------------------------------------
-- 8. crypto_allocation_history  (append-only audit)
------------------------------------------------------------------------
CREATE TABLE crypto_allocation_history (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    pct_stocks      REAL NOT NULL,
    pct_btc         REAL NOT NULL,
    pct_eth         REAL NOT NULL,
    pct_alts        REAL NOT NULL,
    pct_cash        REAL NOT NULL,
    note            TEXT,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE INDEX idx_crypto_allocation_history_date ON crypto_allocation_history(created_at DESC);

-- Seed history with the initial default
INSERT INTO crypto_allocation_history (pct_stocks, pct_btc, pct_eth, pct_alts, pct_cash, note)
VALUES (60, 10, 5, 5, 20, 'Initial seed at migration 0031');
