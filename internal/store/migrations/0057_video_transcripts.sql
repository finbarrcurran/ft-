-- Migration 0057 — SC-52 P2: full transcript store + full-text search.
--
-- One row per video (captions only; no Whisper). `text` is the compact timed transcript the
-- extraction already uses ("[NNNs] words ..." with a marker about every 30 s), so a hit can be
-- turned into a timestamped link. A row exists whether or not the video has been digested, so
-- the corpus can be searched before (or without) any LLM extraction. The FTS5 index is an
-- external-content table kept in step by triggers; a plain `.backup` of the database carries
-- both. No LLM spend anywhere in this path.

CREATE TABLE IF NOT EXISTS video_transcripts (
  id           INTEGER PRIMARY KEY,
  video_id     TEXT    NOT NULL UNIQUE,      -- YouTube video id
  source       TEXT    NOT NULL,             -- jordi | cowen
  channel      TEXT    NOT NULL DEFAULT '',  -- display name
  title        TEXT    NOT NULL DEFAULT '',
  url          TEXT    NOT NULL DEFAULT '',
  published_at TEXT    NOT NULL,             -- YYYY-MM-DD
  duration_sec INTEGER,
  method       TEXT    NOT NULL DEFAULT 'captions',   -- captions | whisper
  text         TEXT    NOT NULL,
  text_sha     TEXT    NOT NULL,             -- sha256 of text: unchanged re-syncs write nothing
  fetched_at   INTEGER NOT NULL              -- unix seconds
);
CREATE INDEX IF NOT EXISTS idx_video_transcripts_src_pub ON video_transcripts(source, published_at);

CREATE VIRTUAL TABLE IF NOT EXISTS video_transcripts_fts USING fts5(
  text, content='video_transcripts', content_rowid='id', tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS video_transcripts_ai AFTER INSERT ON video_transcripts BEGIN
  INSERT INTO video_transcripts_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS video_transcripts_ad AFTER DELETE ON video_transcripts BEGIN
  INSERT INTO video_transcripts_fts(video_transcripts_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS video_transcripts_au AFTER UPDATE ON video_transcripts BEGIN
  INSERT INTO video_transcripts_fts(video_transcripts_fts, rowid, text) VALUES ('delete', old.id, old.text);
  INSERT INTO video_transcripts_fts(rowid, text) VALUES (new.id, new.text);
END;
