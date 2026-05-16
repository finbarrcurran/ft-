-- 0009_percoco_execution.sql — Spec 9c D1.
--
-- Largest single migration in the build. Adds the Percoco execution-layer
-- columns + supporting tables. All additive (no DROP, no UPDATE on existing
-- data, no constraint changes on existing columns) so rollback is safe.
--
-- Notes:
--   * SQLite ALTER TABLE ADD COLUMN doesn't reliably accept CHECK
--     constraints; we enforce setup_type / stage values in app code.
--   * `vol_tier_auto` is separate from existing crypto `vol_tier` (Spec 3)
--     so user override and computed value coexist.
--   * portfolio_value_history uses the date as primary key — one row per
--     day, INSERT OR REPLACE on the nightly snapshot job.
--   * sr_candidates regenerates fully nightly, so the table is small
--     (~36 holdings × 6 candidates = ~220 rows steady-state).

-- ----- stock_holdings extensions ----------------------------------------
ALTER TABLE stock_holdings ADD COLUMN support_1 REAL;
ALTER TABLE stock_holdings ADD COLUMN support_2 REAL;
ALTER TABLE stock_holdings ADD COLUMN resistance_1 REAL;
ALTER TABLE stock_holdings ADD COLUMN resistance_2 REAL;
ALTER TABLE stock_holdings ADD COLUMN atr_weekly REAL;
ALTER TABLE stock_holdings ADD COLUMN vol_tier_auto TEXT;
ALTER TABLE stock_holdings ADD COLUMN setup_type TEXT;            -- 'A_breakout_retest' | 'B_support_bounce' | 'C_continuation'
ALTER TABLE stock_holdings ADD COLUMN stage TEXT NOT NULL DEFAULT 'pre_tp1';
ALTER TABLE stock_holdings ADD COLUMN tp1_hit_at INTEGER;         -- unix seconds
ALTER TABLE stock_holdings ADD COLUMN tp2_hit_at INTEGER;         -- unix seconds
ALTER TABLE stock_holdings ADD COLUMN time_stop_review_at TEXT;   -- ISO YYYY-MM-DD

-- ----- crypto_holdings extensions ---------------------------------------
ALTER TABLE crypto_holdings ADD COLUMN support_1 REAL;
ALTER TABLE crypto_holdings ADD COLUMN support_2 REAL;
ALTER TABLE crypto_holdings ADD COLUMN resistance_1 REAL;
ALTER TABLE crypto_holdings ADD COLUMN resistance_2 REAL;
ALTER TABLE crypto_holdings ADD COLUMN atr_weekly REAL;
ALTER TABLE crypto_holdings ADD COLUMN vol_tier_auto TEXT;        -- crypto already has vol_tier (manual); this is the auto-classified value
ALTER TABLE crypto_holdings ADD COLUMN setup_type TEXT;
ALTER TABLE crypto_holdings ADD COLUMN stage TEXT NOT NULL DEFAULT 'pre_tp1';
ALTER TABLE crypto_holdings ADD COLUMN tp1_hit_at INTEGER;
ALTER TABLE crypto_holdings ADD COLUMN tp2_hit_at INTEGER;
ALTER TABLE crypto_holdings ADD COLUMN time_stop_review_at TEXT;

-- ----- watchlist extensions ---------------------------------------------
-- Watchlist gets the level fields (so a candidate trade can be priced
-- before promotion) but not the stage/tp_hit fields (those only matter
-- once promoted to a holding).
ALTER TABLE watchlist ADD COLUMN support_1 REAL;
ALTER TABLE watchlist ADD COLUMN support_2 REAL;
ALTER TABLE watchlist ADD COLUMN resistance_1 REAL;
ALTER TABLE watchlist ADD COLUMN resistance_2 REAL;
ALTER TABLE watchlist ADD COLUMN atr_weekly REAL;
ALTER TABLE watchlist ADD COLUMN vol_tier_auto TEXT;
ALTER TABLE watchlist ADD COLUMN setup_type TEXT;

