-- Migration 0040 — expand crypto_adapters from 8 → 12 functional types
--
-- Spec 9l Crypto Adapter Expansion v1 (Cover Note, 2026-06-01) extends the
-- adapter repository to full-category coverage by adding four new adapters:
--
--   9.  stablecoin     — /10 safety screen (NEW scorecard_type 'safety_10')
--   10. privacy        — alt_18, RDR gate
--   11. cefi-exchange  — alt_18, CCR gate
--   12. ai-agent       — alt_18, RAUR gate (provisional)
--
-- Two CHECK constraints must be relaxed to admit these:
--   - adapter_type IN (... + 'stablecoin','privacy','cefi-exchange','ai-agent')
--   - scorecard_type IN (... + 'safety_10')
--
-- SQLite cannot ALTER a CHECK constraint, so the table is rebuilt in place
-- (same approach as migration 0035). All 8 existing rows + IDs are preserved
-- so the FK references from crypto_theses (primary/secondary_adapter_id) and
-- crypto_adapter_versions (adapter_id) stay intact. foreign_keys is disabled
-- for the DROP/RENAME so those FK-referencing rows survive the swap.
--
-- This migration only relaxes the constraints. The four new placeholder rows
-- are inserted at boot by cryptotheses.SeedIfEmpty (idempotent, by slug),
-- exactly like the original 8; their real MD bodies are filled afterwards by
-- the content seeder. They land as status='draft' (uncalibrated templates) —
-- no thesis may be locked on adapters 9–12 until first-use calibration.
--
-- legacy_alter_table = ON is required here (unlike migration 0035): the
-- crypto_theses BEFORE INSERT/UPDATE triggers from migration 0032
-- (trg_speculative_horizon_{insert,update}) reference crypto_adapters in their
-- bodies. With the modern (OFF) behaviour, the DROP/RENAME re-parses every
-- trigger body and aborts with "no such table: main.crypto_adapters" mid-swap.
-- legacy_alter_table = ON disables that re-parse so the rebuild completes; the
-- triggers keep referencing crypto_adapters by name (unchanged final name).

PRAGMA foreign_keys = OFF;
PRAGMA legacy_alter_table = ON;

BEGIN TRANSACTION;

-- 1. Rebuild crypto_adapters with the relaxed CHECK constraints.
CREATE TABLE crypto_adapters_new (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    slug                TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL,
    short_description   TEXT NOT NULL,
    adapter_type        TEXT NOT NULL CHECK (adapter_type IN (
                            'btc','l1','l2','defi','infra','depin','rwa','speculative',
                            'stablecoin','privacy','cefi-exchange','ai-agent')),
    scorecard_type      TEXT NOT NULL CHECK (scorecard_type IN (
                            'alt_18','monetary_12','safety_10')),
    current_version     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','locked','needs-review')),
    markdown_current    TEXT NOT NULL,
    rendered_html       TEXT NOT NULL DEFAULT '',
    primary_data_sources TEXT NOT NULL DEFAULT '[]',
    kill_criteria_json  TEXT NOT NULL DEFAULT '[]',
    is_doctrine         INTEGER NOT NULL DEFAULT 0,
    github_path         TEXT,
    github_url          TEXT,
    file_sha            TEXT,
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    locked_at           INTEGER
);

-- 2. Copy all existing rows (IDs preserved).
INSERT INTO crypto_adapters_new
SELECT id, slug, display_name, short_description, adapter_type, scorecard_type,
       current_version, status, markdown_current, rendered_html,
       primary_data_sources, kill_criteria_json, is_doctrine,
       github_path, github_url, file_sha, created_at, updated_at, locked_at
  FROM crypto_adapters;

-- 3. Swap.
DROP TABLE crypto_adapters;
ALTER TABLE crypto_adapters_new RENAME TO crypto_adapters;

-- 4. Re-create indexes from 0031.
CREATE INDEX idx_crypto_adapters_slug   ON crypto_adapters(slug);
CREATE INDEX idx_crypto_adapters_status ON crypto_adapters(status);

COMMIT;

PRAGMA legacy_alter_table = OFF;
PRAGMA foreign_keys = ON;
