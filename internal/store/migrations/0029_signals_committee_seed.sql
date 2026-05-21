-- v1.11.0 — Spec 9k Phase B: seed committee→sector + EO keyword maps.
--
-- Idempotent: clears existing rows first then re-seeds.

DELETE FROM committee_sector_map;
DELETE FROM eo_sector_keywords;

-- ---------- committee_sector_map ----------------------------------
-- columns: committee_code, sector_universe_id, alarm_strength, notes
-- notes carries the human-readable committee name.

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsas', id, 'strong', 'House Armed Services'
  FROM sector_universe WHERE code = 'defense_sovereign';

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssas', id, 'strong', 'Senate Armed Services'
  FROM sector_universe WHERE code = 'defense_sovereign';

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsif', id, 'strong', 'House Energy & Commerce'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare',
                'gics_utilities','gics_comm_services');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'sscom', id, 'strong', 'Senate Commerce, Science & Transportation'
  FROM sector_universe
 WHERE code IN ('gics_comm_services','gics_industrials',
                'chip_design_ip','foundry','semicap','specialty_semi_chem');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssbk', id, 'strong', 'Senate Banking'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsba', id, 'strong', 'House Financial Services'
  FROM sector_universe
 WHERE code IN ('gics_financials','gics_real_estate','data_center_reits');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'ssen', id, 'strong', 'Senate Energy & Natural Resources'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','power_nuclear_smr',
                'power_distributed','power_diversified','power_gas_turbines',
                'gics_utilities','gics_materials','grid_transmission',
                'battery_storage');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsii', id, 'strong', 'House Natural Resources'
  FROM sector_universe
 WHERE code IN ('gics_energy','oil_gas_integrated','gics_materials',
                'precious_metals_gold','precious_metals_silver');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsag', id, 'moderate', 'House Agriculture'
  FROM sector_universe
 WHERE code IN ('gics_consumer_stap','gics_materials');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'sshr', id, 'strong', 'Senate Health, Education, Labor & Pensions'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsvr', id, 'moderate', 'House Veterans Affairs'
  FROM sector_universe
 WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'hsii_perm', id, 'moderate', 'House Permanent Select Committee on Intelligence'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

INSERT INTO committee_sector_map (committee_code, sector_universe_id, alarm_strength, notes)
SELECT 'slin', id, 'moderate', 'Senate Select Committee on Intelligence'
  FROM sector_universe
 WHERE code IN ('defense_sovereign','gics_it','chip_design_ip');

-- ---------- eo_sector_keywords ------------------------------------
-- Defense
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'defense procurement', id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'munitions',           id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'shipbuilding',        id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'drone',               id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'autonomous weapons',  id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'hypersonic',          id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'national defense',    id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'defense industrial base', id FROM sector_universe WHERE code='defense_sovereign';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'ndaa',                id FROM sector_universe WHERE code='defense_sovereign';

-- Semiconductors
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'semiconductor', id FROM sector_universe
 WHERE code IN ('chip_design_ip','foundry','semicap','specialty_semi_chem','hbm_packaging','edge_industrial_silicon');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'chips act', id FROM sector_universe
 WHERE code IN ('chip_design_ip','foundry','semicap','specialty_semi_chem','hbm_packaging');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'export control', id FROM sector_universe
 WHERE code IN ('chip_design_ip','foundry','semicap');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'bis entity list', id FROM sector_universe
 WHERE code IN ('chip_design_ip','foundry','semicap');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'advanced node', id FROM sector_universe
 WHERE code IN ('chip_design_ip','foundry','semicap');

-- AI / GPUs
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'artificial intelligence', id FROM sector_universe WHERE code IN ('gpus_ai_accel','chip_design_ip');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'ai executive order', id FROM sector_universe WHERE code IN ('gpus_ai_accel','chip_design_ip');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'compute export', id FROM sector_universe WHERE code IN ('gpus_ai_accel','chip_design_ip');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'frontier model', id FROM sector_universe WHERE code IN ('gpus_ai_accel');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'ai safety', id FROM sector_universe WHERE code IN ('gpus_ai_accel');

