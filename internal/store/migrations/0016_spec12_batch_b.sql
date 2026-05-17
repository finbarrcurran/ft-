-- 0016_spec12_batch_b.sql — Spec 12 D1 (Batch B subset).
--
-- Crypto current_location + stock/crypto volatility_12m_pct.
-- price_history retention bump (35→365d) is a code change in
-- internal/refresh/daily.go's prune step — not a schema change.

ALTER TABLE crypto_holdings ADD COLUMN current_location TEXT;

ALTER TABLE stock_holdings  ADD COLUMN volatility_12m_pct REAL;
ALTER TABLE crypto_holdings ADD COLUMN volatility_12m_pct REAL;
