-- Migration 0076 — FT-146 / SC-80 (P3): links from what the commentators said to what FT holds or watches.
-- Deterministic (resolver ticker, exact alias name, exact symbol), no LLM, rebuilt in full by `ft video-links rebuild`
-- and nightly. Context only (D6): nothing in scoring, alerts or risk reads this table.
CREATE TABLE IF NOT EXISTS video_links (
  id          INTEGER PRIMARY KEY,
  source_kind TEXT    NOT NULL,            -- mention | item
  source_id   INTEGER NOT NULL,            -- video_mentions.id or video_items.id
  digest_id   INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  ticker      TEXT    NOT NULL,
  link_kind   TEXT    NOT NULL,            -- held | watchlist | universe (the strongest that applies)
  in_held     INTEGER NOT NULL DEFAULT 0,
  in_watch    INTEGER NOT NULL DEFAULT 0,
  method      TEXT    NOT NULL,            -- resolution | alias | symbol
  gate_class  TEXT,                        -- items only: grounded | wrong_place | far
  built_at    INTEGER NOT NULL,
  UNIQUE (source_kind, source_id, ticker)
);
CREATE INDEX IF NOT EXISTS idx_video_links_ticker ON video_links(ticker, digest_id);
CREATE INDEX IF NOT EXISTS idx_video_links_digest ON video_links(digest_id);
