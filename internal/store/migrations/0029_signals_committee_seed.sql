-- v1.11.0 — Spec 9k Phase B: seed committee→sector + EO keyword maps.
--
-- committee_sector_map: 13 committees with material economic
-- jurisdiction over portfolio sectors. Codes match the canonical
-- Library of Congress committee codes used by the
-- unitedstates/congress-legislators dataset.
--
-- eo_sector_keywords: ~50 starter keywords. Keep narrow — false
-- positives are mitigated by the "must intersect held sector" gate
-- in the tier compute. Easy to extend.

-- Drop both tables' existing rows so the seed is idempotent — the
-- map is small and we'd rather have a clean re-seed each migration
-- run while we tune it.
DELETE FROM committee_sector_map;
DELETE FROM eo_sector_keywords;

-- ---------- committee_sector_map ----------------------------------
-- One row per (committee, sector). Multiple sector rows per
-- committee are expected.

-- Helper INSERTs use sub-selects so we don't hard-code sector_universe.id.

-- House Armed Services → Defense, Aerospace
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsas', 'House Armed Services', id, 'strong'
  FROM sector_universe WHERE code IN ('defense_sovereign');

-- Senate Armed Services → Defense
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'ssas', 'Senate Armed Services', id, 'strong'
  FROM sector_universe WHERE code IN ('defense_sovereign');

-- House Energy & Commerce → Pharma, Utilities, Comms, Healthcare
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsif', 'House Energy & Commerce', id, 'strong'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare',
                'gics_utilities','gics_comm_services');

-- Senate Commerce, Science & Transp. → Comms, Transport (Industrials), Semis
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'sscom', 'Senate Commerce, Science & Transportation', id, 'strong'
  FROM sector_universe
 WHERE code IN ('gics_comm_services','gics_industrials',
                'chip_design_ip','foundry','semicap','specialty_semi_chem');

-- Senate Banking → Financials, Real Estate
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'ssbk', 'Senate Banking', id, 'strong'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

-- House Financial Services → Financials, Real Estate
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsba', 'House Financial Services', id, 'strong'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

-- Senate Energy & Natural Resources → Energy, Nuclear, Utilities, Materials
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'ssen', 'Senate Energy & Natural Resources', id, 'strong'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','power_nuclear_smr',
                'power_distributed','power_diversified','power_gas_turbines',
                'gics_utilities','gics_materials','grid_transmission',
                'battery_storage');

-- House Natural Resources → Energy, Materials
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsii', 'House Natural Resources', id, 'strong'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','gics_materials',
                'precious_metals_gold','precious_metals_silver');

-- House Agriculture → Consumer Staples, Materials
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsag', 'House Agriculture', id, 'moderate'
  FROM sector_universe
 WHERE code IN ('gics_consumer_stap','gics_materials');

-- Senate HELP → Pharma, Healthcare
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'sshr', 'Senate Health, Education, Labor & Pensions', id, 'strong'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- House Veterans' Affairs → Pharma, Healthcare
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsvr', 'House Veterans Affairs', id, 'moderate'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- House Permanent Select on Intelligence → Defense, IT
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'hsii_perm', 'House Permanent Select Committee on Intelligence', id, 'moderate'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

-- Senate Intelligence → Defense, IT
INSERT INTO committee_sector_map (committee_code, committee_name, sector_universe_id, strength)
SELECT 'slin', 'Senate Select Committee on Intelligence', id, 'moderate'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

-- ---------- eo_sector_keywords ------------------------------------
-- Each row maps a lowercase keyword/phrase to a sector_universe.id.
-- Substring match against EO title + abstract (case-insensitive).

-- Defense
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('defense procurement'),('munitions'),('shipbuilding'),
                   ('drone'),('autonomous weapons'),('hypersonic'),
                   ('national defense'),('defense industrial base'),
                   ('NDAA'),('replicator'))
WHERE code = 'defense_sovereign';

-- Semiconductors / chip stack
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('semiconductor'),('CHIPS Act'),('fab'),
                   ('export control'),('BIS entity list'),
                   ('foundry'),('advanced node'))
WHERE code IN ('chip_design_ip','foundry','semicap','specialty_semi_chem',
               'hbm_packaging','edge_industrial_silicon');

-- AI / GPUs
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('artificial intelligence'),('AI executive order'),
                   ('compute export'),('frontier model'),('AI safety'))
WHERE code IN ('gpus_ai_accel','chip_design_ip');

-- Energy / hydrocarbons
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('oil'),('natural gas'),('drilling'),('permian'),
                   ('LNG'),('pipeline'),('ANWR'),
                   ('strategic petroleum reserve'),('SPR release'))
WHERE code IN ('gics_energy','oil_gas_integrated');

-- Nuclear / SMR
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('nuclear'),('SMR'),('small modular reactor'),
                   ('uranium'),('enrichment'),('reactor'))
WHERE code = 'power_nuclear_smr';

-- Power / grid
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('grid'),('transmission'),('electric reliability'),
                   ('FERC'),('peak demand'))
WHERE code IN ('grid_transmission','gics_utilities','power_gas_turbines',
               'power_diversified');

-- Pharma
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('drug pricing'),('Medicare negotiation'),
                   ('biosimilar'),('IRA pharma'),('340B'),
                   ('prescription drug'))
WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- Critical minerals / materials
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('critical minerals'),('rare earth'),('lithium'),
                   ('cobalt'),('nickel'),('Defense Production Act'),
                   ('domestic mining'))
WHERE code IN ('gics_materials','battery_storage','precious_metals_gold',
               'precious_metals_silver');

-- Comms / 5G / TikTok
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('5G'),('open RAN'),('Huawei'),('TikTok'),
                   ('foreign adversary'),('connected vehicle'))
WHERE code IN ('gics_comm_services','optical_networking');

-- Real estate / data center
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('data center'),('hyperscale'))
WHERE code IN ('data_center_reits','gics_real_estate');

-- Software / IT / cloud
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('JWCC'),('FedRAMP'),('sovereign cloud'),
                   ('federal cloud'))
WHERE code = 'gics_it';
