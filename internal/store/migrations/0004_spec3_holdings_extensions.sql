-- 0004_spec3_holdings_extensions.sql
--
-- Spec 3 schema additions:
--   * `note` free-text on both holdings tables (D7)
--   * `deleted_at` for soft-delete (D6)
--   * `beta` (stocks only) for SL/TP suggestions (D2/D11)
--   * `earnings_date`, `ex_dividend_date` (stocks only, D10) — populated by a
--     future daily Yahoo fetch; safe to land empty
--   * `vol_tier` (crypto only) for manual volatility tagging (D2/D11)
--   * `holdings_audit` for every CRUD action (D5/D6/D13)
--   * `price_history` for sparklines (D8; populated by a future cron)
--
-- SQLite ALTER TABLE caveat: CHECK constraints on ADD COLUMN aren't reliably
-- supported across older SQLite versions; we enforce vol_tier values in app
-- code instead. NOT NULL is allowed if a DEFAULT is supplied.

ALTER TABLE stock_holdings ADD COLUMN note TEXT;
ALTER TABLE stock_holdings ADD COLUMN deleted_at INTEGER;
ALTER TABLE stock_holdings ADD COLUMN beta REAL;
ALTER TABLE stock_holdings ADD COLUMN earnings_date TEXT;     -- ISO 'YYYY-MM-DD'
ALTER TABLE stock_holdings ADD COLUMN ex_dividend_date TEXT;  -- ISO 'YYYY-MM-DD'

ALTER TABLE crypto_holdings ADD COLUMN note TEXT;
ALTER TABLE crypto_holdings ADD COLUMN deleted_at INTEGER;
ALTER TABLE crypto_holdings ADD COLUMN vol_tier TEXT NOT NULL DEFAULT 'medium';

-- Seed vol_tier on existing rows. BTC/ETH are low-vol; everything else stays medium.
UPDATE crypto_holdings SET vol_tier = 'low' WHERE symbol IN ('BTC', 'ETH');

-- ---------------------------------------------------------------------------
-- Audit log
-- Every mutation to a holding (create / update / soft_delete / restore /
-- import_replace) writes one row here. `changes_json` is a structured diff:
--   { "field": {"old": X, "new": Y}, ... }
-- For 'create' the value is { "new": <full row snapshot> }.
-- For 'soft_delete' / 'restore' the value is { "reason": "..." } or {}.
-- ---------------------------------------------------------------------------
CREATE TABLE holdings_audit (
    id            INTEGER PRIMARY KEY,
    ts            INTEGER NOT NULL,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    holding_kind  TEXT    NOT NULL,                       -- 'stock' | 'crypto'
    holding_id    INTEGER NOT NULL,
    ticker        TEXT,                                   -- stocks only
    symbol        TEXT,                                   -- crypto only
    action        TEXT    NOT NULL,                       -- 'create' | 'update' | 'soft_delete' | 'restore' | 'import_replace'
    changes_json  TEXT    NOT NULL,
    reason        TEXT,
    actor         TEXT    NOT NULL DEFAULT 'fin'
);
CREATE INDEX idx_audit_ts        ON holdings_audit (ts DESC);
CREATE INDEX idx_audit_ticker    ON holdings_audit (ticker);
CREATE INDEX idx_audit_symbol    ON holdings_audit (symbol);
CREATE INDEX idx_audit_user      ON holdings_audit (user_id, ts DESC);

-- ---------------------------------------------------------------------------
-- Price history (sparklines)
-- Spec 3 D8 populates this from a future daily cron (~04:00 UTC).
-- Sliding window of ~30 days per ticker; trivial storage (36 holdings ×
-- 30 days ≈ 1100 rows).
-- ---------------------------------------------------------------------------
CREATE TABLE price_history (
    ticker  TEXT NOT NULL,
    kind    TEXT NOT NULL,    -- 'stock' | 'crypto'
    date    TEXT NOT NULL,    -- ISO 'YYYY-MM-DD'
    close   REAL NOT NULL,
    PRIMARY KEY (ticker, kind, date)
);
CREATE INDEX idx_price_history_ticker ON price_history (ticker, kind, date DESC);
