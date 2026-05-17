-- 0012_transactions.sql — Spec 10 D1.
--
-- Append-only transaction + dividend log. Replaces "position state via
-- avg_open_price" with "position state derived from transactions log".
-- The holding's quantity / cost_basis / realized_pnl become DERIVED values
-- (cached in the holding row but always re-derivable from the txn log).
--
-- Append-only invariant: NO UPDATE on data columns. Corrections create a
-- new row + flip the bad row's superseded_at. This preserves provenance
-- for retrospective accuracy (Spec 9d depends on it).

CREATE TABLE transactions (
    id              INTEGER PRIMARY KEY,

    -- Linkage
    holding_kind    TEXT    NOT NULL CHECK (holding_kind IN ('stock', 'crypto')),
    holding_id      INTEGER NOT NULL,
    ticker          TEXT    NOT NULL,                                -- denormalized for query speed

    -- Transaction shape
    txn_type        TEXT    NOT NULL CHECK (txn_type IN (
                        'buy', 'sell', 'fee', 'opening_position'
                    )),
    executed_at     INTEGER NOT NULL,                                -- unix seconds
    quantity        REAL    NOT NULL,                                -- positive
    price_usd       REAL    NOT NULL,                                -- per unit
    fees_usd        REAL    NOT NULL DEFAULT 0,
    total_usd       REAL    NOT NULL,                                -- (qty × price) + fees for buys; - fees for sells

    -- Provenance
    venue           TEXT,                                            -- 'etoro', 'binance', 'tangem', etc.
    external_id     TEXT,                                            -- broker txn id
    note            TEXT,
    created_at      INTEGER NOT NULL,                                -- unix seconds (immutable)
    superseded_at   INTEGER                                          -- soft-delete for corrections; NEVER UPDATE quantity/price
);
CREATE INDEX idx_txn_holding   ON transactions (holding_kind, holding_id, executed_at);
CREATE INDEX idx_txn_ticker    ON transactions (ticker, executed_at);
CREATE INDEX idx_txn_active    ON transactions (holding_kind, holding_id) WHERE superseded_at IS NULL;

CREATE TABLE dividends (
    id                    INTEGER PRIMARY KEY,
    holding_id            INTEGER NOT NULL,
    ticker                TEXT    NOT NULL,
    ex_date               TEXT    NOT NULL,                          -- ISO YYYY-MM-DD
    pay_date              TEXT,
    amount_per_share_usd  REAL    NOT NULL,
    shares_held           REAL    NOT NULL,
    total_received_usd    REAL    NOT NULL,
    note                  TEXT,
    created_at            INTEGER NOT NULL
);
CREATE INDEX idx_div_holding ON dividends (holding_id, ex_date DESC);

-- Add thesis_link to all three holding tables.
-- watchlist already has thesis_link from Spec 4 — skip there.
ALTER TABLE stock_holdings  ADD COLUMN thesis_link TEXT;
ALTER TABLE crypto_holdings ADD COLUMN thesis_link TEXT;

-- Add realized_pnl_usd as the derived-and-cached field updated after
-- every transaction mutation. Always recomputable from transactions log.
ALTER TABLE stock_holdings  ADD COLUMN realized_pnl_usd REAL NOT NULL DEFAULT 0;
ALTER TABLE crypto_holdings ADD COLUMN realized_pnl_usd REAL NOT NULL DEFAULT 0;

-- ----- Backfill: synthesize 'opening_position' transactions for every
-- existing active holding so the derivation system has data from day 1.
-- These are explicitly tagged 'pre-spec-10-backfill' in the venue column
-- so Fin can later replace them with real broker history (D10 CSV import).

INSERT INTO transactions (
    holding_kind, holding_id, ticker, txn_type, executed_at,
    quantity, price_usd, fees_usd, total_usd,
    venue, note, created_at
)
SELECT
    'stock', id,
    COALESCE(ticker, name),
    'opening_position',
    COALESCE(updated_at, strftime('%s','now')),
    CASE
      WHEN avg_open_price IS NOT NULL AND avg_open_price > 0
        THEN invested_usd / avg_open_price
      ELSE 0
    END,
    COALESCE(avg_open_price, 0),
    0,
    invested_usd,
    'pre-spec-10-backfill',
    'Synthetic opening position from Spec 10 migration. Replace via CSV import if real history is available.',
    strftime('%s','now')
FROM stock_holdings
WHERE deleted_at IS NULL AND avg_open_price IS NOT NULL AND avg_open_price > 0;

INSERT INTO transactions (
    holding_kind, holding_id, ticker, txn_type, executed_at,
    quantity, price_usd, fees_usd, total_usd,
    venue, note, created_at
)
SELECT
    'crypto', id,
    symbol,
    'opening_position',
    COALESCE(updated_at, strftime('%s','now')),
    quantity_held + quantity_staked,
    CASE
      WHEN (quantity_held + quantity_staked) > 0 AND avg_buy_usd IS NOT NULL
        THEN avg_buy_usd
      ELSE 0
    END,
    0,
    COALESCE(cost_basis_usd, 0),
    'pre-spec-10-backfill',
    'Synthetic opening position from Spec 10 migration. Replace via CSV import if real history is available.',
    strftime('%s','now')
FROM crypto_holdings
WHERE deleted_at IS NULL AND (quantity_held + quantity_staked) > 0;
