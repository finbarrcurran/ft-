-- 0038_etoro_performance.sql — SC-17 Phase 1 (eToro statement importer).
--
-- Recurring import of eToro account statements (.xlsx) → annual + YTD
-- performance history. Propose-and-apply (never overwrites holdings; this
-- phase only writes performance rows). Re-uploading a statement that covers
-- year Y supersedes the prior rows for that (user, year) and inserts fresh.
--
-- Compute model (verified against the real statements):
--   * Financial Summary sheet is the AUTHORITATIVE per-type headline P&L,
--     dividends, fees and interest. Summing Closed Positions Profit(USD) by
--     Type reconciles exactly to the FS "(Profit or Loss)" rows.
--   * Strategy axis (discretionary vs copy) comes from Closed Positions
--     `Copied From` (`-`/empty = discretionary, incl. discretionary CFDs;
--     a username = copy). Per SC-17 R1, `Type=CFD` is a WRAPPER, never the
--     strategy or asset-class signal — so the disc/copy split is by
--     Copied From, not by Type.
--   * Wrapper axis (CFD vs cash) is a display detail only and does NOT drive
--     bucketing here.
--
-- Naming: the tables are namespaced `etoro_*` to avoid confusion with the
-- unrelated Spec 9d trade-performance subsystem (closed_trades /
-- performance_snapshots / the Performance tab). SC-17's performance view
-- lives as a panel under the Summary tab.
--
-- The bucket split is denormalised into disc/copy column pairs (rather than
-- one row per strategy_bucket) because dividends/fees/interest have no
-- per-position bucket — this keeps the realised-P&L split honest while
-- carrying the non-trade lines once per (year, asset_type).

-- Per (user, year, asset_type) — the asset-type breakdown that drives the
-- annual table and the discretionary/copy toggle.
CREATE TABLE etoro_performance (
    id                 INTEGER PRIMARY KEY,
    user_id            INTEGER NOT NULL,
    year               INTEGER NOT NULL,         -- close-date year
    asset_type         TEXT    NOT NULL,         -- 'Stocks' | 'ETFs' | 'Crypto' | 'CFDs'

    -- Authoritative realised trading P&L for this type (Financial Summary).
    realised_pnl_usd   REAL NOT NULL DEFAULT 0,
    realised_pnl_eur   REAL NOT NULL DEFAULT 0,

    -- Strategy split of realised P&L (Closed Positions, by Copied From).
    realised_disc_usd  REAL NOT NULL DEFAULT 0,
    realised_disc_eur  REAL NOT NULL DEFAULT 0,
    realised_copy_usd  REAL NOT NULL DEFAULT 0,
    realised_copy_eur  REAL NOT NULL DEFAULT 0,

    -- Dividends attributed to this asset type (Dividends sheet, by Type).
    dividends_usd      REAL NOT NULL DEFAULT 0,
    dividends_eur      REAL NOT NULL DEFAULT 0,

    -- Type-attributable fees (spread fee on type + SDRT for stocks). Stored
    -- as a negative number (a cost), matching the Financial Summary sign.
    fees_usd           REAL NOT NULL DEFAULT 0,
    fees_eur           REAL NOT NULL DEFAULT 0,

    trade_count        INTEGER NOT NULL DEFAULT 0,

    superseded         INTEGER NOT NULL DEFAULT 0,
    source_file        TEXT NOT NULL DEFAULT '',
    imported_at        INTEGER NOT NULL          -- unix seconds
);
CREATE INDEX idx_etoro_perf_user_year
    ON etoro_performance (user_id, year, superseded);

-- Per (user, year) — the authoritative period headline + non-trade lines
-- that drive the YTD card and the annual summary row.
CREATE TABLE etoro_performance_year (
    id                     INTEGER PRIMARY KEY,
    user_id                INTEGER NOT NULL,
    year                   INTEGER NOT NULL,
    range_start            TEXT NOT NULL,        -- YYYY-MM-DD (statement start)
    range_end              TEXT NOT NULL,        -- YYYY-MM-DD (statement end)
    is_ytd                 INTEGER NOT NULL DEFAULT 0, -- 1 = partial / current year

    -- Authoritative totals (sum of the relevant Financial Summary rows).
    realised_pnl_usd       REAL NOT NULL DEFAULT 0, -- sum of all FS P&L rows
    realised_pnl_eur       REAL NOT NULL DEFAULT 0,
    dividends_usd          REAL NOT NULL DEFAULT 0, -- stock+ETF+CFD dividends
    dividends_eur          REAL NOT NULL DEFAULT 0,
    fees_usd               REAL NOT NULL DEFAULT 0, -- all fees (spread+SDRT+admin), negative
    fees_eur               REAL NOT NULL DEFAULT 0,
    interest_usd           REAL NOT NULL DEFAULT 0,
    interest_eur           REAL NOT NULL DEFAULT 0,
    net_usd                REAL NOT NULL DEFAULT 0, -- sum of every FS line (period net)
    net_eur                REAL NOT NULL DEFAULT 0,

    -- Reconciliation: our Closed-Positions-derived realised P&L vs the FS
    -- headline. Expect ~0; small deltas (open-position timing) are flagged.
    computed_pnl_usd       REAL NOT NULL DEFAULT 0,
    computed_pnl_eur       REAL NOT NULL DEFAULT 0,
    recon_delta_usd        REAL NOT NULL DEFAULT 0,
    recon_delta_eur        REAL NOT NULL DEFAULT 0,

    superseded             INTEGER NOT NULL DEFAULT 0,
    source_file            TEXT NOT NULL DEFAULT '',
    imported_at            INTEGER NOT NULL
);
CREATE INDEX idx_etoro_perf_year_user
    ON etoro_performance_year (user_id, year, superseded);
