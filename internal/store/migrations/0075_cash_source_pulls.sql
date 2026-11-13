-- Migration 0075 — FT-142 follow-up (SC-74): where each stored cash value came from, and a per-pull log.
--   portfolio_value_history.cash_source  etoro_api:nightly | etoro_api:balances/history (backfill) | manual
--   etoro_cash_pulls                     one row per eToro cash pull (the 30-min sync's credit, the daily Money cash call)
ALTER TABLE portfolio_value_history ADD COLUMN cash_source TEXT;
CREATE TABLE IF NOT EXISTS etoro_cash_pulls (
  id        INTEGER PRIMARY KEY,
  pulled_at INTEGER NOT NULL,
  component TEXT    NOT NULL,   -- trading_available | money_cash
  source    TEXT    NOT NULL,
  status    TEXT    NOT NULL,   -- ok | failed | missing
  usd       REAL,
  error     TEXT
);
CREATE INDEX IF NOT EXISTS idx_etoro_cash_pulls_at ON etoro_cash_pulls(pulled_at DESC);
