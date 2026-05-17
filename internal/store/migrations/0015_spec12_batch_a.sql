-- 0015_spec12_batch_a.sql — Spec 12 D1 (Batch A subset).
--
-- Batch A schema:
--   * Seed cash balance preferences (so PUT can update without prior INSERT).
--   * Seed focused exchange preference for D3 market-pill click-through.
--   * Add reason_code column to holdings_audit for typed SL/TP edit reasons (D9).
--
-- Crypto current_location + stock/crypto volatility_12m_pct + watchlist/stock
-- forecast columns land in later batch migrations (0016 / 0017) so each batch
-- is independently shippable + revertable.

-- D2 cash balance — single-row, single-user; key/value in user_preferences.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
  ('cash_balance_usd', '0', strftime('%s','now')),
  ('cash_balance_eur', '0', strftime('%s','now'));

-- D3 focused exchange — drives the collapsed market pill across Summary /
-- Stocks / Crypto. Default NYSE matches the legacy single-market behaviour.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
  ('focused_exchange', 'US', strftime('%s','now'));

-- D9 reason_code — already-supported in changes_json (Spec 3 D13); we add a
-- typed column so future analytics can pivot on reason without JSON parsing.
-- Allowed codes are enforced in the handler:
--   tech_break, tp1_hit, tighten_on_profit, loosen_vol,
--   thesis_break, earnings_approaching, rebalance, manual_other
ALTER TABLE holdings_audit ADD COLUMN reason_code TEXT;
