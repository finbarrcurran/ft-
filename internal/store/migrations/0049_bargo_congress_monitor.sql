-- Migration 0049 — SC-47: Bargo Congress feed + raw trade history, and the
-- connectivity monitor (provider freshness + Telegram alerts).
--
-- congress_trades_raw keeps EVERY Congress trade we ever receive, unfiltered
-- and append-only (upserted on source_key). signal_events stays the filtered,
-- tiered view for the Signals tab; this table is the history for later
-- analysis (Bargo's free tier only serves the last ~3 months).
-- No existing table is altered.

CREATE TABLE IF NOT EXISTS congress_trades_raw (
  id               INTEGER PRIMARY KEY,
  source           TEXT NOT NULL,            -- 'bargo' | 'capitol_trades' (legacy file import)
  source_key       TEXT NOT NULL UNIQUE,     -- stable per-trade key (see internal/bargo)
  member           TEXT,
  member_slug      TEXT,
  chamber          TEXT,                     -- 'house' | 'senate'
  state            TEXT,
  party            TEXT,                     -- legacy rows only; Bargo has none
  ticker           TEXT,
  asset            TEXT,
  trade_type       TEXT,                     -- 'purchase' | 'sale' | 'exchange'
  amount_low       REAL,
  amount_high      REAL,
  amount_range     TEXT,
  transaction_date TEXT,                     -- YYYY-MM-DD
  disclosure_date  TEXT,                     -- YYYY-MM-DD
  est_price        REAL,
  recent_price     REAL,
  filing_url       TEXT,
  first_seen_at    INTEGER NOT NULL,
  last_seen_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_congress_raw_txn_date ON congress_trades_raw(transaction_date DESC);
CREATE INDEX IF NOT EXISTS idx_congress_raw_ticker   ON congress_trades_raw(ticker, transaction_date DESC);
CREATE INDEX IF NOT EXISTS idx_congress_raw_member   ON congress_trades_raw(member_slug, transaction_date DESC);

-- One row per Bargo sync attempt.
CREATE TABLE IF NOT EXISTS bargo_sync_runs (
  id           INTEGER PRIMARY KEY,
  started_at   INTEGER NOT NULL,
  finished_at  INTEGER,
  status       TEXT NOT NULL,                -- 'ok' | 'partial' | 'failed'
  requests     INTEGER,
  rows_fetched INTEGER,
  rows_new     INTEGER,
  error        TEXT
);

-- Connectivity monitor: current state per watched source ...
CREATE TABLE IF NOT EXISTS monitor_state (
  source          TEXT PRIMARY KEY,
  status          TEXT NOT NULL,             -- 'ok' | 'down'
  since_ts        INTEGER NOT NULL,          -- when the current status began
  last_checked_ts INTEGER NOT NULL,
  last_ok_ts      INTEGER,
  detail          TEXT,
  last_alert_ts   INTEGER                    -- last Telegram alert sent for this source
);

-- ... and an append-only history of transitions and alerts.
CREATE TABLE IF NOT EXISTS monitor_events (
  id        INTEGER PRIMARY KEY,
  ts        INTEGER NOT NULL,
  source    TEXT NOT NULL,
  kind      TEXT NOT NULL,                   -- 'down' | 'still_down' | 'recovered'
  detail    TEXT,
  delivered INTEGER NOT NULL DEFAULT 0       -- 1 = Telegram accepted it
);
CREATE INDEX IF NOT EXISTS idx_monitor_events_ts ON monitor_events(ts DESC);
