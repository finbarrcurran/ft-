-- 0017_spec12_batch_c.sql — Spec 12 D4a (analyst forecasts).
--
-- Bear/Base/Bull analyst price targets, sourced from Yahoo's
-- quoteSummary?modules=financialData (targetLowPrice / targetMeanPrice /
-- targetHighPrice). Stored on both watchlist (display) and stock_holdings
-- (so the value carries through promotion to a position).
--
-- Crypto: no agreed equivalent free source — see Spec 12 OI-1. Crypto
-- columns intentionally NOT added; UI renders "—" for crypto rows.

ALTER TABLE watchlist ADD COLUMN forecast_low      REAL;
ALTER TABLE watchlist ADD COLUMN forecast_mean     REAL;
ALTER TABLE watchlist ADD COLUMN forecast_high     REAL;
ALTER TABLE watchlist ADD COLUMN forecast_fetched_at INTEGER;

ALTER TABLE stock_holdings ADD COLUMN forecast_low      REAL;
ALTER TABLE stock_holdings ADD COLUMN forecast_mean     REAL;
ALTER TABLE stock_holdings ADD COLUMN forecast_high     REAL;
ALTER TABLE stock_holdings ADD COLUMN forecast_fetched_at INTEGER;
