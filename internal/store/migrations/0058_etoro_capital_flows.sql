-- Migration 0058 — SC-52 P4: dated capital flows (deposits, withdrawals, transfers between the
-- trading account and eToro Money) from the statements' Account Activity sheet.
--
-- Keyed by a hash of the activity row (timestamp, type, details, amount, occurrence), so a
-- re-import or an overlapping statement adds nothing twice. Amounts are USD as eToro reports
-- them; positive = money into the trading account, negative = out. `manual = 1` rows are the
-- fallback for periods without a statement (entered by hand, deletable); statement rows are not.
-- Kinds: deposit | transfer_in | withdrawal | transfer_out | reversal | fx_fee | manual_in | manual_out.

CREATE TABLE IF NOT EXISTS etoro_capital_flows (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL,
  flow_key    TEXT    NOT NULL,
  occurred_at TEXT    NOT NULL,            -- YYYY-MM-DDTHH:MM:SS (statement clock)
  day         TEXT    NOT NULL,            -- YYYY-MM-DD
  kind        TEXT    NOT NULL,
  amount_usd  REAL    NOT NULL,
  details     TEXT    NOT NULL DEFAULT '', -- the statement's Details cell, or the manual note
  manual      INTEGER NOT NULL DEFAULT 0,
  source_file TEXT    NOT NULL DEFAULT '',
  imported_at INTEGER NOT NULL,
  UNIQUE (user_id, flow_key)
);
CREATE INDEX IF NOT EXISTS idx_etoro_flows_day ON etoro_capital_flows(user_id, day);

-- Per statement period: what the Account Summary says, and whether the rows reconciled to it.
ALTER TABLE etoro_statement_periods ADD COLUMN flows_summary_json TEXT NOT NULL DEFAULT '';
