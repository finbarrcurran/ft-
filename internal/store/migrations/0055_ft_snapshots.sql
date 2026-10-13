-- Migration 0055 — SC-46 (P5): weekly FT snapshots.
--
-- One row per ISO week (week_start = that week's Monday). Generating again in the same week
-- REPLACES the row; earlier weeks are kept. payload_json is the deterministic snapshot
-- (metrics, categorical values, sets, informational lines); deltas, trends and the Top
-- changes block are computed from consecutive payloads, never stored as truth.

CREATE TABLE IF NOT EXISTS ft_snapshots (
  id             INTEGER PRIMARY KEY,
  week_start     TEXT    NOT NULL UNIQUE,   -- YYYY-MM-DD, a Monday
  generated_at   INTEGER NOT NULL,          -- unix seconds
  schema_version INTEGER NOT NULL,
  payload_json   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ft_snapshots_week ON ft_snapshots(week_start DESC);
