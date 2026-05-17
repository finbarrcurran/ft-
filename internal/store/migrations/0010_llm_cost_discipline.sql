-- 0010_llm_cost_discipline.sql — Spec 9c.1 D1.
--
-- Cost-control infrastructure for any future LLM-flavored feature. This
-- migration is preventive: no LLM calls exist in FT yet. When the first
-- one ships, it goes through internal/llm.Call() and is bounded by
-- everything seeded here.
--
-- The $5/month + $0.50/day defaults are conservative on purpose. Real
-- workloads can raise via Settings; the system *cannot* exceed the cap.

-- Every LLM call logged, including ones rejected by the budget gate.
CREATE TABLE llm_usage_log (
    id                  INTEGER PRIMARY KEY,
    called_at           INTEGER NOT NULL,                              -- unix seconds
    feature_id          TEXT    NOT NULL,                              -- 'sunday_digest' | 'rescoring' | 'jarvis_query' | 'alert_text' | ...
    feature_context     TEXT,                                          -- free text (ticker, holding_id, etc.)
    provider            TEXT    NOT NULL DEFAULT 'anthropic',
    model               TEXT    NOT NULL,                              -- 'claude-haiku-4-5' etc.
    input_tokens        INTEGER NOT NULL DEFAULT 0,
    output_tokens       INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL    NOT NULL DEFAULT 0,
    outcome             TEXT    NOT NULL CHECK (outcome IN ('success', 'budget_blocked', 'error', 'truncated', 'paused')),
    error_message       TEXT,
    latency_ms          INTEGER,
    request_summary     TEXT                                           -- first 200 chars of prompt for audit
);
CREATE INDEX idx_llm_usage_at      ON llm_usage_log (called_at DESC);
CREATE INDEX idx_llm_usage_feature ON llm_usage_log (feature_id, called_at DESC);
CREATE INDEX idx_llm_usage_outcome ON llm_usage_log (outcome, called_at DESC);

-- Daily aggregation for fast budget checks. Written after every call.
CREATE TABLE llm_usage_daily (
    date                  TEXT    PRIMARY KEY,                        -- 'YYYY-MM-DD' UTC
    call_count            INTEGER NOT NULL DEFAULT 0,
    blocked_count         INTEGER NOT NULL DEFAULT 0,
    total_cost_usd        REAL    NOT NULL DEFAULT 0,
    cost_by_feature_json  TEXT,                                       -- {"sunday_digest": 0.15, ...}
    computed_at           INTEGER NOT NULL                             -- unix seconds
);
CREATE INDEX idx_llm_usage_daily_date ON llm_usage_daily (date DESC);

-- Seed user_preferences with the budget governor defaults. INSERT OR IGNORE
-- so re-running the migration (rebuild paths) doesn't overwrite a Fin-tuned
-- value.
INSERT OR IGNORE INTO user_preferences (key, value, updated_at) VALUES
    -- Hard caps
    ('llm_budget_monthly_usd',           '5.00',                strftime('%s','now')),
    ('llm_budget_daily_usd',             '0.50',                strftime('%s','now')),
    ('llm_hard_stop_enabled',            'true',                strftime('%s','now')),
    -- Threshold alerts (Telegram)
    ('llm_alert_threshold_50_pct',       'true',                strftime('%s','now')),
    ('llm_alert_threshold_75_pct',       'true',                strftime('%s','now')),
    ('llm_alert_threshold_90_pct',       'true',                strftime('%s','now')),
    ('llm_alert_threshold_100_pct',      'true',                strftime('%s','now')),
    -- Model defaults + upgrade discipline
    ('llm_default_model',                'claude-haiku-4-5',    strftime('%s','now')),
    ('llm_require_explicit_upgrade',     'true',                strftime('%s','now')),
    -- Per-call hard caps (anti-agentic-loop)
    ('llm_max_input_tokens_per_call',    '20000',               strftime('%s','now')),
    ('llm_max_output_tokens_per_call',   '2000',                strftime('%s','now')),
    ('llm_max_tools_per_call',           '0',                   strftime('%s','now')),
    -- Caching + global kill switch
    ('llm_prompt_caching_enabled',       'true',                strftime('%s','now')),
    ('llm_globally_paused',              'false',               strftime('%s','now')),
    -- Emergency override slots (set by D9 flow)
    ('llm_emergency_override_until',     '',                    strftime('%s','now')),
    ('llm_emergency_override_reason',    '',                    strftime('%s','now')),
    ('llm_emergency_override_cap_usd',   '0',                   strftime('%s','now')),
    -- Per-feature kill switches (D13). All default on; flip false to disable.
    ('llm_feature_sunday_digest',        'true',                strftime('%s','now')),
    ('llm_feature_rescoring',            'true',                strftime('%s','now')),
    ('llm_feature_alert_text',           'true',                strftime('%s','now')),
    ('llm_feature_jarvis_query',         'true',                strftime('%s','now')),
    -- Threshold-alert dedup state (so we don't spam Telegram on the boundary)
    ('llm_alert_last_fired_50_pct',      '',                    strftime('%s','now')),
    ('llm_alert_last_fired_75_pct',      '',                    strftime('%s','now')),
    ('llm_alert_last_fired_90_pct',      '',                    strftime('%s','now')),
    ('llm_alert_last_fired_100_pct',     '',                    strftime('%s','now')),
    ('llm_alert_last_fired_daily_100',   '',                    strftime('%s','now'));
