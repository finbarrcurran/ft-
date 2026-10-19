-- Migration 0059 — SC-52 P6: flag regime-history rows that must not seed the weekly snapshot's
-- history. The rows stay; the regime history view and the audit trail are untouched. Only
-- snapshot seeding (internal/server/snapshot_seed.go) skips flagged rows.
--
-- Flagged here: the 2026-05-16 smoke-test and reset rows (note 'smoke test' / 'reset', all four at
-- one timestamp: Jordi and Cowen, 'defensive' from test inputs and 'unclassified' resets). The June
-- 2026 Jordi flips are NOT flagged here: they need Fin's confirmation that they were not real
-- changes first (`ft regime-history flag` does it afterwards).

ALTER TABLE regime_history ADD COLUMN seed_excluded INTEGER NOT NULL DEFAULT 0;

UPDATE regime_history
   SET seed_excluded = 1
 WHERE ts = 1778945869 AND note IN ('smoke test', 'reset');
