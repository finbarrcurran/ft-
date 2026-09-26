-- Migration 0046 — SC-43 P1 (Video Digest Brain: schema + ingest).
--
-- Stores the LLM digest of each video package produced by the Jarvis P0 pipeline
-- (packages live on disk at /var/lib/video_digest; FT reads them read-only).
-- Six new tables, video_* prefix (D2) plus cowen_weekly_snapshot. Frame/asset
-- references are paths, never blobs. No existing table is altered.
--
-- Video-derived data is context / candidate-generator only — never a scoring
-- input (D6). suggested_regime is a reserved slot, never auto-applied (D7).

CREATE TABLE IF NOT EXISTS video_sources (
  id          TEXT PRIMARY KEY,            -- 'jordi' | 'cowen'
  name        TEXT NOT NULL,
  channel_id  TEXT NOT NULL                -- YouTube UC… id (P0 A1)
);
INSERT OR IGNORE INTO video_sources (id, name, channel_id) VALUES
  ('jordi', 'Jordi Visser',   'UCSLOw8JrFTBb3qF-p4v0v_w'),
  ('cowen', 'Benjamin Cowen', 'UCRvqjQPSeaWn-uEx-w0XOIg');

-- One row per video package.
CREATE TABLE IF NOT EXISTS video_digests (
  id                          INTEGER PRIMARY KEY,
  source                      TEXT NOT NULL REFERENCES video_sources(id),
  video_id                    TEXT NOT NULL UNIQUE,     -- YouTube video id
  title                       TEXT NOT NULL DEFAULT '',
  url                         TEXT NOT NULL DEFAULT '',
  published_at                TEXT NOT NULL,            -- YYYY-MM-DD (upload date)
  is_flagship                 INTEGER NOT NULL DEFAULT 0,
  duration_sec                INTEGER,
  transcript_method           TEXT,                     -- 'captions' | 'whisper'
  transcript_caption_track    TEXT,                     -- e.g. 'en-orig'
  pipeline_version            TEXT,
  package_path                TEXT NOT NULL,            -- on-disk package dir it was read from
  executive_summary           TEXT,
  themes                      TEXT,                     -- JSON array of strings
  theme_changes_vs_prior_week TEXT,                     -- JSON array; Jordi only, NULL for Cowen
  prior_digest_id             INTEGER,                  -- digest used as prior-week context (no FK: survives force re-ingest)
  regime_read                 TEXT,                     -- free text, both sources
  suggested_regime            TEXT,                     -- reserved; NOT rendered until P3 (D7)
  llm_model                   TEXT,
  llm_cost_usd                REAL,
  ingested_at                 INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
CREATE INDEX IF NOT EXISTS idx_video_digests_source_pub ON video_digests(source, published_at);

CREATE TABLE IF NOT EXISTS video_mentions (
  id           INTEGER PRIMARY KEY,
  digest_id    INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  name         TEXT NOT NULL,
  type         TEXT,                        -- 'stock'|'etf'|'crypto'|'index'|'macro'
  context      TEXT,                        -- one sentence
  stance       TEXT,                        -- 'bullish'|'bearish'|'neutral'|'watching'
  ts_sec       INTEGER,                     -- from the timed transcript; nullable
  in_universe  INTEGER,                     -- P3 join vs nexus_universe; NULL in P1
  held         INTEGER,                     -- P3; NULL in P1
  watchlist    INTEGER                      -- P3; NULL in P1
);
CREATE INDEX IF NOT EXISTS idx_video_mentions_digest ON video_mentions(digest_id);

CREATE TABLE IF NOT EXISTS video_quotes (
  id          INTEGER PRIMARY KEY,
  digest_id   INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  quote_text  TEXT NOT NULL,               -- max 2 per digest (enforced at extraction)
  ts_sec      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_video_quotes_digest ON video_quotes(digest_id);

CREATE TABLE IF NOT EXISTS video_frames (
  id            INTEGER PRIMARY KEY,
  digest_id     INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  file_path     TEXT NOT NULL,             -- absolute path under /var/lib/video_digest — never a blob
  ts_sec        INTEGER,
  origin        TEXT,                      -- 'scene' | 'cue'
  cue_text      TEXT,
  frame_params  TEXT                       -- JSON, copied from the package meta.json
);
CREATE INDEX IF NOT EXISTS idx_video_frames_digest ON video_frames(digest_id);

-- One row per Cowen digest. (b) columns come from the P1 LLM call — fields he
-- demonstrably states. (c) columns are NEVER requested from the LLM (not reliably
-- spoken); they stay NULL until P2 frame OCR populates them.
CREATE TABLE IF NOT EXISTS cowen_weekly_snapshot (
  digest_id             INTEGER PRIMARY KEY REFERENCES video_digests(id) ON DELETE CASCADE,
  -- (b) LLM-extracted
  risk_indicator_band   TEXT,              -- his own wording, not an enum
  btc_vs_200dma         TEXT,              -- 'above' | 'below' | NULL
  mvrv_z_band           TEXT,
  cycle_phase           TEXT,              -- as stated; NULL if not stated
  macro_cpi_print       TEXT,
  macro_fed_posture     TEXT,
  macro_recession_flag  INTEGER,           -- 1 | 0 | NULL
  confidence            TEXT,              -- 'high'|'medium'|'low'|'unstated'
  -- (c) P2 frame-OCR only — never set by the P1 ingest
  btc_vs_200wma         TEXT,
  log_band_third        TEXT,              -- 'L' | 'M' | 'U' | NULL
  btc_dominance_level   TEXT,
  btc_dominance_trend   TEXT,
  eth_btc_level         TEXT,
  eth_btc_trend         TEXT
);
