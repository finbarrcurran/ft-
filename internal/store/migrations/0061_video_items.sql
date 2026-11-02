-- Migration 0061 — SC-53: the v5 item extraction of a video digest, with the grounding gate.
--
-- One run per digest (video_item_runs) and one row per extracted item (video_items): every claim,
-- prediction, level, indicator, named ticker and self-correction the speaker states, as written by
-- the model (fields_json) plus the span it cited. The gate stores a class per item
-- (grounded / wrong_place / far / invented / name_mapping) with its issues; items that fail are kept
-- and shown with their flag, never deleted. signal_ok = 0 marks the invented and name_mapping items
-- that must stay out of any signal. Chart-only numbers are listed in chart_unverified_json and stay
-- display-only. resolution_json holds, for tickers_named, the ticker found for the name as spoken
-- (or "unresolved"). The direction flag (a second small call) is a flag for a human, not a filter.
-- Cascades from video_digests, so a forced re-ingest drops the items and re-extracts them.
CREATE TABLE IF NOT EXISTS video_item_runs (
  id                  INTEGER PRIMARY KEY,
  digest_id           INTEGER NOT NULL UNIQUE REFERENCES video_digests(id) ON DELETE CASCADE,
  schema_version      INTEGER NOT NULL,
  model               TEXT    NOT NULL,
  chunks              INTEGER NOT NULL DEFAULT 1,
  input_tokens        INTEGER,
  output_tokens       INTEGER,
  cost_usd            REAL    NOT NULL DEFAULT 0,
  direction_cost_usd  REAL    NOT NULL DEFAULT 0,
  items_total         INTEGER NOT NULL DEFAULT 0,
  counts_json         TEXT,
  tone                TEXT,
  status              TEXT    NOT NULL DEFAULT 'ok',
  error               TEXT,
  created_at          INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS video_items (
  id                     INTEGER PRIMARY KEY,
  run_id                 INTEGER NOT NULL REFERENCES video_item_runs(id) ON DELETE CASCADE,
  digest_id              INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  block                  TEXT    NOT NULL,
  ord                    INTEGER NOT NULL,
  fields_json            TEXT    NOT NULL,
  ref_start              INTEGER,
  ref_end                INTEGER,
  gate_class             TEXT    NOT NULL,
  gate_issues_json       TEXT,
  chart_unverified_json  TEXT,
  signal_ok              INTEGER NOT NULL DEFAULT 1,
  resolution_json        TEXT,
  direction_verdict      TEXT,
  direction_view         TEXT,
  direction_reason       TEXT
);
CREATE INDEX IF NOT EXISTS idx_video_items_digest ON video_items(digest_id, block, ord);
CREATE INDEX IF NOT EXISTS idx_video_items_class ON video_items(gate_class);