-- Energy / hydrocarbons
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'oil',                       id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'natural gas',               id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'drilling',                  id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'permian',                   id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'lng',                       id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'pipeline',                  id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'anwr',                      id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'strategic petroleum reserve', id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'spr release',               id FROM sector_universe WHERE code IN ('gics_energy','oil_gas_integrated');

-- Nuclear / SMR
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'nuclear',              id FROM sector_universe WHERE code='power_nuclear_smr';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'smr',                  id FROM sector_universe WHERE code='power_nuclear_smr';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'small modular reactor', id FROM sector_universe WHERE code='power_nuclear_smr';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'uranium',              id FROM sector_universe WHERE code='power_nuclear_smr';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'enrichment',           id FROM sector_universe WHERE code='power_nuclear_smr';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'reactor',              id FROM sector_universe WHERE code='power_nuclear_smr';

-- Power / grid
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'grid', id FROM sector_universe WHERE code IN ('grid_transmission','gics_utilities','power_gas_turbines','power_diversified');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'transmission', id FROM sector_universe WHERE code IN ('grid_transmission','gics_utilities');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'electric reliability', id FROM sector_universe WHERE code IN ('grid_transmission','gics_utilities');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'ferc', id FROM sector_universe WHERE code IN ('grid_transmission','gics_utilities');

-- Pharma
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'drug pricing', id FROM sector_universe WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'medicare negotiation', id FROM sector_universe WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'biosimilar', id FROM sector_universe WHERE code IN ('pharma_immunology','pharma_metabolic');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'ira pharma', id FROM sector_universe WHERE code IN ('pharma_immunology','pharma_metabolic');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'prescription drug', id FROM sector_universe WHERE code IN ('pharma_immunology','pharma_metabolic','gics_healthcare');

-- Critical minerals
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'critical minerals', id FROM sector_universe WHERE code IN ('gics_materials','battery_storage','precious_metals_gold','precious_metals_silver');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'rare earth', id FROM sector_universe WHERE code IN ('gics_materials','battery_storage');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'lithium', id FROM sector_universe WHERE code IN ('gics_materials','battery_storage');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'cobalt', id FROM sector_universe WHERE code IN ('gics_materials','battery_storage');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'defense production act', id FROM sector_universe WHERE code IN ('gics_materials','battery_storage');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'domestic mining', id FROM sector_universe WHERE code IN ('gics_materials','precious_metals_gold','precious_metals_silver');

-- Comms / 5G / TikTok
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT '5g', id FROM sector_universe WHERE code IN ('gics_comm_services','optical_networking');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'open ran', id FROM sector_universe WHERE code IN ('gics_comm_services','optical_networking');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'huawei', id FROM sector_universe WHERE code IN ('gics_comm_services','optical_networking');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'tiktok', id FROM sector_universe WHERE code IN ('gics_comm_services');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'foreign adversary', id FROM sector_universe WHERE code IN ('gics_comm_services');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'connected vehicle', id FROM sector_universe WHERE code IN ('gics_comm_services');

-- Data center
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'data center', id FROM sector_universe WHERE code IN ('data_center_reits','gics_real_estate');
INSERT INTO eo_sector_keywords (keyword, sector_universe_id)
SELECT 'hyperscale', id FROM sector_universe WHERE code IN ('data_center_reits','gics_real_estate');

-- Software / cloud
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'jwcc',           id FROM sector_universe WHERE code='gics_it';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'fedramp',        id FROM sector_universe WHERE code='gics_it';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'sovereign cloud',id FROM sector_universe WHERE code='gics_it';
INSERT INTO eo_sector_keywords (keyword, sector_universe_id) SELECT 'federal cloud',  id FROM sector_universe WHERE code='gics_it';
