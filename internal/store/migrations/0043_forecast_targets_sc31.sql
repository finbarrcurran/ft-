-- Migration 0043 — SC-31: forecast-target enrichment + manual override.
--
-- Spec 12 D4a already stored Yahoo's Bear/Base/Bull consensus
-- (forecast_low/mean/high + forecast_fetched_at) on both watchlist and
-- stock_holdings, auto-populated by the 04:00 UTC RunDailyJob. SC-31 adds:
--
--   forecast_median        — Yahoo targetMedianPrice (more robust to a single
--                            outlier than the mean; stored as enrichment, the
--                            displayed "base" stays mean by default, D31.5).
--   forecast_analyst_count — Yahoo numberOfAnalystOpinions. Gates "target
--                            present": a target with 0 analysts is not treated
--                            as covered (snag S-31c).
--   forecast_source        — 'yahoo' (auto, default) | 'manual'. Mirrors the
--                            migration 0036 SL-method "manual-override-wins"
--                            precedent: when 'manual', the daily job SKIPS the
--                            forecast write for that row so a hand-entered
--                            target (SMR / DJT / RKLB, which Yahoo doesn't
--                            cover) survives the next 04:00 run (AC-3 / S-31a).
--
-- The manual-skip is enforced in the store writers (SetStockForecast /
-- SetWatchlistForecast) via `AND COALESCE(forecast_source,'yahoo') <> 'manual'`.

ALTER TABLE watchlist ADD COLUMN forecast_median        REAL;
ALTER TABLE watchlist ADD COLUMN forecast_analyst_count INTEGER;
ALTER TABLE watchlist ADD COLUMN forecast_source        TEXT NOT NULL DEFAULT 'yahoo'
  CHECK (forecast_source IN ('yahoo','manual'));

ALTER TABLE stock_holdings ADD COLUMN forecast_median        REAL;
ALTER TABLE stock_holdings ADD COLUMN forecast_analyst_count INTEGER;
ALTER TABLE stock_holdings ADD COLUMN forecast_source        TEXT NOT NULL DEFAULT 'yahoo'
  CHECK (forecast_source IN ('yahoo','manual'));
