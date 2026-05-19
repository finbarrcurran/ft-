#!/usr/bin/env bash
#
# Nightly SQLite backup for FT — local snapshot + off-site upload.
#
# Installed at /opt/ft/bin/backup-db.sh by deploy/install.sh.
# Runs as `ft` user via /etc/cron.d/ft-backup at 03:15 UTC daily.
#
# Two-stage backup strategy (set 2026-05-19):
#
#  1. Local snapshot — SQLite `.backup` writes a consistent copy to
#     /var/backups/ft/ without locking the live DB. 14-day rolling
#     window via `find -mtime +14`.
#
#  2. Off-site upload — rclone pushes the snapshot to Cloudflare R2
#     (bucket: ft-backups). Credentials at
#     /var/lib/ft/.config/rclone/rclone.conf (mode 0600, ft:ft).
#     90-day retention on R2 via in-script `rclone delete --min-age`.
#     Failure to upload is logged but does NOT fail the script —
#     local backup still succeeded and a transient R2 hiccup
#     shouldn't break the cron chain.

set -euo pipefail

BACKUP_DIR=/var/backups/ft
DB_PATH=/var/lib/ft/ft.db
TIMESTAMP=$(date -u +%Y-%m-%d)
BACKUP_FILE="$BACKUP_DIR/ft-$TIMESTAMP.db"

R2_REMOTE="r2:ft-backups"
R2_RETENTION_DAYS=90
RCLONE_CONFIG=/var/lib/ft/.config/rclone/rclone.conf

# Run from a directory the ft user can always read. Without this, calling the
# script from somewhere ft can't enter (e.g. /home/curran) triggers GNU find's
# "Failed to restore initial working directory" fatal error during pruning.
cd /

if [[ ! -f "$DB_PATH" ]]; then
    echo "ERROR: source DB missing at $DB_PATH" >&2
    exit 2
fi

mkdir -p "$BACKUP_DIR"

# ----- Stage 1: local snapshot --------------------------------------------

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

# Prune local backups older than 14 days.
find "$BACKUP_DIR" -maxdepth 1 -name 'ft-*.db' -mtime +14 -print -delete >/dev/null

SIZE=$(stat -c%s "$BACKUP_FILE")
echo "Local backup OK: $BACKUP_FILE ($SIZE bytes)"

# ----- Stage 2: off-site upload to Cloudflare R2 --------------------------
#
# Graceful no-op if rclone or config is missing (e.g. dev environment).
# Errors here are logged but don't fail the script — the local backup
# already succeeded, and we don't want a transient R2 issue to break the
# cron chain.

if ! command -v rclone >/dev/null 2>&1; then
    echo "R2 upload skipped: rclone not installed" >&2
elif [[ ! -f "$RCLONE_CONFIG" ]]; then
    echo "R2 upload skipped: $RCLONE_CONFIG not present (set FT_GITHUB_TOKEN equivalent for R2 creds)" >&2
else
    # Don't propagate failures from this block.
    set +e
    rclone --config "$RCLONE_CONFIG" copyto \
        "$BACKUP_FILE" "$R2_REMOTE/ft-$TIMESTAMP.db" \
        --s3-no-check-bucket \
        --stats=0
    if [[ $? -eq 0 ]]; then
        echo "R2 upload OK: $R2_REMOTE/ft-$TIMESTAMP.db"
    else
        echo "WARN: R2 upload failed (local backup still good)" >&2
    fi
    # Prune R2 backups older than 90 days. Independent of local prune so
    # we have a longer disaster-recovery window off-site.
    rclone --config "$RCLONE_CONFIG" delete \
        "$R2_REMOTE" \
        --min-age "${R2_RETENTION_DAYS}d" \
        --include 'ft-*.db' \
        --s3-no-check-bucket \
        --stats=0 2>/dev/null
    set -e
fi
