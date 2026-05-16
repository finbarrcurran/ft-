-- 0006_exchange_override.sql — Spec 5 D2.
--
-- Per-row override for when the ticker-suffix rule misclassifies an exchange
-- (e.g. dual-listed names, weird suffixes). Stored on stock_holdings only;
-- crypto rows don't have a market.

ALTER TABLE stock_holdings ADD COLUMN exchange_override TEXT;
