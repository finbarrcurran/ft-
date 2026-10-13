-- Migration 0053 — SC-50: position-level eToro statement storage (idempotent, overlap-safe).
--
-- The performance importer used to keep only per-year totals and let each upload replace the
-- year, so a later, narrower statement erased an earlier one (the live 2026 row covers only
-- 2026-05-30..07-04) and multi-year statements were booked entirely to the End Date year.
-- Closed positions and dividend rows are now stored once each, keyed by eToro's Position ID
-- (re-importing the same statement, or an overlapping one, changes nothing); the
-- Financial Summary's whole-period lines (fees, interest, refunds, other income) are stored
-- per statement period. etoro_performance_year / etoro_performance are re-derived from these
-- tables for the years a statement touches.

CREATE TABLE IF NOT EXISTS etoro_closed_positions (
  user_id      INTEGER NOT NULL,
  position_id  TEXT    NOT NULL,
  close_date   TEXT,                       -- YYYY-MM-DD
  year         INTEGER NOT NULL,           -- year of the position's OWN close date
  asset_type   TEXT    NOT NULL,           -- Stocks | ETFs | Crypto | CFDs
  is_copy      INTEGER NOT NULL DEFAULT 0,
  profit_usd   REAL    NOT NULL DEFAULT 0,
  profit_eur   REAL    NOT NULL DEFAULT 0,
  source_file  TEXT    NOT NULL DEFAULT '',
  imported_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, position_id)
);
CREATE INDEX IF NOT EXISTS idx_etoro_cp_year ON etoro_closed_positions(user_id, year);

CREATE TABLE IF NOT EXISTS etoro_dividend_rows (
  user_id      INTEGER NOT NULL,
  row_key      TEXT    NOT NULL,           -- position id | pay date | instrument | amount
  position_id  TEXT,
  pay_date     TEXT,
  year         INTEGER NOT NULL,
  asset_type   TEXT    NOT NULL,
  usd          REAL    NOT NULL DEFAULT 0,
  eur          REAL    NOT NULL DEFAULT 0,
  source_file  TEXT    NOT NULL DEFAULT '',
  imported_at  INTEGER NOT NULL,
  PRIMARY KEY (user_id, row_key)
);
CREATE INDEX IF NOT EXISTS idx_etoro_div_year ON etoro_dividend_rows(user_id, year);

-- One row per imported statement: the whole-period Financial Summary lines.
CREATE TABLE IF NOT EXISTS etoro_statement_periods (
  id                   INTEGER PRIMARY KEY,
  user_id              INTEGER NOT NULL,
  range_start          TEXT NOT NULL,
  range_end            TEXT NOT NULL,
  source_file          TEXT NOT NULL DEFAULT '',
  net_usd REAL NOT NULL DEFAULT 0,            net_eur REAL NOT NULL DEFAULT 0,
  realised_usd REAL NOT NULL DEFAULT 0,       realised_eur REAL NOT NULL DEFAULT 0,
  dividends_usd REAL NOT NULL DEFAULT 0,      dividends_eur REAL NOT NULL DEFAULT 0,
  dividend_residual_usd REAL NOT NULL DEFAULT 0, dividend_residual_eur REAL NOT NULL DEFAULT 0, -- FS dividends not on the Dividends sheet (CFD dividends)
  fees_usd REAL NOT NULL DEFAULT 0,           fees_eur REAL NOT NULL DEFAULT 0,
  interest_usd REAL NOT NULL DEFAULT 0,       interest_eur REAL NOT NULL DEFAULT 0,
  refunds_usd REAL NOT NULL DEFAULT 0,        refunds_eur REAL NOT NULL DEFAULT 0,
  other_income_usd REAL NOT NULL DEFAULT 0,   other_income_eur REAL NOT NULL DEFAULT 0,
  unclassified_usd REAL NOT NULL DEFAULT 0,   unclassified_eur REAL NOT NULL DEFAULT 0,
  recon_pnl_delta_usd REAL NOT NULL DEFAULT 0, recon_pnl_delta_eur REAL NOT NULL DEFAULT 0,
  recon_net_delta_usd REAL NOT NULL DEFAULT 0, recon_net_delta_eur REAL NOT NULL DEFAULT 0,
  fees_by_type_json    TEXT NOT NULL DEFAULT '{}',   -- {"Stocks":[usd,eur],...}
  lines_json           TEXT NOT NULL DEFAULT '[]',   -- every Financial Summary line with its component
  superseded           INTEGER NOT NULL DEFAULT 0,
  imported_at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_etoro_periods_user ON etoro_statement_periods(user_id, superseded);

ALTER TABLE etoro_performance_year ADD COLUMN refunds_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN refunds_eur REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN other_income_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN other_income_eur REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN unclassified_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN unclassified_eur REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN recon_net_delta_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN recon_net_delta_eur REAL NOT NULL DEFAULT 0;
ALTER TABLE etoro_performance_year ADD COLUMN coverage_note TEXT NOT NULL DEFAULT '';
