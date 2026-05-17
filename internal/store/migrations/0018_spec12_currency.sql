-- 0018_spec12_currency.sql — Spec 12 closeout.
--
-- AC #15 of Spec 12 promised that typing a ticker in the Edit modal would
-- auto-fill name + sector + currency. The first two land in existing
-- columns; this migration adds the column for the third.
--
-- Listing currency, NOT trading currency for cost basis — Yahoo returns
-- e.g. "GBP" for RR.L. Investment values stay USD-denominated.

ALTER TABLE stock_holdings ADD COLUMN currency TEXT;
