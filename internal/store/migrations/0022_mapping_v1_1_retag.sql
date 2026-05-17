-- 0022_mapping_v1_1_retag.sql — Sector_Holdings_Mapping_v1.1 refresh-session locks.
--
-- Two retags vs the v1 mapping applied at migration 0019:
--
--   ORCL: gics_it → data_center_reits
--     Rationale: Oracle's OCI / AI-infra capex is the bull thesis driving
--     today's rotation read, not the legacy database revenue mix.
--
--   WPM: precious_metals_silver → precious_metals_gold
--     Rationale: Wheaton's streaming mix is ~55/45 silver-gold, and gold
--     has edged ahead in recent years. Tagging under gold is the more
--     honest primary classification.
--
-- Both updates are no-ops if the user has already manually retagged via
-- the Edit modal (the WHERE clause matches the v1 starting state).

-- ORCL
UPDATE stock_holdings
   SET sector_universe_id = (SELECT id FROM sector_universe WHERE code = 'data_center_reits'),
       updated_at = strftime('%s','now')
 WHERE ticker = 'ORCL'
   AND deleted_at IS NULL
   AND sector_universe_id = (SELECT id FROM sector_universe WHERE code = 'gics_it');

-- WPM
UPDATE stock_holdings
   SET sector_universe_id = (SELECT id FROM sector_universe WHERE code = 'precious_metals_gold'),
       updated_at = strftime('%s','now')
 WHERE ticker = 'WPM'
   AND deleted_at IS NULL
   AND sector_universe_id = (SELECT id FROM sector_universe WHERE code = 'precious_metals_silver');
