-- 0007_user_preferences.sql — Spec 6 D1.
--
-- Single-user-scoped key/value preference store. No user_id column (FT is
-- single-user); add one later if multi-user lands. Seed with the default
-- heatmap mode so a fresh DB renders the existing market-cap view.
--
-- Future keys will include: alert-history filters, saved keyboard
-- shortcuts, default tab, etc.

CREATE TABLE user_preferences (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

INSERT INTO user_preferences (key, value, updated_at)
VALUES ('heatmap_mode', 'market_cap', strftime('%s','now'));
