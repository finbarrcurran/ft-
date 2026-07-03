#!/usr/bin/env bash
# SC-37 P1 — nightly off-box backup of the live FT DB. UNTESTED DRAFT: not wired
# to cron and will no-op-fail until Fin provisions the B2 bucket/key and the
# /etc/ft/backup.env credentials file (§P0). Each stage gates the next; any
# failure fires a single Telegram alert; green nights are silent.
#
# Wire-up (once creds land): chmod +x; add the 03:00 UTC timer; kill-test.
set -euo pipefail

ENV_FILE="/etc/ft/backup.env"        # §0-G: root-owned, mode 600. Provides:
                                     #   RESTIC_REPOSITORY (b2:ft-backup-jarvis:/)
                                     #   RESTIC_PASSWORD
                                     #   B2_ACCOUNT_ID / B2_ACCOUNT_KEY
DB="/var/lib/ft/ft.db"
TMP="/tmp/ft-backup"
BOT_ALERT_URL="${BOT_ALERT_URL:-http://127.0.0.1:8081/api/bot/alerts}"

fail() {  # fail <stage> <detail>
  local msg="🔴 FT backup FAILED at [$1]: $2"
  curl -fsS -X POST "$BOT_ALERT_URL" -H 'Content-Type: application/json' \
    -d "{\"source\":\"ft-backup\",\"dedupe_key\":\"ft-backup-daily\",\"text\":$(printf '%s' "$msg" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')}" \
    >/dev/null 2>&1 || true
  echo "$msg" >&2
  exit 1
}

[ -f "$ENV_FILE" ] || fail "preflight" "missing $ENV_FILE (credentials not provisioned)"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

mkdir -p "$TMP"
trap 'rm -f "$TMP/ft.db"' EXIT

# 1. Online-consistent snapshot (never a raw cp of a hot DB).
sqlite3 "$DB" ".backup '$TMP/ft.db'" || fail "snapshot" "sqlite3 .backup failed"

# 2. Integrity gate.
ic="$(sqlite3 "$TMP/ft.db" 'PRAGMA integrity_check;')" || fail "integrity" "PRAGMA errored"
[ "$ic" = "ok" ] || fail "integrity" "integrity_check returned: $ic"

# 3. Encrypted push to B2 (restic encrypts at rest).
restic backup "$TMP/ft.db" --tag ft-db --host jarvis || fail "push" "restic backup failed"

# 4. Retention prune (§0-C: 7 daily / 4 weekly / 6 monthly).
restic forget --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune || fail "prune" "restic forget failed"

echo "ft-backup OK $(date -u +%FT%TZ)"
