-- Migration 0034 — stock-side sector adapter sub-type tagging
--
-- Adds `sector_adapter_subtype TEXT NULL` column to `stock_holdings` per
-- Claude.ai's response 2026-05-31 to Claude Code's three scope questions on
-- the stock_holdings re-tag blocker.
--
-- Stores kebab-case composite string `<sector_scorecards.code>:<subtype>`
-- examples:
--   cloud-infra:cloud-software-layer
--   industrial-electrical:diversified-ie
--   asset-hedge:gold-physical
--   asset-hedge:silver-physical
--   ai-infra-semi:custom-silicon-asic
--   pharma:glp1-pure-play
--
-- Design rationale per Claude.ai's answers:
--   Q1: sector_universe stays (load-bearing for Spec 9f rotation tracker +
--       sector_snapshots + heatmap + user_sector_ordering +
--       sector_rotation_digests). Not deprecatable without major migration.
--   Q2: Option R (Retain both). Two taxonomies serve orthogonal purposes —
--       sector_universe is "where it rotates" (34-bucket granular rotation
--       taxonomy); sector_scorecards is "how it's analyzed" (coarser adapter
--       framework). Adapter-to-rotation is naturally one-to-many.
--   Q3: Migration 0034 acceptable; Option B.ii — TEXT column not FK; no
--       separate sub-types table; multi-segment stays single-primary-tag
--       per Defense v1.1 §B5 dominant-segment-by-GP convention. First-class
--       multi-tag is Spec 9i territory (don't pre-bake).
--
-- The 'escalate trigger' framing from crypto-side patches was domain-specific
-- discipline, not universal. Stock-side adding a single TEXT column is
-- lightweight.
--
-- CHECK constraint: format must contain a colon separator when non-null.
-- Conservative regex — allows kebab-case slug on both sides of the colon.
-- Falls back to "anything with a colon" for safety; downstream code is the
-- canonical validator.

ALTER TABLE stock_holdings
  ADD COLUMN sector_adapter_subtype TEXT NULL
    CHECK (sector_adapter_subtype IS NULL OR sector_adapter_subtype LIKE '%-%:%');

CREATE INDEX idx_stock_holdings_sector_adapter_subtype
  ON stock_holdings(sector_adapter_subtype)
  WHERE sector_adapter_subtype IS NOT NULL;
