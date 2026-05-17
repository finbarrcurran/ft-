-- 0020_scorecards.sql — Spec 9g D1.
--
-- Adapter scorecards repository. Single row per adapter (current version);
-- append-only history table archives every "Save as new version" bump.
-- The Philosophy v1.1 doctrine document gets is_doctrine=1 so the UI hides
-- the Edit button + the PUT handler returns 403 for it.

CREATE TABLE sector_scorecards (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    code                TEXT NOT NULL UNIQUE,
    display_name        TEXT NOT NULL,
    short_description   TEXT NOT NULL,
    current_version     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'locked', 'needs-review')),
    markdown_current    TEXT NOT NULL,
    applies_to_sectors  TEXT NOT NULL,                                   -- JSON array of sector_universe.code
    is_doctrine         INTEGER NOT NULL DEFAULT 0,                      -- 1 = Philosophy (no UI edit)
    created_at          INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    updated_at          INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE sector_scorecard_versions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    scorecard_id    INTEGER NOT NULL REFERENCES sector_scorecards(id),
    version         TEXT NOT NULL,
    markdown        TEXT NOT NULL,
    changelog_note  TEXT,
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    UNIQUE(scorecard_id, version)
);

CREATE INDEX idx_scorecard_versions ON sector_scorecard_versions(scorecard_id, created_at DESC);
