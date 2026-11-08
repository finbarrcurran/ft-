-- SC-55 P1 — locked theses are never hard-deleted. A row whose file is gone or no longer parses is ARCHIVED
-- (status 'archived', archived_at, archive_reason) and keeps its markdown, rendered HTML and file SHA; it is
-- restored by the next sync that finds a valid file for the same (ticker, version).
-- archive_reason values: file_missing | header_invalid.
ALTER TABLE theses_index ADD COLUMN archived_at INTEGER;
ALTER TABLE theses_index ADD COLUMN archive_reason TEXT;

-- Defense in depth: nothing can DELETE from theses_index unless a row exists in thesis_delete_unlock.
-- Intentional removal (admin only), from sqlite3:
--   INSERT INTO thesis_delete_unlock (reason, created_at) VALUES ('why', strftime('%s','now'));
--   DELETE FROM theses_index WHERE id = N;
--   DELETE FROM thesis_delete_unlock;
CREATE TABLE IF NOT EXISTS thesis_delete_unlock (
    id         INTEGER PRIMARY KEY,
    reason     TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TRIGGER IF NOT EXISTS theses_index_no_delete
BEFORE DELETE ON theses_index
WHEN (SELECT COUNT(*) FROM thesis_delete_unlock) = 0
BEGIN
    SELECT RAISE(ABORT, 'theses_index rows are never deleted: archive instead (see migration 0065 for the admin path)');
END;
