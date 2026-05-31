-- Migration 0035 — relax stock_holdings.sector_adapter_subtype CHECK constraint
--
-- Migration 0034 added the column with CHECK `LIKE '%-%:%'` requiring a hyphen
-- before the colon. That pattern rejects single-word adapter slugs like
-- `pharma:glp1-pure-play`, `defense:prime-european-rearm`, `hydrocarbons:integrated-major`.
-- (Confirmed by `pharma:glp1-pure-play` failing on NVO re-tag during initial
-- application.)
--
-- This migration relaxes the CHECK to simply require a colon separator when
-- non-null. Downstream code remains the canonical validator for adapter-slug
-- + sub-type identity.
--
-- SQLite doesn't support ALTER COLUMN to change CHECK constraints, so we
-- rebuild the table. All existing data preserved; index 0034 re-created
-- post-rebuild.

PRAGMA foreign_keys = OFF;

BEGIN TRANSACTION;

-- 1. Snapshot existing stock_holdings into a temp shape with looser CHECK
CREATE TABLE stock_holdings_new (
    id                       INTEGER PRIMARY KEY,
    user_id                  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                     TEXT    NOT NULL,
    ticker                   TEXT,
    category                 TEXT,
    invested_usd             REAL    NOT NULL DEFAULT 0,
    avg_open_price           REAL,
    current_price            REAL,
    stop_loss                REAL,
    take_profit              REAL,
    rsi14                    REAL,
    ma50                     REAL,
    ma200                    REAL,
    golden_cross             INTEGER,
    support                  REAL,
    resistance               REAL,
    analyst_target           REAL,
    proposed_entry           REAL,
    technical_setup          TEXT    NOT NULL DEFAULT '',
    analyst_rr_view          TEXT    NOT NULL DEFAULT '',
    strategy_note            TEXT    NOT NULL DEFAULT '',
    sector                   TEXT,
    updated_at               INTEGER NOT NULL,
    daily_change_pct         REAL,
    note                     TEXT,
    deleted_at               INTEGER,
    beta                     REAL,
    earnings_date            TEXT,
    ex_dividend_date         TEXT,
    exchange_override        TEXT,
    support_1                REAL,
    support_2                REAL,
    resistance_1             REAL,
    resistance_2             REAL,
    atr_weekly               REAL,
    vol_tier_auto            TEXT,
    setup_type               TEXT,
    stage                    TEXT NOT NULL DEFAULT 'pre_tp1',
    tp1_hit_at               INTEGER,
    tp2_hit_at               INTEGER,
    time_stop_review_at      TEXT,
    thesis_link              TEXT,
    realized_pnl_usd         REAL NOT NULL DEFAULT 0,
    volatility_12m_pct       REAL,
    forecast_low             REAL,
    forecast_mean            REAL,
    forecast_high            REAL,
    forecast_fetched_at      INTEGER,
    currency                 TEXT,
    sector_universe_id       INTEGER NULL REFERENCES sector_universe(id),
    sector_adapter_subtype   TEXT NULL
        CHECK (sector_adapter_subtype IS NULL OR sector_adapter_subtype LIKE '%:%')
);

-- 2. Copy all data
INSERT INTO stock_holdings_new
SELECT id, user_id, name, ticker, category, invested_usd, avg_open_price,
       current_price, stop_loss, take_profit, rsi14, ma50, ma200, golden_cross,
       support, resistance, analyst_target, proposed_entry, technical_setup,
       analyst_rr_view, strategy_note, sector, updated_at, daily_change_pct,
       note, deleted_at, beta, earnings_date, ex_dividend_date,
       exchange_override, support_1, support_2, resistance_1, resistance_2,
       atr_weekly, vol_tier_auto, setup_type, stage, tp1_hit_at, tp2_hit_at,
       time_stop_review_at, thesis_link, realized_pnl_usd, volatility_12m_pct,
       forecast_low, forecast_mean, forecast_high, forecast_fetched_at,
       currency, sector_universe_id, sector_adapter_subtype
  FROM stock_holdings;

-- 3. Drop the strict-CHECK table + rename
DROP TABLE stock_holdings;
ALTER TABLE stock_holdings_new RENAME TO stock_holdings;

-- 4. Re-create indexes from 0001 + 0034
CREATE INDEX idx_stock_holdings_user        ON stock_holdings(user_id);
CREATE INDEX idx_stock_holdings_user_ticker ON stock_holdings(user_id, ticker);
CREATE INDEX idx_stock_holdings_sector_adapter_subtype
  ON stock_holdings(sector_adapter_subtype)
  WHERE sector_adapter_subtype IS NOT NULL;

COMMIT;

PRAGMA foreign_keys = ON;
