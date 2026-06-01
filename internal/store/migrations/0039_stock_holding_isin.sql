-- SC-17 Phase 2 — durable external identity for eToro holdings reconciliation.
--
-- FT holdings key on `ticker`; eToro statements carry an ISIN on closed rows.
-- Storing the ISIN on the holding lets future statement uploads match
-- high-confidence on ISIN (the "durable key") instead of ticker-only matches,
-- which per SC-17 R2/R3 always require manual confirmation. Nullable: only the
-- ~44-50% of holdings whose ticker also appears in Closed Positions can be
-- seeded on any given run, and CFDs (e.g. GLD) never carry an ISIN.
ALTER TABLE stock_holdings ADD COLUMN isin TEXT;
