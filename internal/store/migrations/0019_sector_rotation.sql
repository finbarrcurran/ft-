-- 0019_sector_rotation.sql — Spec 9f D1.
--
-- Cross-Sector Investment Philosophy v1.1 §4 — locked taxonomy of 34 rows
-- (17 AI-rotation sub-sectors + 6 independent-thesis sub-sectors + 11 GICS
-- top-level). Each row carries an ETF proxy used by the daily ingestion
-- in internal/sector_rotation/ingest.go.
--
-- Holdings backfill from Sector_Holdings_Mapping_v1.md: every existing
-- stock_holdings row gets tagged with one primary sector_universe_id.
-- ORCL defaults to "Information Technology" (gics_it) per the Software-vs-
-- DataCenter-REITs ambiguity called out in the mapping doc — user can
-- re-tag via the Edit modal once the UI lands.

-- ----- 1) Sector taxonomy ------------------------------------------------

CREATE TABLE sector_universe (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    code                 TEXT NOT NULL UNIQUE,
    display_name         TEXT NOT NULL,
    parent_gics          TEXT NOT NULL,
    jordi_stage          INTEGER NULL,
    rotation_thesis      TEXT NULL,
    etf_ticker_primary   TEXT NOT NULL,
    etf_ticker_secondary TEXT NULL,
    active               INTEGER NOT NULL DEFAULT 1,
    display_order_auto   INTEGER NOT NULL,
    created_at           INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- §4.1 — 17 AI-rotation sub-sectors (Jordi stages 1-10)
INSERT INTO sector_universe (code, display_name, parent_gics, jordi_stage, rotation_thesis, etf_ticker_primary, etf_ticker_secondary, display_order_auto) VALUES
  ('gpus_ai_accel',          'GPUs & AI accelerators',                 'Information Technology', 1, 'Jordi stage 1 — compute layer', 'SOXX', NULL, 1),
  ('chip_design_ip',         'Chip design / IP',                       'Information Technology', 1, 'Jordi stages 1, 9 — design + edge silicon IP', 'SOXX', NULL, 2),
  ('optical_networking',     'Optical / networking',                   'Information Technology', 2, 'Jordi stage 2 — datacentre interconnect', 'SOXX', NULL, 3),
  ('hbm_packaging',          'HBM / advanced packaging',               'Information Technology', 3, 'Jordi stage 3 — memory bandwidth bottleneck', 'SOXX', NULL, 4),
  ('foundry',                'Foundry',                                'Information Technology', 2, 'Jordi stages 1-4 — wafer production', 'SOXX', NULL, 5),
  ('semicap',                'Semicap (litho, etch, dep)',             'Information Technology', 2, 'Jordi stages 1-4 — equipment bottleneck', 'SOXX', NULL, 6),
  ('specialty_semi_chem',    'Specialty semi chemicals',               'Materials',              7, 'Jordi stage 7 — wafer + photoresist materials', 'XLB',  'SOXX', 7),
  ('power_gas_turbines',     'Power generation — gas turbines',        'Industrials',            5, 'Jordi stage 5 — power gen bottleneck', 'XLI',  'FXR', 8),
  ('power_nuclear_smr',      'Power generation — nuclear / SMR',       'Utilities',              5, 'Jordi stage 5 — baseload nuclear restart', 'URA',  'URNM', 9),
  ('power_distributed',      'Power generation — distributed / fuel cells', 'Utilities',         5, 'Jordi stage 5 — behind-the-meter generation', 'ICLN', NULL, 10),
  ('power_diversified',      'Power generation — diversified industrial', 'Industrials',         5, 'Jordi stage 5 — multi-segment industrial power', 'XLI', NULL, 11),
  ('grid_transmission',      'Grid & transmission',                    'Industrials',            5, 'Jordi stage 5 — grid upgrade cycle', 'GRID', 'PAVE', 12),
  ('cooling_thermal',        'Cooling & thermal management',           'Industrials',            6, 'Jordi stage 6 — data-centre thermal load', 'XLI',  NULL, 13),
  ('battery_storage',        'Battery storage & metals',               'Materials',              8, 'Jordi stage 8 — grid-scale storage', 'LIT',  'BATT', 14),
  ('data_center_reits',      'Data center REITs',                      'Real Estate',            5, 'Jordi stages 5-6 — physical AI buildout', 'VNQ',  NULL, 15),
  ('edge_industrial_silicon','Edge / industrial silicon',              'Information Technology', 9, 'Jordi stage 9 — edge inference', 'SOXX', NULL, 16),
  ('embodiment',             'Embodiment — robotics, EV, drones',      'Industrials',           10, 'Jordi stage 10 — physical AI applications', 'ROBO', 'IDRV', 17);

-- §4.2 — 6 independent-thesis sub-sectors (non-AI)
INSERT INTO sector_universe (code, display_name, parent_gics, jordi_stage, rotation_thesis, etf_ticker_primary, etf_ticker_secondary, display_order_auto) VALUES
  ('pharma_metabolic',       'Pharma — metabolic / obesity',           'Health Care',  NULL, 'Demographics + obesity epidemic; payer capitulation forces volume', 'XLV',  'IHE', 18),
  ('pharma_immunology',      'Pharma — diversified immunology',        'Health Care',  NULL, 'Post-Humira pipeline transition; chronic-disease demand', 'XLV',  'IHE', 19),
  ('precious_metals_gold',   'Precious metals — gold',                 'Materials',    NULL, 'Fiscal/monetary regime + sovereign de-dollarisation', 'GLD',  'GDX', 20),
  ('precious_metals_silver', 'Precious metals — silver',               'Materials',    NULL, 'Monetary + industrial hybrid; physical/paper dislocation', 'SLV',  'SIL', 21),
  ('defense_sovereign',      'Defense — sovereign re-arming',          'Industrials',  NULL, 'NATO 3%+ commitments, European production gap', 'ITA',  'EUAD', 22),
  ('oil_gas_integrated',     'Oil & gas — integrated',                 'Energy',       NULL, 'Energy security + LNG export cycle', 'XLE',  'XOP', 23);

-- §4.3 — 11 GICS top-level sectors (always-on baseline rotation rows)
INSERT INTO sector_universe (code, display_name, parent_gics, jordi_stage, rotation_thesis, etf_ticker_primary, etf_ticker_secondary, display_order_auto) VALUES
  ('gics_energy',            'Energy (GICS)',                          'Energy',                NULL, 'GICS top-level — baseline rotation tracking', 'XLE',  NULL, 24),
  ('gics_materials',         'Materials (GICS)',                       'Materials',             NULL, 'GICS top-level — baseline rotation tracking', 'XLB',  NULL, 25),
  ('gics_industrials',       'Industrials (GICS)',                     'Industrials',           NULL, 'GICS top-level — baseline rotation tracking', 'XLI',  NULL, 26),
  ('gics_consumer_disc',     'Consumer Discretionary',                 'Consumer Discretionary',NULL, 'GICS top-level — baseline rotation tracking', 'XLY',  NULL, 27),
  ('gics_consumer_stap',     'Consumer Staples',                       'Consumer Staples',      NULL, 'GICS top-level — baseline rotation tracking', 'XLP',  NULL, 28),
  ('gics_healthcare',        'Health Care (GICS)',                     'Health Care',           NULL, 'GICS top-level — baseline rotation tracking', 'XLV',  NULL, 29),
  ('gics_financials',        'Financials',                             'Financials',            NULL, 'GICS top-level — baseline rotation tracking', 'XLF',  NULL, 30),
  ('gics_it',                'Information Technology',                 'Information Technology',NULL, 'GICS top-level — baseline rotation tracking', 'XLK',  NULL, 31),
  ('gics_comm_services',     'Communication Services',                 'Communication Services',NULL, 'GICS top-level — baseline rotation tracking', 'XLC',  NULL, 32),
  ('gics_utilities',         'Utilities',                              'Utilities',             NULL, 'GICS top-level — baseline rotation tracking', 'XLU',  NULL, 33),
  ('gics_real_estate',       'Real Estate',                            'Real Estate',           NULL, 'GICS top-level — baseline rotation tracking', 'XLRE', NULL, 34);

-- ----- 2) Daily ETF close snapshots --------------------------------------

