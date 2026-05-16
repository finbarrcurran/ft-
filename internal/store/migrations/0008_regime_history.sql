-- 0008_regime_history.sql — Spec 9b D1.
--
-- Append-only history of regime changes. Current state lives in
-- `user_preferences` under keys: regime_jordi, regime_cowen,
-- regime_jordi_set_at, regime_cowen_set_at, regime_cowen_last_inputs.
-- This table captures every transition for retrospective.
--
-- source values:
--   'manual'          → user clicked Jordi pill or Cowen "Quick set"
--   'auto_cowen_form' → user submitted the 8-field Cowen weekly capture
--
-- inputs_json:
--   manual              → null or {"prior": "<old regime>"} for diff context
--   auto_cowen_form     → full 8-field form payload + computed flags

CREATE TABLE regime_history (
    id            INTEGER PRIMARY KEY,
    ts            INTEGER NOT NULL,                                  -- unix seconds
    framework_id  TEXT    NOT NULL CHECK (framework_id IN ('jordi', 'cowen')),
    regime        TEXT    NOT NULL CHECK (regime IN ('stable', 'shifting', 'defensive', 'unclassified')),
    source        TEXT    NOT NULL CHECK (source IN ('manual', 'auto_cowen_form')),
    inputs_json   TEXT,
    note          TEXT
);
CREATE INDEX idx_regime_history_framework ON regime_history (framework_id, ts DESC);
CREATE INDEX idx_regime_history_ts        ON regime_history (ts DESC);

-- Seed both regimes as 'unclassified' so the API has something to return on
-- first load after migration. user_preferences (Spec 6) is already in place.
INSERT INTO user_preferences (key, value, updated_at) VALUES
    ('regime_jordi',         'unclassified', strftime('%s','now')),
    ('regime_cowen',         'unclassified', strftime('%s','now')),
    ('regime_jordi_set_at',  strftime('%s','now'), strftime('%s','now')),
    ('regime_cowen_set_at',  strftime('%s','now'), strftime('%s','now'))
ON CONFLICT(key) DO NOTHING;
