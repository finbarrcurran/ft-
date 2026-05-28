-- Migration 0030 — add 'oge' to signal_events.signal_type CHECK constraint.
--
-- v1.21C. Adds Office of Government Ethics (OGE) Form 278e Public Annual
-- Statement filings as a recognised signal type. OGE filings are the
-- disclosure regime for sitting Presidents, Vice Presidents, and Cabinet
-- members — STOCK Act PTRs don't apply to them. Filings are annual
-- (not per-transaction), so OGE rows in signal_events represent
-- "discovered position" rather than "transaction".
--
-- SQLite doesn't support altering a CHECK constraint in place, so we
-- recreate the table. Indexes are recreated explicitly afterwards.

PRAGMA foreign_keys=OFF;

BEGIN TRANSACTION;

CREATE TABLE signal_events_new (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    signal_type          TEXT NOT NULL CHECK (signal_type IN ('insider','congress','executive_order','oge')),
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
    UNIQUE(signal_type, source, source_id)
);

INSERT INTO signal_events_new
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

COMMIT;

PRAGMA foreign_keys=ON;
