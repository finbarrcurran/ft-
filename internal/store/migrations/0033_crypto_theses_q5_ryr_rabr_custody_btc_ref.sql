-- 0033_crypto_theses_q5_ryr_rabr_custody_btc_ref.sql
--
-- Spec 9l v0.4 §B + v0.5 §H + §L.6 + DePIN adapter §3 + RWA adapter §3.
--
-- Five doctrinal items in one migration, designed against real seeded
-- Batch 3a data (AAVE/RNDR/BUIDL) per Migration_0033_Doctrine_Handoff.md:
--
--   Item 1: Q5 mechanism enum extension (7 → 14 values)
--   Item 2: DePIN RYR cross-pillar fields (4 columns)
--   Item 3: RWA RABR cross-pillar fields (5 columns)
--   Item 4: RWA Custody Verification Tier (1 column + 2 informational)
--   Item 5: BTC `btc_beta` enum extension (`reference` value)
--
-- SQLite CHECK constraint replacement requires table rebuild. Items 1
-- and 5 are both CHECK changes on `crypto_theses` — folded into a
-- single rebuild that also pre-includes all new columns from items
-- 2/3/4. Cleaner than rebuild + 13 separate ALTERs.
--
-- Re-tag UPDATEs and data populates are NOT in this file — they are
-- performed by a separate post-migration step (cmd/ft-apply-0033)
-- after the migration runner reports clean apply.

PRAGMA foreign_keys=OFF;

BEGIN TRANSACTION;

-- ============================================================
-- Items 1 + 2 + 3 + 4 + 5 — table rebuild with full new schema
-- ============================================================
CREATE TABLE crypto_theses_new (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    coin_symbol                 TEXT NOT NULL,
    coin_name                   TEXT NOT NULL,
    coingecko_id                TEXT,
    primary_adapter_id          INTEGER NOT NULL REFERENCES crypto_adapters(id),
    secondary_adapter_id        INTEGER REFERENCES crypto_adapters(id),
    scorecard_type              TEXT NOT NULL CHECK (scorecard_type IN ('alt_18','monetary_12')),
    pillar_scores_json          TEXT NOT NULL,
    total_score                 INTEGER NOT NULL,
    max_score                   INTEGER NOT NULL,
    band                        TEXT NOT NULL CHECK (band IN ('strong','accumulate','hold','trim','exit')),
    pillar_pass_gate_failed     INTEGER NOT NULL DEFAULT 0,

    -- Item 1: Q5 mechanism enum 7→14 values
    q5_mechanism                TEXT CHECK (q5_mechanism IN (
        -- Original v0.1 set:
        'fee_burn','fee_share','buyback','staking_yield',
        'governance_only','none','other',
        -- 0033 additions per v0.4 §B + v0.5 §H:
        'direct_asset_claim',          -- RWA — 1:1 backed claim on underlying asset (BUIDL)
        'required_for_service',        -- Infrastructure — token required for service (LINK, EIGEN, GRT)
        'dsr_surplus',                 -- DeFi stablecoin-issuer — DSR + surplus auctions (Sky/MKR)
        'burn_and_mint',               -- DePIN/Speculative — BME or transaction-tax burn (RNDR, LUNC)
        'buyback_stake',               -- DeFi/Infrastructure — buyback to treasury, not burn
        'real_yield_staking',          -- Distinct from staking_yield — paid from real fees (AAVE)
        'governance_with_fee_switch'   -- UNI-class — governance controls dormant fee switch
    ) OR q5_mechanism IS NULL),
    q5_annual_usd               REAL,
    q5_fdv_usd                  REAL,
    q5_accrual_pct              REAL,

    q9_team_note                TEXT,
    active_veto                 TEXT,
    active_veto_reason          TEXT,
    veto_tripped_at             INTEGER,
    catalyst_date               TEXT,
    catalyst_note               TEXT,
    holding_horizon             TEXT NOT NULL CHECK (holding_horizon IN ('never_sell','cycle','multi_year','medium','trade','tbd')),

    -- Item 5: BTC `btc_beta` enum 4→5 values (`reference` added)
    btc_beta                    TEXT NOT NULL CHECK (btc_beta IN ('high','medium','low','inverse','reference')),

    secondary_tags_json         TEXT NOT NULL DEFAULT '[]',
    liquidity_passed            INTEGER NOT NULL DEFAULT 0,
    liquidity_venues_json       TEXT NOT NULL DEFAULT '[]',
    liquidity_checked_at        INTEGER,
    status                      TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','locked','needs-review','watching','invalidated','forked')),
    version                     TEXT NOT NULL DEFAULT 'v1',
    markdown_current            TEXT NOT NULL,
    rendered_html               TEXT NOT NULL DEFAULT '',
    github_path                 TEXT,
    github_url                  TEXT,
    file_sha                    TEXT,
    locked_at                   INTEGER,
    last_reviewed_at            INTEGER,
    next_review_at              INTEGER,
    created_at                  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at                  INTEGER NOT NULL DEFAULT (strftime('%s','now')),

    -- Item 2: DePIN RYR cross-pillar fields (nullable; mandatory at thesis lock for DePIN adapter)
    q4_q5_ryr                   REAL,
    q5_paid_revenue_usd         REAL,
    q5_emissions_usd            REAL,
    network_age_months          INTEGER,

    -- Item 3: RWA RABR cross-pillar fields (nullable; mandatory at thesis lock for RWA adapter)
    q5_rabr                     REAL,
    q5_verified_asset_value_usd REAL,
    q5_token_supply_at_par_usd  REAL,
    q5_audit_date               TEXT,
    q5_auditor                  TEXT,

    -- Item 4: RWA Custody Verification Tier (nullable; mandatory for RWA adapter)
    q6_custody_tier             TEXT CHECK (q6_custody_tier IN ('tier_1','tier_2','tier_3') OR q6_custody_tier IS NULL),
    q6_custody_cadence          TEXT,
    q6_custody_jurisdiction     TEXT,

    UNIQUE(coin_symbol, version)
);

