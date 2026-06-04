-- Migration 0041 — SC-24 Named Political-Figure Tracker.
--
-- Supplements Spec 9k (SC-23 sits alongside as the 13F tracker; this is the
-- named-individual layer). Two structural changes + seeds:
--
--   1. signal_events gains a new signal_type 'oge_278t' (OGE Form 278-T
--      PERIODIC transaction reports — quarterly, ~45-day lag, transaction
--      level) which is DISTINCT from the existing 'oge' type (Form 278e
--      ANNUAL position disclosure). 278-T rows store the OGE value BAND in
--      amount_bucket and leave amount_usd NULL — we never invent a point
--      value from a disclosed range (spec §C). A nullable
--      tracked_individual_id links a row back to the named watchlist.
--
--   2. tracked_individuals — the user-managed named watchlist. Each row
--      carries the person's disclosure_regime so the UI can be honest about
--      who actually files anything:
--        executive_278t   — files OGE 278-T periodic transaction reports
--        congressional_ptr — files STOCK Act PTRs (already ingested by 9k)
--        none             — no federal disclosure duty (tracked only via
--                            linked tickers + news; absence != "no trades")
--
-- Seeds: Donald Trump (executive_278t) plus Don Jr / Eric / Jared Kushner
-- (none, so AC5's "no filings — linked-tickers/news only" state has data),
-- and DJT + DJTWW into the watchlist so 9k's existing 2-business-day Form 4
-- feed catches insider activity on the family's only real signal (spec §D).
--
-- SQLite can't alter a CHECK in place, so signal_events is recreated
-- (mirrors migration 0030). Pure additive otherwise; rollback by dropping
-- tracked_individuals + the new column.

PRAGMA foreign_keys=OFF;

BEGIN TRANSACTION;

CREATE TABLE signal_events_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    signal_type          TEXT NOT NULL CHECK (signal_type IN ('insider','congress','executive_order','oge','oge_278t')),
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
    acknowledged, acknowledged_at, notes, issuer_name)
SELECT id, signal_type, tier, event_date, filed_date, ingested_at,
       ticker, sector_universe_id, actor_name, actor_role, legislator_id,
       action, amount_usd, amount_bucket, source, source_url, source_id,
       raw_json, alarm_reasons, pushed_telegram, pushed_at,
       acknowledged, acknowledged_at, notes, issuer_name
  FROM signal_events;

DROP TABLE signal_events;
ALTER TABLE signal_events_new RENAME TO signal_events;

CREATE INDEX idx_signal_events_event_date ON signal_events(event_date DESC);
CREATE INDEX idx_signal_events_tier_ack   ON signal_events(tier, acknowledged);
CREATE INDEX idx_signal_events_ticker     ON signal_events(ticker);
CREATE INDEX idx_signal_events_type_date  ON signal_events(signal_type, event_date DESC);
CREATE INDEX idx_signal_events_tracked    ON signal_events(tracked_individual_id);

-- Named watchlist (SC-24 §E / §G).
CREATE TABLE IF NOT EXISTS tracked_individuals (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    role               TEXT NULL,
    disclosure_regime  TEXT NOT NULL CHECK (disclosure_regime IN ('executive_278t','congressional_ptr','none')),
    notes              TEXT NULL,
    active             INTEGER NOT NULL DEFAULT 1,
    added_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(name)
);

INSERT OR IGNORE INTO tracked_individuals (name, role, disclosure_regime, notes) VALUES
  ('Donald J. Trump', 'President of the United States', 'executive_278t',
   'Files OGE Form 278-T periodic transaction reports (~quarterly, ~45-day lag). Stake also held via DJT (~52% Donald J. Trump Revocable Trust).'),
  ('Donald Trump Jr.', 'Trump Organization', 'none',
   'No federal disclosure duty — private citizen. Tracked only via linked tickers + news.'),
  ('Eric Trump', 'Trump Organization', 'none',
   'No federal disclosure duty — private citizen. Tracked only via linked tickers + news.'),
  ('Jared Kushner', 'Former Senior Advisor (government volunteer)', 'none',
   'Exempt from disclosure as a government volunteer. Tracked only via linked tickers + news.');

-- Seed Trump-linked tickers into the watchlist so the existing Form 4 feed
-- (2-business-day insider filings) catches officer/director/10%-owner
-- activity automatically (spec §D). user_id 1 = the single FT user.
INSERT INTO watchlist (user_id, ticker, kind, company_name, sector, note, added_at, updated_at)
SELECT 1, 'DJT', 'stock', 'Trump Media & Technology Group', 'Communication Services',
       'SC-24 Trump-linked — majority-owned by Donald J. Trump Revocable Trust (~52%). Truth Social + Truth.Fi.',
       strftime('%s','now'), strftime('%s','now')
WHERE NOT EXISTS (
    SELECT 1 FROM watchlist WHERE user_id = 1 AND kind = 'stock' AND ticker = 'DJT' AND deleted_at IS NULL
);

INSERT INTO watchlist (user_id, ticker, kind, company_name, sector, note, added_at, updated_at)
SELECT 1, 'DJTWW', 'stock', 'Trump Media & Technology Group (Warrants)', 'Communication Services',
       'SC-24 Trump-linked — TMTG warrants (same entity as DJT).',
       strftime('%s','now'), strftime('%s','now')
WHERE NOT EXISTS (
    SELECT 1 FROM watchlist WHERE user_id = 1 AND kind = 'stock' AND ticker = 'DJTWW' AND deleted_at IS NULL
);

COMMIT;

PRAGMA foreign_keys=ON;
