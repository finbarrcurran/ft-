-- 0077_visser46_partb.sql — Jordi-46 Part B: CoinGecko data for the 46-name index.
-- Everything here is new and separate: it reads crypto_index_members and writes price_history (kind
-- crypto/stock) and the two new tables below. Nothing touches the watchlist, stock_holdings,
-- crypto_holdings, theses, alerts or entry-zone logic, and Part B raises no alert on prices.

-- The kill switch and the soft stop (user_preferences). The switch ships OFF: Fin turns it on after the dry run.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
  ('coingecko_partb_enabled',   'off',  strftime('%s','now')),
  ('coingecko_partb_soft_stop', '7000', strftime('%s','now'));

-- Every CoinGecko call, per endpoint (markets, market_chart, global, simple_price, other), keyed or keyless.
-- A call is counted when it is made, so a 429 and its retry are two rows' worth.
CREATE TABLE IF NOT EXISTS coingecko_calls (
  day       TEXT    NOT NULL,                     -- UTC 'YYYY-MM-DD'
  endpoint  TEXT    NOT NULL,
  feature   TEXT    NOT NULL DEFAULT '',          -- '' = existing features; 'partb_refresh', 'partb_backfill' = Part B
  keyed     INTEGER NOT NULL CHECK (keyed IN (0,1)),
  calls     INTEGER NOT NULL DEFAULT 0,
  ok        INTEGER NOT NULL DEFAULT 0,
  fail      INTEGER NOT NULL DEFAULT 0,
  fail_429  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, endpoint, feature, keyed)
);

-- Latest /coins/markets snapshot per token of the index (one row per coin, replaced every 30 minutes).
CREATE TABLE IF NOT EXISTS crypto_index_snapshot (
  coingecko_id  TEXT    PRIMARY KEY,
  symbol        TEXT    NOT NULL,
  fetched_at    INTEGER NOT NULL,                 -- unix seconds, when FT made the call
  price_usd     REAL,
  market_cap    REAL,
  mcap_rank     INTEGER,
  chg_24h       REAL,
  chg_7d        REAL,
  chg_30d       REAL
);
