-- Migration 0042 — SC-23 13F Institutional Tracker.
--
-- Supplements Spec 9k. Adds a fourth/fifth signal class to the Signals tab: a
-- user-curated watchlist of 13F filer CIKs whose quarterly SEC filings are
-- diffed quarter-over-quarter and cross-checked against FT's universe
-- (stock_holdings / watchlist / sector_universe). Sibling of SC-24 (the named
-- political tracker) — together they form the "External Actors" area.
--
-- Structural changes:
--
--   1. signal_events gains a new signal_type 'thirteenf'. FLAG/ALARM-tier
--      diffs (the ones that touch YOUR book) are emitted as signal_events so
--      they ride 9k's existing unified feed + Telegram routing (AC7 — no
--      parallel alerting system). SQLite can't ALTER a CHECK in place, so the
--      table is recreated (mirrors 0030 / 0041); this carries the
--      tracked_individual_id column added in 0041.
--
--   2. tracked_funds      — the user's watchlist of 13F filer CIKs.
--      fund_13f_holdings  — append-only snapshot of each filing period's book.
--      fund_13f_diffs     — computed quarter-over-quarter changes per position.
--      cusip_ticker_map   — CUSIP→ticker lookup (13F is CUSIP-keyed, FT keys on
--                           ticker). Enrich-and-flag: unmapped CUSIPs are kept
--                           and flagged, NEVER guessed (S-23b / SC-16/17 R3).
--
-- The hard caveat (spec §C / AC5) — quarterly · ~45-day lag · long-equity +
-- listed-options only · not the full book — is enforced in the API/UI layer,
-- not the schema.
--
-- Seeds: Situational Awareness LP (CIK 0002045724) + Berkshire Hathaway
-- (CIK 0001067983). Pure additive otherwise; rollback by dropping the four new
-- tables + the 'thirteenf' signal rows.

PRAGMA foreign_keys=OFF;

BEGIN TRANSACTION;

CREATE TABLE signal_events_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    signal_type          TEXT NOT NULL CHECK (signal_type IN ('insider','congress','executive_order','oge','oge_278t','thirteenf')),
    tier                 TEXT NOT NULL CHECK (tier IN ('info','flag','alarm')),
    event_date           TEXT NOT NULL,
    filed_date           TEXT NOT NULL,
    ingested_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ticker               TEXT NULL,
    sector_universe_id   INTEGER NULL REFERENCES sector_universe(id),
    actor_name           TEXT NULL,
    actor_role           TEXT NULL,
    legislator_id        INTEGER NULL,
    action               TEXT NULL CHECK (action IS NULL OR action IN ('BUY','SELL','ORDER','HOLD')),
    amount_usd           REAL NULL,
    amount_bucket        TEXT NULL,
    source               TEXT NOT NULL,
    source_url           TEXT NULL,
    source_id            TEXT NULL,
    raw_json             TEXT NULL,
    alarm_reasons        TEXT NULL,
    pushed_telegram      INTEGER NOT NULL DEFAULT 0,
    pushed_at            TEXT NULL,
    acknowledged         INTEGER NOT NULL DEFAULT 0,
    acknowledged_at      TEXT NULL,
    notes                TEXT NULL,
    issuer_name          TEXT NULL,
    tracked_individual_id INTEGER NULL,
    UNIQUE(signal_type, source, source_id)
);

INSERT INTO signal_events_new
   (id, signal_type, tier, event_date, filed_date, ingested_at,
    ticker, sector_universe_id, actor_name, actor_role, legislator_id,
    action, amount_usd, amount_bucket, source, source_url, source_id,
    raw_json, alarm_reasons, pushed_telegram, pushed_at,
    acknowledged, acknowledged_at, notes, issuer_name, tracked_individual_id)
SELECT id, signal_type, tier, event_date, filed_date, ingested_at,
       ticker, sector_universe_id, actor_name, actor_role, legislator_id,
       action, amount_usd, amount_bucket, source, source_url, source_id,
       raw_json, alarm_reasons, pushed_telegram, pushed_at,
       acknowledged, acknowledged_at, notes, issuer_name, tracked_individual_id
  FROM signal_events;

DROP TABLE signal_events;
ALTER TABLE signal_events_new RENAME TO signal_events;

CREATE INDEX idx_signal_events_event_date ON signal_events(event_date DESC);
CREATE INDEX idx_signal_events_tier_ack   ON signal_events(tier, acknowledged);
CREATE INDEX idx_signal_events_ticker     ON signal_events(ticker);
CREATE INDEX idx_signal_events_type_date  ON signal_events(signal_type, event_date DESC);
CREATE INDEX idx_signal_events_tracked    ON signal_events(tracked_individual_id);

-- ---- 13F tables ----------------------------------------------------------

