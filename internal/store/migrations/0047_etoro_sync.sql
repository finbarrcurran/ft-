-- Migration 0047 — SC-42 (eToro ↔ FT direct sync: holdings + TP/SL).
--
-- FT polls eToro's Public API (every 30 min) and records the broker's actual
-- open positions and stop-loss / take-profit levels. Deterministic — no LLM.
-- eToro's levels are stored ALONGSIDE FT's own stop/TP (stock_holdings is not
-- touched; FT's alert logic is unchanged — Fin decision 2026-09-26).
-- No existing table is altered.

-- eToro instrument id → symbol cache (positions carry only instrument ids).
CREATE TABLE IF NOT EXISTS etoro_instruments (
  instrument_id       INTEGER PRIMARY KEY,
  symbol              TEXT NOT NULL,          -- eToro symbolFull, e.g. 'GLD', 'RHM.DE'
  ticker              TEXT NOT NULL,          -- normalised to FT's convention
  name                TEXT,
  instrument_type_id  INTEGER,
  updated_at          INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- One row per sync attempt.
CREATE TABLE IF NOT EXISTS etoro_sync_runs (
  id            INTEGER PRIMARY KEY,
  started_at    INTEGER NOT NULL,
  finished_at   INTEGER,
  status        TEXT NOT NULL,                -- 'ok' | 'failed' | 'skipped'
  positions     INTEGER,                      -- positions returned by eToro (own + copy)
  lots_new      INTEGER,                      -- new lot states recorded (opened or changed)
  lots_closed   INTEGER,
  error         TEXT
);

-- Raw per-lot history. One row per observed STATE of a position: a new row is
-- written only when a tracked field changes (units, SL, TP, trailing flag,
-- leverage), so re-running a sync never duplicates history. last_seen_at moves
-- forward on every sync. Stop/TP are NULL when eToro reports them disabled
-- (isNoStopLoss / isNoTakeProfit) — eToro's placeholder rates are never stored.
CREATE TABLE IF NOT EXISTS etoro_holdings_lots (
  id               INTEGER PRIMARY KEY,
  position_id      INTEGER NOT NULL,
  instrument_id    INTEGER NOT NULL,
  ticker           TEXT NOT NULL,
  is_buy           INTEGER NOT NULL,          -- 1 long, 0 short
  units            REAL NOT NULL,
  open_rate        REAL,
  open_date        TEXT,                      -- ISO 8601 from eToro
  leverage         REAL,
  stop_loss        REAL,                      -- NULL = no stop-loss set
  take_profit      REAL,                      -- NULL = no take-profit set
  is_tsl           INTEGER NOT NULL DEFAULT 0,-- trailing stop-loss active
  mirror_id        INTEGER NOT NULL DEFAULT 0,-- >0 = copy-trade lot (kept out of effective levels)
  settlement_type  INTEGER,                   -- eToro: 0 CFD, 1 real asset, 2 SWAP, 3 crypto margin, 4 future
  first_seen_at    INTEGER NOT NULL,
  last_seen_at     INTEGER NOT NULL,
  superseded_at    INTEGER,                   -- a newer state of this position was recorded
  closed_at        INTEGER                    -- the position stopped appearing
);
CREATE INDEX IF NOT EXISTS idx_etoro_lots_position ON etoro_holdings_lots(position_id);
CREATE INDEX IF NOT EXISTS idx_etoro_lots_ticker ON etoro_holdings_lots(ticker, first_seen_at);

-- Current state per ticker and direction, own (non-copy) lots only. Replaced
-- on every successful sync. Longs: sl_effective = HIGHEST stop, tp_effective =
-- LOWEST take-profit across lots (conservative "look at this" trigger); shorts
-- mirror it (lowest stop, highest TP). has_no_sl = 1 when no lot has a stop.
CREATE TABLE IF NOT EXISTS etoro_holdings_effective (
  ticker                   TEXT NOT NULL,
  direction                TEXT NOT NULL,     -- 'long' | 'short'
  instrument_id            INTEGER NOT NULL,
  total_units              REAL NOT NULL,
  avg_open_price           REAL,
  lot_count                INTEGER NOT NULL,
  sl_effective             REAL,
  tp_effective             REAL,
  sl_effective_position_id INTEGER,
  tp_effective_position_id INTEGER,
  has_no_sl                INTEGER NOT NULL,  -- 1 = no stop on ANY lot
  lots_without_sl          INTEGER NOT NULL,  -- partial coverage is also visible
  has_no_tp                INTEGER NOT NULL,
  copy_lot_count           INTEGER NOT NULL DEFAULT 0,
  last_synced_at           INTEGER NOT NULL,
  PRIMARY KEY (ticker, direction)
);
