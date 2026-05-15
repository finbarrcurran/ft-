#!/usr/bin/env bash
#
# Nightly SQLite backup for FT.
#
# Installed at /opt/ft/bin/backup-db.sh by deploy/install.sh.
# Runs as `ft` user via /etc/cron.d/ft-backup.
#
# Strategy: SQLite's `.backup` is an online-safe copy — doesn't lock
# readers or writers, so it's fine to run while the service is live. We
# also run an integrity check on the copy; if it fails we keep the bad
# file (for forensics) and exit non-zero so cron records it as a failure.

set -euo pipefail

BACKUP_DIR=/var/backups/ft
DB_PATH=/var/lib/ft/ft.db
TIMESTAMP=$(date -u +%Y-%m-%d)
BACKUP_FILE="$BACKUP_DIR/ft-$TIMESTAMP.db"

if [[ ! -f "$DB_PATH" ]]; then
    echo "ERROR: source DB missing at $DB_PATH" >&2
    exit 2
fi

mkdir -p "$BACKUP_DIR"

# SQLite online backup. Avoids the file-copy race; consistent snapshot.
sqlite3 "$DB_PATH" ".backup $BACKUP_FILE"

# Verify the snapshot before we trust it. PRAGMA integrity_check returns "ok"
# on success or a series of error lines on failure.
INTEGRITY=$(sqlite3 "$BACKUP_FILE" "PRAGMA integrity_check;")
if [[ "$INTEGRITY" != "ok" ]]; then
    echo "ERROR: integrity_check failed for $BACKUP_FILE:" >&2
    echo "$INTEGRITY" >&2
    exit 3
fi

# Prune backups older than 14 days. -mtime +14 = older than 14 × 24h.
find "$BACKUP_DIR" -maxdepth 1 -name 'ft-*.db' -mtime +14 -print -delete >/dev/null

SIZE=$(stat -c%s "$BACKUP_FILE")
echo "Backup OK: $BACKUP_FILE ($SIZE bytes)"
