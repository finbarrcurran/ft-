-- v1.11.0 — Spec 9k Phase B: seed committee→sector + EO keyword maps.
--
-- Idempotent: clears existing rows first then re-seeds. The map is
-- small (~50 rows total) and re-seeding on every run lets us tune
-- the jurisdiction set without manual DB surgery.

DELETE FROM committee_sector_map;
DELETE FROM eo_sector_keywords;

-- ---------- committee_sector_map ----------------------------------
-- Columns: committee_code, sector_universe_id, alarm_strength, notes
-- notes carries the human-readable committee name so the UI can
-- render the chamber + committee that triggered an ALARM.

-- House Armed Services → Defense
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsas', id, 'strong', 'House Armed Services'
  FROM sector_universe WHERE code IN ('defense_sovereign');

-- Senate Armed Services → Defense
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssas', id, 'strong', 'Senate Armed Services'
  FROM sector_universe WHERE code IN ('defense_sovereign');

-- House Energy & Commerce → Pharma, Utilities, Comms, Healthcare
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsif', id, 'strong', 'House Energy & Commerce'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare',
                'gics_utilities','gics_comm_services');

-- Senate Commerce, Science & Transp. → Comms, Industrials, Semis
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'sscom', id, 'strong', 'Senate Commerce, Science & Transportation'
  FROM sector_universe
 WHERE code IN ('gics_comm_services','gics_industrials',
                'chip_design_ip','foundry','semicap','specialty_semi_chem');

-- Senate Banking → Financials, Real Estate
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssbk', id, 'strong', 'Senate Banking'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

-- House Financial Services → Financials, Real Estate
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsba', id, 'strong', 'House Financial Services'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

-- Senate Energy & Natural Resources → Energy, Nuclear, Utilities, Materials
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssen', id, 'strong', 'Senate Energy & Natural Resources'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','power_nuclear_smr',
                'power_distributed','power_diversified','power_gas_turbines',
                'gics_utilities','gics_materials','grid_transmission',
                'battery_storage');

-- House Natural Resources → Energy, Materials
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsii', id, 'strong', 'House Natural Resources'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','gics_materials',
                'precious_metals_gold','precious_metals_silver');

-- House Agriculture → Consumer Staples, Materials
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsag', id, 'moderate', 'House Agriculture'
  FROM sector_universe
 WHERE code IN ('gics_consumer_stap','gics_materials');

-- Senate HELP → Pharma, Healthcare
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'sshr', id, 'strong', 'Senate Health, Education, Labor & Pensions'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- House Veterans' Affairs → Pharma, Healthcare
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsvr', id, 'moderate', 'House Veterans Affairs'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- House Intel → Defense, IT
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsii_perm', id, 'moderate', 'House Permanent Select Committee on Intelligence'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

-- Senate Intel → Defense, IT
INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'slin', id, 'moderate', 'Senate Select Committee on Intelligence'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

-- ---------- eo_sector_keywords ------------------------------------

-- Defense
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('defense procurement'),('munitions'),('shipbuilding'),
                   ('drone'),('autonomous weapons'),('hypersonic'),
                   ('national defense'),('defense industrial base'),
                   ('NDAA'),('replicator')) AS kws(kw)
WHERE code = 'defense_sovereign';

-- Semiconductors / chip stack
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('semiconductor'),('CHIPS Act'),('fab'),
                   ('export control'),('BIS entity list'),
                   ('advanced node')) AS kws(kw)
WHERE code IN ('chip_design_ip','foundry','semicap','specialty_semi_chem',
               'hbm_packaging','edge_industrial_silicon');

-- AI / GPUs
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('artificial intelligence'),('AI executive order'),
                   ('compute export'),('frontier model'),('AI safety')) AS kws(kw)
WHERE code IN ('gpus_ai_accel','chip_design_ip');

-- Energy / hydrocarbons
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('oil'),('natural gas'),('drilling'),('permian'),
                   ('LNG'),('pipeline'),('ANWR'),
                   ('strategic petroleum reserve'),('SPR release')) AS kws(kw)
WHERE code IN ('gics_energy','oil_gas_integrated');

-- Nuclear / SMR
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('nuclear'),('SMR'),('small modular reactor'),
                   ('uranium'),('enrichment'),('reactor')) AS kws(kw)
WHERE code = 'power_nuclear_smr';

-- Power / grid
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('grid'),('transmission'),('electric reliability'),
                   ('FERC'),('peak demand')) AS kws(kw)
WHERE code IN ('grid_transmission','gics_utilities','power_gas_turbines',
               'power_diversified');

-- Pharma
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('drug pricing'),('Medicare negotiation'),
                   ('biosimilar'),('IRA pharma'),('340B'),
                   ('prescription drug')) AS kws(kw)
WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- Critical minerals / materials
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('critical minerals'),('rare earth'),('lithium'),
                   ('cobalt'),('nickel'),('Defense Production Act'),
                   ('domestic mining')) AS kws(kw)
WHERE code IN ('gics_materials','battery_storage','precious_metals_gold',
               'precious_metals_silver');

-- Comms / 5G / TikTok
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('5G'),('open RAN'),('Huawei'),('TikTok'),
                   ('foreign adversary'),('connected vehicle')) AS kws(kw)
WHERE code IN ('gics_comm_services','optical_networking');

-- Real estate / data center
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('data center'),('hyperscale')) AS kws(kw)
WHERE code IN ('data_center_reits','gics_real_estate');

-- Software / IT / cloud
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT lower(kw), id FROM sector_universe
CROSS JOIN (VALUES ('JWCC'),('FedRAMP'),('sovereign cloud'),
                   ('federal cloud')) AS kws(kw)
WHERE code = 'gics_it';