-- ----- portfolio_value_history -----------------------------------------
-- One row per UTC date. Used for drawdown computation + equity curve.
-- Daily snapshot writes INSERT OR REPLACE so re-running the cron is idempotent.
CREATE TABLE portfolio_value_history (
    date              TEXT PRIMARY KEY,         -- 'YYYY-MM-DD'
    total_value_usd   REAL NOT NULL,
    stocks_value_usd  REAL NOT NULL,
    crypto_value_usd  REAL NOT NULL,
    computed_at       INTEGER NOT NULL          -- unix seconds
);
CREATE INDEX idx_pvh_date ON portfolio_value_history (date DESC);

-- ----- sr_candidates (regenerated nightly) ------------------------------
-- One row per (ticker, kind, level_type, price-cluster-centroid).
-- Touches = number of weekly pivots in the cluster. Score = ranking metric.
CREATE TABLE sr_candidates (
    ticker         TEXT    NOT NULL,
    kind           TEXT    NOT NULL,                  -- 'stock' | 'crypto'
    level_type     TEXT    NOT NULL,                  -- 'support' | 'resistance'
    price          REAL    NOT NULL,                  -- cluster centroid
    touches        INTEGER NOT NULL,
    last_touch_at  TEXT    NOT NULL,                  -- ISO YYYY-MM-DD
    score          REAL    NOT NULL,                  -- ranking, higher = better
    computed_at    INTEGER NOT NULL,                  -- unix seconds
    PRIMARY KEY (ticker, kind, level_type, price)
);
CREATE INDEX idx_sr_candidates_lookup ON sr_candidates (ticker, kind, level_type, score DESC);

-- ----- weekly_bars cache (regenerated nightly from price_history) -------
-- Stores Open / High / Low / Close per weekly bar so ATR + S/R don't have
-- to re-aggregate daily closes on every read. ISO week key + ticker.
CREATE TABLE weekly_bars (
    ticker      TEXT NOT NULL,
    kind        TEXT NOT NULL,
    week_start  TEXT NOT NULL,                        -- 'YYYY-MM-DD' Monday of that week (UTC)
    open        REAL NOT NULL,
    high        REAL NOT NULL,
    low         REAL NOT NULL,
    close       REAL NOT NULL,
    PRIMARY KEY (ticker, kind, week_start)
);
CREATE INDEX idx_weekly_bars_lookup ON weekly_bars (ticker, kind, week_start DESC);

-- ----- daily_bars cache (raw OHLC from providers) -----------------------
-- price_history (Spec 3 D8) only stores closes. ATR needs HL data, so we
-- add a second store for full OHLC. Populated by `ft backfill-bars` + the
-- existing daily 04:00 UTC cron extended.
CREATE TABLE daily_bars (
    ticker  TEXT NOT NULL,
    kind    TEXT NOT NULL,
    date    TEXT NOT NULL,                            -- ISO YYYY-MM-DD
    open    REAL NOT NULL,
    high    REAL NOT NULL,
    low     REAL NOT NULL,
    close   REAL NOT NULL,
    volume  REAL,                                     -- optional
    PRIMARY KEY (ticker, kind, date)
);
CREATE INDEX idx_daily_bars_lookup ON daily_bars (ticker, kind, date DESC);

-- ----- user_preferences seeds for risk caps -----------------------------
-- All values stored as TEXT (the preference store is generic key/value);
-- handlers parse to numbers as needed. INSERT OR IGNORE so re-running the
-- migration doesn't overwrite user-tuned values.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
    ('risk_concentration_cap_pct',       '15',    strftime('%s','now')),
    ('risk_theme_concentration_cap_pct', '30',    strftime('%s','now')),
    ('risk_total_active_cap_pct',        '8',     strftime('%s','now')),
    ('risk_drawdown_circuit_breaker_pct','10',    strftime('%s','now')),
    ('risk_per_trade_default_pct',       '1',     strftime('%s','now')),
    ('risk_per_trade_max_pct',           '2',     strftime('%s','now')),
    ('risk_circuit_breaker_active',      'false', strftime('%s','now')),
    ('risk_circuit_breaker_until',       '',      strftime('%s','now'));
