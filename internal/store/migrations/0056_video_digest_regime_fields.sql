-- Migration 0056 — video digest regime extraction v2 (P0 of the 2026-10-19 handover).
--
-- The original extraction asked for a regime label with no FT definitions, so the
-- speaker's "regime shift" (a structural change) was read as FT "Shifting" (a tighter
-- risk posture). v2 extracts against FT's own definitions and stores the evidence.
--
-- video_digests.suggested_regime is LEGACY: kept as stored, never written by v2, never
-- shown in the UI. Digests re-extracted with --force carry the old value across.
-- Video-derived data stays context only (D6); proposed_regime is never auto-applied (D7).

ALTER TABLE video_digests ADD COLUMN extraction_version  INTEGER NOT NULL DEFAULT 1;
ALTER TABLE video_digests ADD COLUMN regime_narrative    TEXT;   -- 2-3 plain sentences
ALTER TABLE video_digests ADD COLUMN catalysts           TEXT;   -- JSON [{event, date|null}]
ALTER TABLE video_digests ADD COLUMN proposed_regime     TEXT;   -- stable|shifting|defensive|unclassified (lowercase, = regime pkg)
ALTER TABLE video_digests ADD COLUMN proposed_confidence TEXT;   -- high|medium|low
ALTER TABLE video_digests ADD COLUMN proposed_quote      TEXT;   -- verbatim evidence, grounded in the transcript
ALTER TABLE video_digests ADD COLUMN proposed_quote_ts   INTEGER;
ALTER TABLE video_digests ADD COLUMN cross_asset_tilt    TEXT;   -- equities|crypto|balanced|unstated — context only, not a regime state
ALTER TABLE video_digests ADD COLUMN cross_asset_quote   TEXT;
ALTER TABLE video_digests ADD COLUMN accumulation_stance TEXT;   -- accumulating|waiting|reducing|unstated
ALTER TABLE video_digests ADD COLUMN accumulation_quote  TEXT;
