-- 0011_performance.sql — Spec 9d D1.
--
-- Closed-trade retrospective + performance snapshots. Append-only by
-- design: every trade open + every trade close gets ONE row that's
-- never UPDATEd. Corrections happen via `superseded_at` soft-delete
-- + a new row (preserves provenance for cohort analytics).
--
-- A trade-close row is derived by walking holdings_audit:
--   1. Find the most recent 'create' audit row → trade_snapshot_json
--      (Spec 9c D17) captures entry conditions
--   2. Walk subsequent partial-sell audit rows → volume-weighted exit
--   3. Detect closure: stage transitioned to 'stopped' OR quantity → 0
--   4. Compute realized P&L + R-multiple
--   5. Insert into closed_trades
--
-- Idempotency: each `source_audit_close_id` maps to exactly one
-- closed_trades row (UNIQUE constraint). Re-running derivation is safe.

CREATE TABLE closed_trades (
    id                       INTEGER PRIMARY KEY,

    -- Identification
    ticker                   TEXT    NOT NULL,
    kind                     TEXT    NOT NULL CHECK (kind IN ('stock', 'crypto')),
    holding_id               INTEGER NOT NULL,                    -- original holding row (may be soft-deleted)

    -- Entry snapshot (copied from holdings_audit.trade_snapshot_json at open)
    opened_at                INTEGER NOT NULL,                    -- unix seconds
    setup_type               TEXT,                                -- A_breakout_retest | B_support_bounce | C_continuation
    regime_effective         TEXT,                                -- stable | shifting | defensive | unclassified
    jordi_score              INTEGER,
    cowen_score              INTEGER,
    percoco_score            INTEGER,
    atr_weekly_at_entry      REAL,
    vol_tier_at_entry        TEXT,

    -- Levels at entry
    support_1_at_entry       REAL,
    resistance_1_at_entry    REAL,
    resistance_2_at_entry    REAL,
    entry_price              REAL NOT NULL,
    sl_at_entry              REAL NOT NULL,
    tp1_at_entry             REAL,
    tp2_at_entry             REAL,
    r_multiple_tp1_planned   REAL,
    r_multiple_tp2_planned   REAL,

    -- Sizing at entry
    position_size_units      REAL NOT NULL,
    position_size_usd        REAL NOT NULL,
    per_trade_risk_pct       REAL,
    per_trade_risk_usd       REAL,
    portfolio_value_at_entry REAL,

    -- Exit snapshot
    closed_at                INTEGER NOT NULL,
    exit_reason              TEXT    NOT NULL CHECK (exit_reason IN (
                                'tp1_hit', 'tp2_hit', 'sl_hit', 'bounce_close',
                                'time_stop', 'manual_close', 'thesis_change', 'partial_then_remainder'
                             )),
    exit_price_avg           REAL    NOT NULL,                    -- volume-weighted average
    holding_period_days      INTEGER NOT NULL,

    -- Realized outcome
    realized_pnl_usd         REAL    NOT NULL,
    realized_pnl_pct         REAL    NOT NULL,
    realized_r_multiple      REAL    NOT NULL,                    -- (exit − entry) / (entry − sl); negative on losers

    -- Post-trade note (manual; settable via PUT)
    post_mortem              TEXT,

    -- Provenance + supersede
    source_audit_open_id     INTEGER NOT NULL,
    source_audit_close_id    INTEGER NOT NULL UNIQUE,              -- the idempotency key
    derived_at               INTEGER NOT NULL,                    -- when this row was computed
    superseded_at            INTEGER                              -- soft-delete for corrections
);
CREATE INDEX idx_closed_trades_opened     ON closed_trades (opened_at DESC);
CREATE INDEX idx_closed_trades_ticker     ON closed_trades (ticker, opened_at DESC);
CREATE INDEX idx_closed_trades_setup      ON closed_trades (setup_type, opened_at DESC);
CREATE INDEX idx_closed_trades_regime     ON closed_trades (regime_effective, opened_at DESC);
CREATE INDEX idx_closed_trades_superseded ON closed_trades (superseded_at) WHERE superseded_at IS NULL;

-- Daily aggregated metrics per cohort. Re-derived nightly. UNIQUE on
-- (date, cohort_key) so the nightly job UPSERTs cleanly.
CREATE TABLE performance_snapshots (
    id                      INTEGER PRIMARY KEY,
    snapshot_date           TEXT    NOT NULL,                    -- 'YYYY-MM-DD'
    cohort_key              TEXT    NOT NULL,                    -- 'all' | 'setup:A_breakout_retest' | 'jordi:13-16' | ...
    window                  TEXT    NOT NULL DEFAULT 'all',      -- 'all' | '365d' | '90d' | '30d'
    trade_count             INTEGER NOT NULL,
    win_count               INTEGER NOT NULL,
    win_rate                REAL    NOT NULL,
    avg_winner_r            REAL    NOT NULL,
    avg_loser_r             REAL    NOT NULL,
    expectancy_r            REAL    NOT NULL,                     -- win_rate × avg_winner_r + (1-win_rate) × avg_loser_r
    total_realized_pnl_usd  REAL    NOT NULL,
    avg_holding_period_days REAL,
    max_drawdown_pct        REAL,                                 -- within cohort
    computed_at             INTEGER NOT NULL,
    UNIQUE (snapshot_date, cohort_key, window)
);
CREATE INDEX idx_perf_snapshots_date   ON performance_snapshots (snapshot_date DESC, cohort_key);
CREATE INDEX idx_perf_snapshots_cohort ON performance_snapshots (cohort_key, snapshot_date DESC);
