-- Spec 9e Phase 1 — Crypto Indicators Tab
--
-- Adds the schema that powers the new Crypto Indicators tab:
-- 12 indicators across 4 buckets (Cowen / Pal / Universal / Sentiment)
-- aggregated into a -100 to +100 composite score with action bands.
--
-- Phase 1 = schema + scoring engine + tab shell + F&G move.
-- Phase 2 (data providers, daily snapshot cron) follows in v1.8.1.
-- Phase 3 (gauge SVG, bucket cards, Top/Worst) in v1.8.2.

-- One row per indicator. Latest reading + score lives here for fast read.
CREATE TABLE IF NOT EXISTS crypto_indicators (
    id              TEXT PRIMARY KEY,
    bucket          TEXT NOT NULL CHECK (bucket IN ('cowen','pal','universal','sentiment')),
    display_name    TEXT NOT NULL,
    unit            TEXT,
    source          TEXT NOT NULL,
    current_value   REAL,
    current_score   REAL,
    trend_4w        REAL,
    updated_at      INTEGER,
    fetch_error     TEXT
);

-- Append-only daily snapshot of every indicator + composite for backtest.
CREATE TABLE IF NOT EXISTS crypto_indicator_snapshots (
    snapshot_date   TEXT NOT NULL,
    indicator_id    TEXT NOT NULL,
    raw_value       REAL,
    score           REAL,
    PRIMARY KEY (snapshot_date, indicator_id)
);

-- Per-snapshot composite + bucket sub-scores + action band + BTC price.
CREATE TABLE IF NOT EXISTS crypto_composite_snapshots (
    snapshot_date       TEXT PRIMARY KEY,
    composite_score     REAL NOT NULL,
    cowen_subscore      REAL,
    pal_subscore        REAL,
    universal_subscore  REAL,
    sentiment_subscore  REAL,
    action_band         TEXT NOT NULL CHECK (action_band IN
                          ('strong_accumulate','accumulate','neutral','caution','distribute_wait')),
    btc_price_usd       REAL,
    notes               TEXT
);

-- Weights config (one row, edited via SQL for v1; Settings UI in Phase 2).
CREATE TABLE IF NOT EXISTS crypto_indicator_weights (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    cowen_weight        REAL NOT NULL DEFAULT 0.25,
    pal_weight          REAL NOT NULL DEFAULT 0.20,
    universal_weight    REAL NOT NULL DEFAULT 0.35,
    sentiment_weight    REAL NOT NULL DEFAULT 0.20,
    updated_at          INTEGER NOT NULL
);

INSERT OR IGNORE INTO crypto_indicator_weights
    (id, cowen_weight, pal_weight, universal_weight, sentiment_weight, updated_at)
VALUES (1, 0.25, 0.20, 0.35, 0.20, strftime('%s','now'));

-- Manual override for Cowen Risk Indicator. Most recent row wins; staleness
-- (default 14 days) checked at read time. Older rows kept for audit.
CREATE TABLE IF NOT EXISTS cowen_risk_manual (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    risk_value    REAL NOT NULL CHECK (risk_value >= 0 AND risk_value <= 1),
    entered_at    INTEGER NOT NULL,
    source_note   TEXT
);

CREATE INDEX IF NOT EXISTS idx_crypto_indicator_snapshots_date
    ON crypto_indicator_snapshots(snapshot_date DESC);
CREATE INDEX IF NOT EXISTS idx_crypto_composite_snapshots_date
    ON crypto_composite_snapshots(snapshot_date DESC);

-- Seed the 12 Phase 1 indicators. Buckets per Spec 9e §D3.
INSERT OR IGNORE INTO crypto_indicators (id, bucket, display_name, unit, source) VALUES
    ('cowen_price_vs_200wma',     'cowen',     'Price vs 200-week MA',                 'ratio',         'computed'),
    ('cowen_log_band',            'cowen',     'Log regression band position',         'third',         'computed'),
    ('cowen_risk_indicator',      'cowen',     'Cowen Risk Indicator (proxy or manual)', '0-1',         'computed'),
    ('cowen_btc_dominance',       'cowen',     'BTC Dominance',                        '%',             'coingecko'),
    ('cowen_eth_btc',             'cowen',     'ETH/BTC ratio',                        'ratio',         'coingecko'),
    ('pal_dxy',                   'pal',       'DXY (Dollar Index)',                   'index',         'fred'),
    ('pal_us2y',                  'pal',       'US 2-Year Yield',                      '%',             'fred'),
    ('pal_ism',                   'pal',       'ISM Manufacturing PMI',                'index',         'manual_json'),
    ('universal_etf_flow_7d',     'universal', 'Spot BTC ETF Net Flow (7d rolling)',   'USD millions',  'farside'),
    ('universal_stablecoin_supply','universal','Stablecoin Total Supply (4-wk ROC)',   '%',             'defillama'),
    ('sentiment_fear_greed',      'sentiment', 'Crypto Fear & Greed',                  '0-100',         'alternative_me');
