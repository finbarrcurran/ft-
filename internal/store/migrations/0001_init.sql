-- 0001_init.sql
-- Initial schema for FT (Finance Tracker).
--
-- Conventions:
--   * Timestamps are unix epoch seconds (INTEGER).
--   * Dates that are conceptually dates, not instants, are ISO 'YYYY-MM-DD' TEXT.
--   * Booleans are INTEGER 0/1/NULL.
--   * JSON blobs (rare) are TEXT.
--
-- The schema models the phase-7 Next.js prototype's data structures, with
-- field names from the prototype's types/index.ts adapted to snake_case.

-- ---------------------------------------------------------------------------
-- USERS — single user expected, but the schema supports family use later.
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id              INTEGER PRIMARY KEY,
    email           TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT    NOT NULL,         -- argon2id encoded string
    display_name    TEXT,
    created_at      INTEGER NOT NULL,
    last_login_at   INTEGER
);

-- ---------------------------------------------------------------------------
-- SESSIONS — opaque random tokens stored in cookies.
-- ---------------------------------------------------------------------------
CREATE TABLE sessions (
    token       TEXT    PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    user_agent  TEXT,
    ip          TEXT
);
CREATE INDEX idx_sessions_user    ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- ---------------------------------------------------------------------------
-- SERVICE TOKENS — long-lived bearer tokens for OpenClaw (phase 2).
-- Plaintext shown once at creation; only the sha256 hash is persisted.
-- ---------------------------------------------------------------------------
CREATE TABLE service_tokens (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT    NOT NULL,
    token_hash    TEXT    NOT NULL UNIQUE,
    scopes        TEXT    NOT NULL DEFAULT 'alerts:read holdings:read',
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER,
    revoked_at    INTEGER
);
CREATE INDEX idx_service_tokens_user ON service_tokens(user_id);

-- ---------------------------------------------------------------------------
-- STOCK HOLDINGS
-- Field set mirrors the prototype's StockHolding TypeScript type.
-- ---------------------------------------------------------------------------
CREATE TABLE stock_holdings (
    id                  INTEGER PRIMARY KEY,
    user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    ticker              TEXT,
    category            TEXT,
    invested_usd        REAL    NOT NULL DEFAULT 0,
    avg_open_price      REAL,
    current_price       REAL,
    stop_loss           REAL,
    take_profit         REAL,
    rsi14               REAL,
    ma50                REAL,
    ma200               REAL,
    golden_cross        INTEGER,                       -- 0/1/NULL
    support             REAL,
    resistance          REAL,
    analyst_target      REAL,
    proposed_entry      REAL,
    technical_setup     TEXT    NOT NULL DEFAULT '',
    analyst_rr_view     TEXT    NOT NULL DEFAULT '',
    strategy_note       TEXT    NOT NULL DEFAULT '',
    sector              TEXT,
    updated_at          INTEGER NOT NULL
);
CREATE INDEX idx_stock_holdings_user        ON stock_holdings(user_id);
CREATE INDEX idx_stock_holdings_user_ticker ON stock_holdings(user_id, ticker);

-- ---------------------------------------------------------------------------
-- CRYPTO HOLDINGS
-- EUR is the source-of-truth currency; USD is derived using the latest FX.
-- ---------------------------------------------------------------------------
CREATE TABLE crypto_holdings (
    id                  INTEGER PRIMARY KEY,
    user_id             INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT    NOT NULL,
    symbol              TEXT    NOT NULL,
    classification      TEXT    NOT NULL,              -- 'core' | 'alt'
    category            TEXT,
    wallet              TEXT,
    quantity_held       REAL    NOT NULL DEFAULT 0,
    quantity_staked     REAL    NOT NULL DEFAULT 0,
    avg_buy_eur         REAL,
    cost_basis_eur      REAL,
    current_price_eur   REAL,
    current_value_eur   REAL,
    avg_buy_usd         REAL,
    cost_basis_usd      REAL,
    current_price_usd   REAL,
    current_value_usd   REAL,
    rsi14               REAL,
    change_7d_pct       REAL,
    change_30d_pct      REAL,
    strategy_note       TEXT    NOT NULL DEFAULT '',
    updated_at          INTEGER NOT NULL
);
CREATE INDEX idx_crypto_holdings_user        ON crypto_holdings(user_id);
CREATE INDEX idx_crypto_holdings_user_symbol ON crypto_holdings(user_id, symbol);

-- ---------------------------------------------------------------------------
-- NOTIFICATION LOG — idempotency guard for alerts fired to the bot.
-- The UNIQUE constraint guarantees the same alert (same holding, kind, day)
-- can never be fired twice. The ACK protocol is itself idempotent: re-posting
-- the same ACK is a no-op.
-- ---------------------------------------------------------------------------
CREATE TABLE notification_log (
    id             INTEGER PRIMARY KEY,
    holding_kind   TEXT    NOT NULL,                    -- 'stock' | 'crypto'
    holding_id     INTEGER NOT NULL,
    alert_kind     TEXT    NOT NULL,                    -- 'red' | 'amber' | 'green' | 'stop_breach' | …
    alert_day      TEXT    NOT NULL,                    -- 'YYYY-MM-DD'
    acked_at       INTEGER,
    created_at     INTEGER NOT NULL,
    UNIQUE(holding_kind, holding_id, alert_kind, alert_day)
);
CREATE INDEX idx_notification_log_holding ON notification_log(holding_kind, holding_id);

-- ---------------------------------------------------------------------------
-- META — small key/value store for things like FX snapshot and last-refresh.
-- ---------------------------------------------------------------------------
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO meta (key, value) VALUES
    ('fx_snapshot_eur_usd', '1.08'),
    ('schema_version',      '1');

-- ---------------------------------------------------------------------------
-- NEWS CACHE — small TTL cache for the third-party news adapters.
-- Free-tier API rate limits are tight (NewsAPI 100/day, CryptoPanic 100/day).
-- ---------------------------------------------------------------------------
CREATE TABLE news_cache (
    scope       TEXT    PRIMARY KEY,                   -- 'market' | 'crypto'
    fetched_at  INTEGER NOT NULL,
    payload     TEXT    NOT NULL                        -- JSON string
);
