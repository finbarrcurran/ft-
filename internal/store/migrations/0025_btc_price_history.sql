-- v1.8.3 — BTC price history for Cowen indicators.
--
-- Stores BTC daily closes back to 2013-04-29 (CoinGecko earliest). Seeded
-- once on first refresh via CoinGecko /coins/bitcoin/market_chart?days=max
-- (~3500 rows, ~250 KB). Refreshed daily by the 00:30 UTC cron — only
-- appends rows where snapshot_date > MAX(snapshot_date) so it's idempotent.
--
-- Used by:
--   - cowen_log_band: log-linear regression on log10(price) vs log10(days)
--   - cowen_price_vs_200wma: ratio of latest close to 200-week MA
--   - cowen_risk_indicator (proxy): combines the above two
--
-- Pure cache. Can be wiped + rebuilt at any time by calling the refresher
-- with btc_price_history empty — it does a full re-seed.

CREATE TABLE IF NOT EXISTS btc_price_history (
    snapshot_date   TEXT PRIMARY KEY,    -- YYYY-MM-DD UTC
    close_usd       REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_btc_price_history_date
    ON btc_price_history(snapshot_date DESC);
