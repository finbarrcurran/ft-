-- Spec 9k Phase A — Political & Insider Signal Tab
--
-- All 5 tables from Spec 9k §D1 created in this migration even though
-- only signal_events + the universe-filter helpers are used in v1.10.0.
-- The Congress (D4) + Executive Order (D5) tables are scaffolded now so
-- v1.11.0 can ship without a second schema migration.
--
-- Pure cache + reference tables. No data migration risk; rollback by
-- dropping the 5 new tables.

-- 1. Unified signal events table (insider in v1.10.0; Congress + EO from v1.11.0)
CREATE TABLE IF NOT EXISTS signal_events (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    signal_type          TEXT NOT NULL CHECK (signal_type IN ('insider','congress','executive_order')),
    tier                 TEXT NOT NULL CHECK (tier IN ('info','flag','alarm')),
    event_date           TEXT NOT NULL,         -- YYYY-MM-DD UTC — actual transaction / EO publication
    filed_date           TEXT NOT NULL,         -- YYYY-MM-DD UTC — disclosure / filing date
    ingested_at          TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ticker               TEXT NULL,
    sector_universe_id   INTEGER NULL REFERENCES sector_universe(id),
    actor_name           TEXT NULL,
    actor_role           TEXT NULL,
    legislator_id        INTEGER NULL,          -- FK added in v1.11.0 when legislators populates
    action               TEXT NULL CHECK (action IS NULL OR action IN ('BUY','SELL','ORDER')),
    amount_usd           REAL NULL,
    amount_bucket        TEXT NULL,
    source               TEXT NOT NULL,         -- 'sec_edgar' | 'house_sw' | 'senate_sw' | 'fed_reg'
    source_url           TEXT NULL,
    source_id            TEXT NULL,             -- accession # / EO # / PTR id — for de-dup
    raw_json             TEXT NULL,
    alarm_reasons        TEXT NULL,             -- JSON array
    pushed_telegram      INTEGER NOT NULL DEFAULT 0,
    pushed_at            TEXT NULL,
    acknowledged         INTEGER NOT NULL DEFAULT 0,
    acknowledged_at      TEXT NULL,
    notes                TEXT NULL,
    UNIQUE(signal_type, source, source_id)
);

CREATE INDEX IF NOT EXISTS idx_signal_events_event_date ON signal_events(event_date DESC);
CREATE INDEX IF NOT EXISTS idx_signal_events_tier_ack ON signal_events(tier, acknowledged);
CREATE INDEX IF NOT EXISTS idx_signal_events_ticker ON signal_events(ticker);
CREATE INDEX IF NOT EXISTS idx_signal_events_type_date ON signal_events(signal_type, event_date DESC);

-- 2. Legislator reference (seeded quarterly from unitedstates/congress-legislators)
CREATE TABLE IF NOT EXISTS legislators (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    full_name       TEXT NOT NULL,
    chamber         TEXT NOT NULL CHECK (chamber IN ('house','senate')),
    party           TEXT NULL,
    state           TEXT NOT NULL,
    district        TEXT NULL,
    bioguide_id     TEXT UNIQUE,
    active          INTEGER NOT NULL DEFAULT 1,
    last_refreshed  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_legislators_name ON legislators(full_name);

-- 3. Committee assignments (one row per legislator-committee pair)
CREATE TABLE IF NOT EXISTS committee_assignments (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    legislator_id   INTEGER NOT NULL REFERENCES legislators(id),
    committee_code  TEXT NOT NULL,
    committee_name  TEXT NOT NULL,
    role            TEXT NULL,
    last_refreshed  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(legislator_id, committee_code)
);

-- 4. Committee → sector_universe jurisdiction map (drives Congress ALARM)
CREATE TABLE IF NOT EXISTS committee_sector_map (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    committee_code      TEXT NOT NULL,
    sector_universe_id  INTEGER NOT NULL REFERENCES sector_universe(id),
    alarm_strength      TEXT NOT NULL DEFAULT 'strong' CHECK (alarm_strength IN ('strong','moderate')),
    notes               TEXT NULL,
    UNIQUE(committee_code, sector_universe_id)
);

-- 5. Executive Order keyword → sector_universe map
CREATE TABLE IF NOT EXISTS eo_sector_keywords (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    sector_universe_id  INTEGER NOT NULL REFERENCES sector_universe(id),
    keyword             TEXT NOT NULL,
    weight              INTEGER NOT NULL DEFAULT 1,
    UNIQUE(sector_universe_id, keyword)
);

CREATE INDEX IF NOT EXISTS idx_eo_keywords_kw ON eo_sector_keywords(LOWER(keyword));
