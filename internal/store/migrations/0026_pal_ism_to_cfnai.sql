-- v1.9.1 — Replace pal_ism (manual JSON upload) with pal_cfnai
-- (Chicago Fed National Activity Index, auto-fetched from FRED).
--
-- Same monthly cadence as ISM (~22nd of each month), same business-
-- cycle pulse signal Pal uses ISM for. CFNAI is on FRED with a free
-- API and never licensing-disputed.
--
-- Scoring scale differs from ISM (CFNAI is signed around 0; ISM is
-- 0-100 around 50); thresholds in the JSON definition reflect this.
-- The bucket weight (Pal 20%) is unchanged.
--
-- Snapshot rows for pal_ism become orphaned but harmless — kept for
-- historical comparison if anyone goes spelunking.

DELETE FROM crypto_indicators WHERE id = 'pal_ism';

INSERT OR IGNORE INTO crypto_indicators (id, bucket, display_name, unit, source) VALUES
  ('pal_cfnai', 'pal', 'Business cycle (CFNAI)', 'index', 'fred');