-- The user's watchlist of 13F filer CIKs (zero-padded 10-digit strings).
CREATE TABLE IF NOT EXISTS tracked_funds (
    cik         TEXT PRIMARY KEY,           -- '0002045724'
    name        TEXT NOT NULL,
    notes       TEXT NULL,
    active      INTEGER NOT NULL DEFAULT 1,
    last_period TEXT NULL,                  -- most recent period_of_report ingested
    last_pulled_at TEXT NULL,
    added_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Append-only snapshot of each filing period's book. One row per
-- (cik, period_of_report, cusip, put_call). value_usd is the 13F-reported
-- dollar value of the position; shares is sshPrnamt. ticker is NULL until the
-- CUSIP is mapped (enrich-and-flag).
CREATE TABLE IF NOT EXISTS fund_13f_holdings (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    cik              TEXT NOT NULL,
    period_of_report TEXT NOT NULL,          -- 'YYYY-MM-DD' (quarter end)
    cusip            TEXT NOT NULL,
    ticker           TEXT NULL,
    issuer_name      TEXT NULL,
    value_usd        REAL NULL,
    shares           REAL NULL,
    put_call         TEXT NULL CHECK (put_call IS NULL OR put_call IN ('call','put')),
    accession        TEXT NULL,
    filed_at         TEXT NULL,
    UNIQUE(cik, period_of_report, cusip, put_call)
);
CREATE INDEX IF NOT EXISTS idx_f13f_holdings_cik_period ON fund_13f_holdings(cik, period_of_report);
CREATE INDEX IF NOT EXISTS idx_f13f_holdings_ticker ON fund_13f_holdings(ticker);

-- Computed quarter-over-quarter changes. Recomputed (idempotent) on each diff
-- run for the latest pair of periods.
CREATE TABLE IF NOT EXISTS fund_13f_diffs (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    cik               TEXT NOT NULL,
    period            TEXT NOT NULL,          -- the NEW (latest) period
    prior_period      TEXT NULL,
    cusip             TEXT NOT NULL,
    ticker            TEXT NULL,
    issuer_name       TEXT NULL,
    put_call          TEXT NULL,
    change_type       TEXT NOT NULL CHECK (change_type IN ('new','exit','increase','decrease')),
    prior_value       REAL NULL,
    new_value         REAL NULL,
    prior_shares      REAL NULL,
    new_shares        REAL NULL,
    overlaps_universe INTEGER NOT NULL DEFAULT 0,
    overlap_source    TEXT NULL,              -- holding | watchlist | sector_etf
    tier              TEXT NOT NULL CHECK (tier IN ('info','flag','alarm')),
    reasons           TEXT NULL,              -- JSON array
    computed_at       TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cik, period, cusip, put_call, change_type)
);
CREATE INDEX IF NOT EXISTS idx_f13f_diffs_cik_period ON fund_13f_diffs(cik, period);
CREATE INDEX IF NOT EXISTS idx_f13f_diffs_tier ON fund_13f_diffs(tier);

-- CUSIP→ticker lookup. 13F reports CUSIP; FT keys on ticker. Unmapped CUSIPs
-- are flagged (cusip_unmapped) rather than guessed (S-23b).
CREATE TABLE IF NOT EXISTS cusip_ticker_map (
    cusip       TEXT PRIMARY KEY,
    ticker      TEXT NOT NULL,
    issuer_name TEXT NULL,
    source      TEXT NOT NULL DEFAULT 'seed',
    added_at    TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed funds (spec §B + batch §0: Situational Awareness + Berkshire).
INSERT OR IGNORE INTO tracked_funds (cik, name, notes) VALUES
  ('0002045724', 'Situational Awareness LP',
   'Leopold Aschenbrenner. Signature thesis is the SHORT side (semiconductor puts) — 13F shows listed put OPTIONS but not outright shorts/swaps. Read with the §C caveat.'),
  ('0001067983', 'Berkshire Hathaway Inc',
   'Warren Buffett. Large concentrated long-equity book — good signal-to-noise for universe overlap.');

-- Seed CUSIP→ticker for common large-caps likely to overlap the universe
-- (NVDA archetype + Berkshire's known book). Real CUSIPs; enrich-and-flag
-- handles everything else.
INSERT OR IGNORE INTO cusip_ticker_map (cusip, ticker, issuer_name) VALUES
  ('67066G104', 'NVDA',  'NVIDIA Corp'),
  ('037833100', 'AAPL',  'Apple Inc'),
  ('594918104', 'MSFT',  'Microsoft Corp'),
  ('023135106', 'AMZN',  'Amazon.com Inc'),
  ('02079K305', 'GOOGL', 'Alphabet Inc Class A'),
  ('02079K107', 'GOOG',  'Alphabet Inc Class C'),
  ('30303M102', 'META',  'Meta Platforms Inc'),
  ('88160R101', 'TSLA',  'Tesla Inc'),
  ('007903107', 'AMD',   'Advanced Micro Devices'),
  ('11135F101', 'AVGO',  'Broadcom Inc'),
  ('874039100', 'TSM',   'Taiwan Semiconductor ADR'),
  ('46625H100', 'JPM',   'JPMorgan Chase & Co'),
  ('060505104', 'BAC',   'Bank of America Corp'),
  ('191216100', 'KO',    'Coca-Cola Co'),
  ('025816109', 'AXP',   'American Express Co'),
  ('166764100', 'CVX',   'Chevron Corp'),
  ('674599105', 'OXY',   'Occidental Petroleum'),
  ('084670702', 'BRK.B', 'Berkshire Hathaway Class B'),
  ('64110L106', 'NFLX',  'Netflix Inc'),
  ('92826C839', 'V',     'Visa Inc Class A'),
  ('57636Q104', 'MA',    'Mastercard Inc Class A'),
  ('22160K105', 'COST',  'Costco Wholesale Corp'),
  ('017175100', 'ALL',   'Allstate Corp'),
  ('438516106', 'HON',   'Honeywell International');

COMMIT;

PRAGMA foreign_keys=ON;
