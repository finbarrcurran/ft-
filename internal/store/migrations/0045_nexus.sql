-- Migration 0045 — SC-36 (AI Nexus Tab / Visser Replication Layer).
--
-- Replicates Jordi Visser's (VisserLabs / 22V Research) "AI Macro Nexus"
-- three-report weekly pack natively in FT: Technical (Trend Score), Exhaustion,
-- and Fundamentals. Snapshots are keyed (ticker, as_of) — same-date re-upload
-- replaces, a new date appends — and carry both an upload origin (the Visser
-- sheet, verbatim) and, later (W3+), an FT-computed origin (source='computed').
--
-- Universe = the Visser 100 (is_nexus=1) plus, derived live at read time, the
-- user's own holdings + watchlist (NOT stored here). No existing table changes;
-- benchmarks (SPY/QQQ/SOXX) ride the existing daily_bars table (kind='benchmark').

-- Universe & membership. theme is NULL for non-nexus members (holding/watchlist
-- -only) — those are joined in live and never stored here.
CREATE TABLE IF NOT EXISTS nexus_universe (
  ticker      TEXT PRIMARY KEY,           -- canonical FT/Yahoo symbol
  company     TEXT NOT NULL DEFAULT '',
  theme       TEXT,                        -- NULL for non-nexus members
  is_nexus    INTEGER NOT NULL DEFAULT 0,  -- 1 = member of the Visser 100
  active      INTEGER NOT NULL DEFAULT 1,
  added_at    INTEGER NOT NULL DEFAULT (strftime('%s','now')),
  deleted_at  INTEGER                      -- soft-delete convention
);

-- Symbol bridge: Visser/upload symbol → FT symbol (sparse; only mismatches).
CREATE TABLE IF NOT EXISTS nexus_ticker_map (
  source_symbol TEXT PRIMARY KEY,
  ft_symbol     TEXT NOT NULL
);

-- Technical (Trend Score) snapshots. Promoted columns mirror the Signal Sheet
-- 1:1 so the UI renders without JSON parsing; components_json keeps the full
-- per-check detail for drill-down + verification debugging.
CREATE TABLE IF NOT EXISTS nexus_technical (
  ticker          TEXT NOT NULL,
  as_of           TEXT NOT NULL,           -- YYYY-MM-DD
  source          TEXT NOT NULL CHECK (source IN ('upload','computed')),
  price           REAL,
  trend_score     INTEGER,                 -- 0–100
  setup_label     TEXT,                    -- one of the 8 labels
  components_json TEXT,                    -- per-check booleans + raw inputs
  rsi14           REAL,
  ret_1w          REAL, ret_1m REAL, ret_3m REAL,
  vs_20d          REAL, vs_50d REAL, vs_200d REAL,   -- % distance
  slope_50d       REAL, slope_200d REAL,
  dist_52w_hi     REAL,
  atr_pct         REAL, vol_ratio REAL,
  rs_spy          REAL, rs_qqq REAL, rs_rank INTEGER,
  monday_note     TEXT,                    -- upload-only field; NULL for computed
  PRIMARY KEY (ticker, as_of, source)
);
CREATE INDEX IF NOT EXISTS idx_nexus_technical_asof ON nexus_technical (as_of);

-- Exhaustion snapshots.
CREATE TABLE IF NOT EXISTS nexus_exhaustion (
  ticker          TEXT NOT NULL,
  as_of           TEXT NOT NULL,
  source          TEXT NOT NULL CHECK (source IN ('upload','computed')),
  price           REAL,
  exh_score       REAL,                    -- 1–100
  band            TEXT,                    -- Extreme|Elevated|Moderate|Low
  components_json TEXT,                    -- raw signal values + per-component scores
  rsi14 REAL, rsi5 REAL, williams_r REAL,
  pos_20d REAL, ext_20d_atr REAL, ext_50d_atr REAL,
  ret_vol_1m REAL, imp_5d_atr REAL,
  vol_ratio REAL, atr_expansion REAL,
  td_setup INTEGER, td_countdown INTEGER, td_score REAL,
  atr_pct REAL, ret_1m REAL, ret_5d REAL,
  data_wt_pct REAL,                        -- available model weight
  PRIMARY KEY (ticker, as_of, source)
);
CREATE INDEX IF NOT EXISTS idx_nexus_exhaustion_asof ON nexus_exhaustion (as_of);

-- Fundamentals snapshots.
CREATE TABLE IF NOT EXISTS nexus_fundamentals (
  ticker          TEXT NOT NULL,
  as_of           TEXT NOT NULL,
  source          TEXT NOT NULL CHECK (source IN ('upload','computed')),
  market_cap      REAL,
  fwd_pe          REAL,
  next_fy_eps_growth REAL,                 -- decimal (0.39 = 39%)
  fwd_peg         REAL,
  price           REAL,
  current_fy_eps  REAL, next_fy_eps REAL,
  current_fy_end  TEXT, next_fy_end TEXT,
  data_status     TEXT,
  PRIMARY KEY (ticker, as_of, source)
);
CREATE INDEX IF NOT EXISTS idx_nexus_fundamentals_asof ON nexus_fundamentals (as_of);
