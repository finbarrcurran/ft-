-- Migration 0074 — FT-142 / SC-73: eToro cash, two separate components, latest value each.
--   trading_available  the Trading account's available cash in USD (portfolio call's `credit`; excludes
--                      open-trade margin and unrealised P&L), stored by every eToro sync
--   money_cash         the eToro Money cash account(s) in their own currency plus the USD equivalent
--                      (balances call, once a day; needs the money.balance:read scope)
-- The Summary cash is the sum of the two while both are fresh; the manual cash entry is the fallback.
CREATE TABLE IF NOT EXISTS etoro_cash (
  component     TEXT PRIMARY KEY,          -- trading_available | money_cash
  amount_native REAL NOT NULL,
  currency      TEXT NOT NULL,
  usd           REAL NOT NULL,
  fx_to_usd     REAL NOT NULL,
  source        TEXT NOT NULL,             -- etoro_api:portfolio.credit | etoro_api:balances
  as_of         INTEGER NOT NULL           -- unix seconds
);
