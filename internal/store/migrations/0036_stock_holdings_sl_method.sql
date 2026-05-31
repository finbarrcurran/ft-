-- Migration 0036 — SC-08 (candidate Spec 9c.2): explicit per-holding stop-loss
-- methodology.
--
-- Two methods:
--   'vol_envelope' (default) — catastrophe stop a full annual-volatility band
--                              below entry: entry × (1 − vol12m − safety).
--                              For long-term conviction holdings.
--   'technical'              — stop below structural support, buffered by
--                              weekly ATR (support1 − vol_tier×ATR). For
--                              shorter-term entries.
--
-- sl_safety_pct is the additive buffer below the vol band (default 2%).
--
-- Long-only assumption (handover Snag S6). Crypto / watchlist parallel deferred.
-- New holdings default to vol_envelope per SC-08 AC #1. The manual `stop_loss`
-- column remains the authoritative override (manual-override-wins): when set it
-- drives the Dist-to-SL column + proximity alerts; otherwise the chosen method
-- computes the effective stop.

ALTER TABLE stock_holdings
  ADD COLUMN sl_method TEXT NOT NULL DEFAULT 'vol_envelope'
    CHECK (sl_method IN ('technical','vol_envelope'));

ALTER TABLE stock_holdings
  ADD COLUMN sl_safety_pct REAL NOT NULL DEFAULT 0.02;
