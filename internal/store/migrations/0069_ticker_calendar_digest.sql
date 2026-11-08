-- SC-56 P4 — the daily report-date digest is sent at most once per UTC day, even across restarts.
CREATE TABLE IF NOT EXISTS ticker_calendar_digest_sent (
    day     TEXT PRIMARY KEY,        -- YYYY-MM-DD (UTC)
    sent_at INTEGER NOT NULL,
    lines   INTEGER NOT NULL DEFAULT 0
);
