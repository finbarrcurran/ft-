-- 0003_crypto_is_core.sql
--
-- Adds `is_core` to crypto_holdings for the Spec 2 Crypto Core/Alt donut and,
-- ahead of Spec 3, a manual user-editable "core" flag in the edit modal.
--
-- Redundant with the existing `classification` TEXT column for now — both
-- agree on the same rows (BTC/ETH = core). The point of a separate boolean
-- is to decouple "is this a core holding?" from any future classification
-- enum expansion, and to make the edit-modal UI a clean checkbox.

ALTER TABLE crypto_holdings ADD COLUMN is_core INTEGER NOT NULL DEFAULT 0;

-- Seed: BTC and ETH are core. Matches the existing classification column.
UPDATE crypto_holdings SET is_core = 1 WHERE symbol IN ('BTC', 'ETH');
