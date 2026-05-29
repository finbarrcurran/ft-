-- 0032_crypto_theses_speculative_horizon.sql — Spec 9l v0.3 patch.
--
-- Closes the Phase 0 confirmation point #4 schema gap: enforces the
-- Speculative adapter's horizon restriction at the DB level.
--
-- Per Speculative_adapter_v1.md §6 Guardrail 2:
--   "Speculative theses CANNOT be set to Never-Sell, Cycle, or Multi-year
--    horizon. Allowed horizons: Trade (<3mo, default for memes) or Medium
--    (3-12mo, allowed for stronger Speculative theses with multi-cycle
--    narrative resilience)."
--
-- Implementation note: SQLite CHECK constraints can't reference other
-- tables (we'd need to JOIN to crypto_adapters.adapter_type to know
-- whether the row is speculative). A BEFORE INSERT / BEFORE UPDATE
-- trigger pair is the canonical approach. RAISE(ABORT, ...) emits a
-- clear error that the API layer can match on and translate to 422.
--
-- The trigger pair fires on:
--   1. INSERT into crypto_theses
--   2. UPDATE OF primary_adapter_id, holding_horizon on crypto_theses
--      (only if those columns actually change — UPDATE OF clause optimizes)
--
-- TBD horizon is allowed even for Speculative (it's "defer decision";
-- thesis cannot be locked until a real horizon is set; locked-at NULL
-- enforces that at the API layer). 'tbd' is therefore NOT rejected here.

------------------------------------------------------------------------
-- BEFORE INSERT trigger
------------------------------------------------------------------------
CREATE TRIGGER trg_speculative_horizon_insert
BEFORE INSERT ON crypto_theses
FOR EACH ROW
WHEN NEW.holding_horizon IN ('never_sell', 'cycle', 'multi_year')
 AND EXISTS (
       SELECT 1 FROM crypto_adapters
       WHERE id = NEW.primary_adapter_id
         AND adapter_type = 'speculative'
     )
BEGIN
    SELECT RAISE(ABORT,
      'Spec 9l: Speculative theses cannot be locked at Never-Sell, Cycle, or Multi-year horizon. Allowed: Trade or Medium.');
END;

------------------------------------------------------------------------
-- BEFORE UPDATE trigger
------------------------------------------------------------------------
CREATE TRIGGER trg_speculative_horizon_update
BEFORE UPDATE OF primary_adapter_id, holding_horizon ON crypto_theses
FOR EACH ROW
WHEN NEW.holding_horizon IN ('never_sell', 'cycle', 'multi_year')
 AND EXISTS (
       SELECT 1 FROM crypto_adapters
       WHERE id = NEW.primary_adapter_id
         AND adapter_type = 'speculative'
     )
BEGIN
    SELECT RAISE(ABORT,
      'Spec 9l: Speculative theses cannot be moved to Never-Sell, Cycle, or Multi-year horizon. Allowed: Trade or Medium.');
END;