CREATE TABLE sector_snapshots (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    sector_universe_id   INTEGER NOT NULL REFERENCES sector_universe(id),
    snapshot_date        TEXT NOT NULL,                                  -- ISO YYYY-MM-DD
    close_primary        REAL NOT NULL,
    close_secondary      REAL NULL,
    benchmark_spy_close  REAL NOT NULL,
    benchmark_vwrl_close REAL NULL,
    source               TEXT NOT NULL,                                  -- 'yahoo' | 'finnhub' | ...
    created_at           INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(sector_universe_id, snapshot_date)
);
CREATE INDEX idx_sector_snapshots_date ON sector_snapshots(snapshot_date DESC);

-- ----- 3) User's manual ordering -----------------------------------------

CREATE TABLE user_sector_ordering (
    sector_universe_id   INTEGER PRIMARY KEY REFERENCES sector_universe(id),
    display_order_user   INTEGER NOT NULL,
    updated_at           INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- ----- 4) Link holdings + watchlist to taxonomy --------------------------

ALTER TABLE stock_holdings ADD COLUMN sector_universe_id INTEGER NULL REFERENCES sector_universe(id);
ALTER TABLE watchlist      ADD COLUMN sector_universe_id INTEGER NULL REFERENCES sector_universe(id);

-- Backfill the 24 active stock holdings per Sector_Holdings_Mapping_v1.md.
-- All updates are scoped by ticker AND deleted_at IS NULL so soft-deleted
-- rows aren't touched. NOOP for any ticker not present.

UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='gpus_ai_accel')          WHERE ticker='NVDA'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='foundry')                WHERE ticker='TSM'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='semicap')                WHERE ticker='ASML'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='chip_design_ip')         WHERE ticker='ARM'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='edge_industrial_silicon')WHERE ticker='LSCC'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='optical_networking')     WHERE ticker='COHR'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='cooling_thermal')        WHERE ticker='MOD'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='gics_it')                WHERE ticker='ORCL'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='specialty_semi_chem')    WHERE ticker='4063.T'  AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='chip_design_ip')         WHERE ticker='RGTI'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='power_diversified')      WHERE ticker='RR.L'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='power_distributed')      WHERE ticker='BE'      AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='oil_gas_integrated')     WHERE ticker='XOM'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='grid_transmission')      WHERE ticker='SU.PA'   AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='defense_sovereign')      WHERE ticker='RHM.DE'  AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='pharma_metabolic')       WHERE ticker='LLY'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='pharma_immunology')      WHERE ticker='ABBV'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_gold')   WHERE ticker='GLD'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_gold')   WHERE ticker='AEM'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_gold')   WHERE ticker='AU'      AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_silver') WHERE ticker='SLV'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_silver') WHERE ticker='WPM'     AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='precious_metals_silver') WHERE ticker='PAAS'    AND deleted_at IS NULL;
UPDATE stock_holdings SET sector_universe_id = (SELECT id FROM sector_universe WHERE code='battery_storage')        WHERE ticker='ALB'     AND deleted_at IS NULL;

-- ----- 5) Weekly digest log ----------------------------------------------

CREATE TABLE sector_rotation_digests (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    week_ending  TEXT NOT NULL UNIQUE,                                   -- ISO YYYY-MM-DD (Friday)
    markdown     TEXT NOT NULL,
    created_at   INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

-- ----- 6) Macro strip free-text ------------------------------------------

INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
  ('jordi_current_sector_read',
   'Energy — power generation for AI/industrial buildout (Jensen 1000x thesis).',
   strftime('%s','now'));
