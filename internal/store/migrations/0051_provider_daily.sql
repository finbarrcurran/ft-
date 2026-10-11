-- Migration 0051 — SC-48: per-provider, per-day call counters, and removal of two retired providers.
--
-- provider_health keeps only lifetime totals, so "calls per day by provider" did not exist
-- (the gap found in SC-48 Phase 1). provider_daily is written by the same health.Record choke
-- point: one row per provider per UTC day with ok / fail counts and a failure breakdown.
-- Twelve Data (retired: free plan is US-only and capped at 800 credits/day; FT spent ~3,400/day
-- and every call failed) and CryptoCompare (no code calls it any more) lose their stale
-- provider_health rows so they stop appearing as failing.

CREATE TABLE IF NOT EXISTS provider_daily (
  provider   TEXT NOT NULL,
  day        TEXT NOT NULL,                 -- YYYY-MM-DD (UTC)
  calls      INTEGER NOT NULL DEFAULT 0,
  ok         INTEGER NOT NULL DEFAULT 0,
  fail       INTEGER NOT NULL DEFAULT 0,
  fail_429   INTEGER NOT NULL DEFAULT 0,
  fail_4xx   INTEGER NOT NULL DEFAULT 0,    -- other 4xx
  fail_5xx   INTEGER NOT NULL DEFAULT 0,
  fail_other INTEGER NOT NULL DEFAULT 0,    -- network, parse, timeout, ...
  PRIMARY KEY (provider, day)
);

DELETE FROM provider_health WHERE provider IN ('twelvedata', 'cryptocompare');
