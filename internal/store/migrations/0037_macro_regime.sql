-- 0037_macro_regime.sql — Spec 9p (Macro Regime & Sector Rotation) P1+P2 schema.
--
-- Adds the macro-regime layer that sits on top of the existing Sector
-- Rotation tab (retitled "Macro Regime & Sector Rotation"). The growth ×
-- inflation quadrant model (§A), its augmenting state, the curated
-- playbook doctrine (§F, P2), the daily indicator snapshots (mirrors the
-- 9e crypto-indicator snapshot pattern), and the ISM manual override
-- (mirrors cowen_risk_manual). FRED is the data source; the macroregime
-- package reuses cryptoindicators/providers.FREDClient.
--
-- Tables:
--   macro_indicators          — latest reading per FRED-backed series.
--   macro_indicator_snapshots — append-only daily (snapshot_date, series).
--   macro_regime_history      — append-only; latest row = current regime.
--   regime_playbook           — curated favoured/neutral/avoid doctrine.
--   ism_manual                — manual ISM headline override (staleness-gated).

CREATE TABLE macro_indicators (
    series_id   TEXT PRIMARY KEY,          -- our stable key (e.g. 'empire', 'cpi_headline')
    fred_id     TEXT NOT NULL,             -- FRED series_id actually fetched
    name        TEXT NOT NULL,
    source      TEXT NOT NULL,             -- 'FRED' | 'manual' | 'computed'
    axis        TEXT NOT NULL,             -- 'growth' | 'inflation' | 'augment'
    grp         TEXT NOT NULL,             -- 'regional_fed' | 'employment' | 'output' | 'cpi' | 'rates' | 'liquidity' | 'curve' | 'credit' | 'dollar'
    value       REAL,
    prior       REAL,
    roc         REAL,                      -- rate-of-change (units depend on grp)
    direction   TEXT,                      -- 'up' | 'down' | 'flat'
    as_of       TEXT,                      -- observation date YYYY-MM-DD
    fetch_error TEXT,                      -- non-empty if last fetch failed
    updated_at  INTEGER NOT NULL           -- unix seconds
);

CREATE TABLE macro_indicator_snapshots (
    snapshot_date TEXT NOT NULL,           -- YYYY-MM-DD (UTC)
    series_id     TEXT NOT NULL,
    value         REAL,
    roc           REAL,
    PRIMARY KEY (snapshot_date, series_id)
);
CREATE INDEX idx_macro_ind_snap_series ON macro_indicator_snapshots (series_id, snapshot_date DESC);

CREATE TABLE macro_regime_history (
    id                 INTEGER PRIMARY KEY,
    quadrant           TEXT NOT NULL,      -- 'Q1' | 'Q2' | 'Q3' | 'Q4' | 'unclassified'
    shorthand          TEXT NOT NULL,      -- 'Goldilocks' | 'Reflation' | 'Stagflation' | 'Deflation/Recession'
    growth_dir         TEXT NOT NULL,      -- 'accel' | 'decel' | 'unknown'
    inflation_dir      TEXT NOT NULL,      -- 'accel' | 'decel' | 'unknown'
    rates_regime       TEXT,              -- 'hiking' | 'cutting' | 'hold'
    liquidity_regime   TEXT,              -- 'expansion' | 'contraction'
    curve_regime       TEXT,              -- 'normal' | 'inverted'
    credit_regime      TEXT,              -- 'widening' | 'tightening'
    dollar_regime      TEXT,              -- 'strengthening' | 'weakening'
    confidence         TEXT,              -- 'high' | 'medium' | 'low'
    thematic_flags_json TEXT,             -- JSON array of strings
    growth_momentum    REAL,              -- [-1,1]
    inflation_momentum REAL,              -- [-1,1]
    suggested_jordi    TEXT,              -- 'stable' | 'shifting' | 'defensive' | 'unclassified'
    computed_at        INTEGER NOT NULL    -- unix seconds
);
CREATE INDEX idx_macro_regime_history_ts ON macro_regime_history (computed_at DESC);

