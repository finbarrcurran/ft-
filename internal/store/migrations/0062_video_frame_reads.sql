-- SC-54 — what a vision model read off each chart frame. A re-read ADDS a row, never overwrites one.
-- Values are display-only ("from chart, unverified") and never feed a signal.
CREATE TABLE IF NOT EXISTS video_frame_reads (
  id              INTEGER PRIMARY KEY,
  digest_id       INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  frame_id        INTEGER REFERENCES video_frames(id) ON DELETE SET NULL,
  ts_sec          INTEGER NOT NULL,
  model           TEXT    NOT NULL,
  read_at         INTEGER NOT NULL,                 -- unix seconds
  series          TEXT,                             -- series or indicator name
  visible_values  TEXT,                             -- what is printed in the image, as read
  confidence      TEXT,                             -- high | medium | low
  cost_usd        REAL    NOT NULL DEFAULT 0        -- the share of the call this frame was in
);
CREATE INDEX IF NOT EXISTS idx_vfr_digest ON video_frame_reads(digest_id, ts_sec);

-- One row per read pass over a digest: how it ended.
CREATE TABLE IF NOT EXISTS video_frame_runs (
  id          INTEGER PRIMARY KEY,
  digest_id   INTEGER NOT NULL REFERENCES video_digests(id) ON DELETE CASCADE,
  model       TEXT    NOT NULL,
  run_at      INTEGER NOT NULL,
  status      TEXT    NOT NULL,                     -- done | skipped | partial | failed
  reason      TEXT,                                 -- why frames were skipped ("not public", "daily sub-cap", ...)
  frames      INTEGER NOT NULL DEFAULT 0,           -- frames sent
  calls       INTEGER NOT NULL DEFAULT 0,
  reads       INTEGER NOT NULL DEFAULT 0,           -- rows stored
  cost_usd    REAL    NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_vfruns_digest ON video_frame_runs(digest_id);