-- Copy existing rows (NULL for new columns)
INSERT INTO crypto_theses_new (
    id, coin_symbol, coin_name, coingecko_id,
    primary_adapter_id, secondary_adapter_id, scorecard_type,
    pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
    q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
    q9_team_note, active_veto, active_veto_reason, veto_tripped_at,
    catalyst_date, catalyst_note,
    holding_horizon, btc_beta, secondary_tags_json,
    liquidity_passed, liquidity_venues_json, liquidity_checked_at,
    status, version, markdown_current, rendered_html,
    github_path, github_url, file_sha,
    locked_at, last_reviewed_at, next_review_at,
    created_at, updated_at
)
SELECT
    id, coin_symbol, coin_name, coingecko_id,
    primary_adapter_id, secondary_adapter_id, scorecard_type,
    pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
    q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
    q9_team_note, active_veto, active_veto_reason, veto_tripped_at,
    catalyst_date, catalyst_note,
    holding_horizon, btc_beta, secondary_tags_json,
    liquidity_passed, liquidity_venues_json, liquidity_checked_at,
    status, version, markdown_current, rendered_html,
    github_path, github_url, file_sha,
    locked_at, last_reviewed_at, next_review_at,
    created_at, updated_at
  FROM crypto_theses;

DROP TABLE crypto_theses;
ALTER TABLE crypto_theses_new RENAME TO crypto_theses;

-- Re-create indexes (preserves performance characteristics from 0031)
CREATE INDEX idx_crypto_theses_symbol          ON crypto_theses(coin_symbol);
CREATE INDEX idx_crypto_theses_status          ON crypto_theses(status);
CREATE INDEX idx_crypto_theses_band            ON crypto_theses(band);
CREATE INDEX idx_crypto_theses_horizon         ON crypto_theses(holding_horizon);
CREATE INDEX idx_crypto_theses_btc_beta        ON crypto_theses(btc_beta);
CREATE INDEX idx_crypto_theses_primary_adapter ON crypto_theses(primary_adapter_id);
CREATE INDEX idx_crypto_theses_next_review     ON crypto_theses(next_review_at);
CREATE INDEX idx_crypto_theses_veto            ON crypto_theses(active_veto) WHERE active_veto IS NOT NULL;

-- Re-create Speculative horizon triggers from 0032
-- (Triggers attach to the table; dropping the table drops them too)
CREATE TRIGGER trg_speculative_horizon_insert
BEFORE INSERT ON crypto_theses
FOR EACH ROW
WHEN NEW.holding_horizon IN ('never_sell','cycle','multi_year')
 AND EXISTS (
       SELECT 1 FROM crypto_adapters
       WHERE id = NEW.primary_adapter_id
         AND adapter_type = 'speculative'
     )
BEGIN
    SELECT RAISE(ABORT,
      'Spec 9l: Speculative theses cannot be locked at Never-Sell, Cycle, or Multi-year horizon. Allowed: Trade or Medium.');
END;

CREATE TRIGGER trg_speculative_horizon_update
BEFORE UPDATE OF primary_adapter_id, holding_horizon ON crypto_theses
FOR EACH ROW
WHEN NEW.holding_horizon IN ('never_sell','cycle','multi_year')
 AND EXISTS (
       SELECT 1 FROM crypto_adapters
       WHERE id = NEW.primary_adapter_id
         AND adapter_type = 'speculative'
     )
BEGIN
    SELECT RAISE(ABORT,
      'Spec 9l: Speculative theses cannot be moved to Never-Sell, Cycle, or Multi-year horizon. Allowed: Trade or Medium.');
END;

COMMIT;

PRAGMA foreign_keys=ON;
