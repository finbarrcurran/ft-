-- 0014_provider_health.sql — Spec 7 D1.
--
-- One row per external provider, updated synchronously on every call.
-- Surfaces in the Settings → Diagnostics panel.
--
-- Provider names (lowercase, stable identifiers):
--   finnhub, twelvedata, yahoo, coingecko, frankfurter, newsapi,
--   cryptopanic, alternative_me, cnn_feargreed
--
-- Counts are lifetime totals (never reset). consecutive_failures resets
-- to 0 on every success.

CREATE TABLE provider_health (
    provider              TEXT    PRIMARY KEY,
    last_success_at       INTEGER,
    last_failure_at       INTEGER,
    last_error            TEXT,
    consecutive_failures  INTEGER NOT NULL DEFAULT 0,
    success_count         INTEGER NOT NULL DEFAULT 0,
    failure_count         INTEGER NOT NULL DEFAULT 0,
    updated_at            INTEGER NOT NULL
);
