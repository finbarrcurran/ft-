-- FT-110 / FT-079: record the cash balance with each nightly value row, so a drawdown can be read on the
-- cash-inclusive basis. Rows written before this migration have NULL (the risk check falls back to today's cash).
ALTER TABLE portfolio_value_history ADD COLUMN cash_value_usd REAL;