CREATE TABLE regime_playbook (
    id              INTEGER PRIMARY KEY,
    regime_key      TEXT NOT NULL,        -- 'Q1' | 'Q2' | 'Q3' | 'Q4'
    asset_or_sector TEXT NOT NULL,
    stance          TEXT NOT NULL CHECK (stance IN ('favored', 'neutral', 'avoid')),
    rationale       TEXT,
    source          TEXT,
    sort_order      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_regime_playbook_regime ON regime_playbook (regime_key, stance);

CREATE TABLE ism_manual (
    id         INTEGER PRIMARY KEY,
    value      REAL NOT NULL,
    entered_at INTEGER NOT NULL
);

-- §F starter doctrine (Claude.ai; editable in-app). source='spec_9p_§F'.
INSERT INTO regime_playbook (regime_key, asset_or_sector, stance, rationale, source, sort_order) VALUES
  ('Q1', 'AI-semi',                'favored', 'Goldilocks rewards long-duration growth + risk-on.', 'spec_9p_§F', 1),
  ('Q1', 'Cloud',                  'favored', 'Long-duration growth thrives as inflation decelerates.', 'spec_9p_§F', 2),
  ('Q1', 'Long-duration growth',   'favored', 'Falling inflation + steady growth lifts duration.', 'spec_9p_§F', 3),
  ('Q1', 'BTC/ETH',               'favored', 'Risk-on liquidity backdrop favours majors.', 'spec_9p_§F', 4),
  ('Q1', 'Industrials/Power',      'neutral', 'Participates but not the leadership.', 'spec_9p_§F', 5),
  ('Q1', 'Defensives',             'avoid',   'Lag in a risk-on accelerating-growth tape.', 'spec_9p_§F', 6),
  ('Q1', 'Cash',                   'avoid',   'Opportunity cost high when growth accelerates.', 'spec_9p_§F', 7),
  ('Q1', 'Gold',                   'avoid',   'Real yields and risk-on appetite weigh on gold.', 'spec_9p_§F', 8),
  ('Q2', 'Hydrocarbons/Energy',    'favored', 'Reflation lifts commodities and energy.', 'spec_9p_§F', 1),
  ('Q2', 'Mining/Materials',       'favored', 'Rising inflation + growth lifts hard-asset cyclicals.', 'spec_9p_§F', 2),
  ('Q2', 'Gold',                   'favored', 'Inflation hedge as prices accelerate.', 'spec_9p_§F', 3),
  ('Q2', 'Silver',                 'favored', 'High-beta precious metal in reflation.', 'spec_9p_§F', 4),
  ('Q2', 'Lithium',                'favored', 'Materials demand in reflation.', 'spec_9p_§F', 5),
  ('Q2', 'BTC',                    'favored', 'Reflation/risk-on supports BTC.', 'spec_9p_§F', 6),
  ('Q2', 'Value',                  'favored', 'Value/cyclicals lead when inflation accelerates.', 'spec_9p_§F', 7),
  ('Q2', 'AI-semi',                'neutral', 'Growth holds but no longer sole leadership.', 'spec_9p_§F', 8),
  ('Q2', 'Long bonds',             'avoid',   'Accelerating inflation punishes duration.', 'spec_9p_§F', 9),
  ('Q3', 'Gold',                   'favored', 'Classic stagflation hedge.', 'spec_9p_§F', 1),
  ('Q3', 'Silver',                 'favored', 'Hard asset as growth decelerates + inflation persists.', 'spec_9p_§F', 2),
  ('Q3', 'Energy',                 'favored', 'Supply-driven inflation supports energy.', 'spec_9p_§F', 3),
  ('Q3', 'Defense',                'favored', 'Defensive cash-flows + thematic geopolitics.', 'spec_9p_§F', 4),
  ('Q3', 'Cash',                   'favored', 'Capital preservation when growth fades + inflation bites.', 'spec_9p_§F', 5),
  ('Q3', 'Hard assets',            'favored', 'Stores of value in stagflation.', 'spec_9p_§F', 6),
  ('Q3', 'Hydrocarbons',           'neutral', 'Energy mixed within stagflation.', 'spec_9p_§F', 7),
  ('Q3', 'Growth/Tech',            'avoid',   'De-rates as growth slows + rates stay high.', 'spec_9p_§F', 8),
  ('Q3', 'Long-duration',          'avoid',   'Persistent inflation punishes duration.', 'spec_9p_§F', 9),
  ('Q3', 'Credit',                 'avoid',   'Spreads widen as growth decelerates.', 'spec_9p_§F', 10),
  ('Q3', 'Crypto alts',            'avoid',   'High-beta risk underperforms in stagflation.', 'spec_9p_§F', 11),
  ('Q4', 'Long Treasuries',        'favored', 'Duration rallies as growth + inflation both fall.', 'spec_9p_§F', 1),
  ('Q4', 'USD',                    'favored', 'Safe-haven bid in deflation/recession.', 'spec_9p_§F', 2),
  ('Q4', 'Defensives',             'favored', 'Staples/utilities/pharma outperform in downturns.', 'spec_9p_§F', 3),
  ('Q4', 'Dividend payers',        'favored', 'Income + quality bid in recession.', 'spec_9p_§F', 4),
  ('Q4', 'Quality',                'favored', 'Balance-sheet strength rewarded.', 'spec_9p_§F', 5),
  ('Q4', 'Gold',                   'neutral', 'Mixed: real-yield tailwind vs liquidity drain.', 'spec_9p_§F', 6),
  ('Q4', 'Cyclicals',              'avoid',   'Earnings cut as demand contracts.', 'spec_9p_§F', 7),
  ('Q4', 'High-beta tech',         'avoid',   'Risk-off de-rating.', 'spec_9p_§F', 8),
  ('Q4', 'Crypto alts',            'avoid',   'Liquidity drain hits high-beta crypto hardest.', 'spec_9p_§F', 9),
  ('Q4', 'Mining',                 'avoid',   'Cyclical demand collapse.', 'spec_9p_§F', 10);
