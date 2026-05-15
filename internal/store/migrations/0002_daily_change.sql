-- 0002_daily_change.sql
-- Add a daily_change_pct column to both holdings tables so the bot's
-- /api/bot/holdings/movers endpoint can rank by today's move.

ALTER TABLE stock_holdings  ADD COLUMN daily_change_pct REAL;
ALTER TABLE crypto_holdings ADD COLUMN daily_change_pct REAL;
