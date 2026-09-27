-- Migration 0048 — SC-44 (automatic holdings reconcile from eToro).
--
-- Reconciles FT's stock_holdings / crypto_holdings against SC-42's
-- etoro_holdings_effective after every sync. Value changes on matched holdings
-- apply silently (eToro owns invested_usd and avg_open_price); anything that
-- changes WHICH holdings exist is queued here for explicit approval.
-- Existing tables altered: only SC-42's own eToro tables (new nullable columns).

-- eToro's own USD amount per lot (Position.amount), so invested_usd comes from
-- eToro directly with no FT-side FX math (SC-44 §2).
ALTER TABLE etoro_holdings_lots      ADD COLUMN amount_usd REAL;
ALTER TABLE etoro_holdings_effective ADD COLUMN invested_usd REAL;

ALTER TABLE etoro_sync_runs ADD COLUMN values_updated INTEGER;
ALTER TABLE etoro_sync_runs ADD COLUMN proposals_pending INTEGER;

-- Propose-and-approve queue for existence changes.
CREATE TABLE IF NOT EXISTS etoro_reconcile_proposals (
  id                  INTEGER PRIMARY KEY,
  kind                TEXT NOT NULL CHECK (kind IN ('stock', 'crypto')),
  action              TEXT NOT NULL CHECK (action IN ('add', 'remove')),
  ticker              TEXT NOT NULL,
  name                TEXT,
  holding_id          INTEGER,               -- FT row a 'remove' targets
  etoro_units         REAL,                  -- 'add': what eToro holds
  etoro_invested_usd  REAL,
  etoro_avg_price     REAL,                  -- instrument currency, same unit as FT's avg_open_price
  etoro_lots          INTEGER,
  ft_invested_usd     REAL,                  -- 'remove': what FT currently shows
  has_thesis_link     INTEGER NOT NULL DEFAULT 0,
  status              TEXT NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'approved', 'dismissed', 'resolved')),
  created_at          INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL,
  decided_at          INTEGER,
  note                TEXT
);
-- One live (pending or dismissed) proposal per change; approved/resolved rows
-- are history. A dismissed proposal stays dismissed until its condition clears.
CREATE UNIQUE INDEX IF NOT EXISTS idx_etoro_proposals_live
  ON etoro_reconcile_proposals(kind, action, ticker) WHERE status IN ('pending', 'dismissed');
