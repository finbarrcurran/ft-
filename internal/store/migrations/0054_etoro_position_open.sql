-- Migration 0054 — SC-51 Part A: per-trade statistics need the position's open date (hold time)
-- and its invested amount (small-position filter). Both columns exist in the statement's Closed
-- Positions sheet ("Open Date", "Amount") but 0053 did not keep them. NULL = not yet known;
-- re-importing a statement through the Position-ID merge fills them in.

ALTER TABLE etoro_closed_positions ADD COLUMN open_date  TEXT;      -- YYYY-MM-DD
ALTER TABLE etoro_closed_positions ADD COLUMN amount_usd REAL;      -- invested amount, USD
