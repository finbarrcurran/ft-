#!/usr/bin/env bash
# SC-37 P1 — nightly off-box backup of the live FT DB (Backblaze B2 via restic).
# Runs as root (systemd): needs /etc/ft/backup.env, /etc/ft-bot/env, and ft.db.
# Each stage gates the next; any failure fires ONE Telegram alert; green = silent.
set -uo pipefail

ENV_FILE="/etc/ft/backup.env"
BOT_ENV="/etc/ft-bot/env"
DB="/var/lib/ft/ft.db"
TMP="/tmp/ft-backup"

ft_alert() {  # $1 = message text
  [ -f "$BOT_ENV" ] || { echo "alert: missing $BOT_ENV" >&2; return 0; }
  local TOK CHAT
  TOK=$(grep -E '^TELEGRAM_BOT_TOKEN=' "$BOT_ENV" | cut -d= -f2-)
  CHAT=$(grep -E '^TELEGRAM_CHAT_ID=' "$BOT_ENV" | cut -d= -f2-)
  [ -n "$TOK" ] && [ -n "$CHAT" ] || { echo "alert: no token/chat" >&2; return 0; }
  curl -fsS -m 20 "https://api.telegram.org/bot${TOK}/sendMessage" \
    --data-urlencode "chat_id=${CHAT}" --data-urlencode "text=$1" >/dev/null 2>&1 \
    || echo "alert: telegram send failed" >&2
}
fail() { ft_alert "FT backup FAILED at [$1]: $2"; echo "FT backup FAIL [$1]: $2" >&2; rm -f "$TMP/ft.db"; exit 1; }

[ -f "$ENV_FILE" ] || fail "preflight" "missing $ENV_FILE"
set -a; . "$ENV_FILE"; set +a
[ -f "$DB" ] || fail "preflight" "missing DB $DB"

mkdir -p "$TMP"

# 1. Online-consistent snapshot (never a raw cp of a hot DB).
if ! sqlite3 "$DB" ".backup '$TMP/ft.db'"; then fail "snapshot" "sqlite3 .backup failed"; fi

# 2. Integrity gate.
ic=$(sqlite3 "$TMP/ft.db" 'PRAGMA integrity_check;' 2>&1) || fail "integrity" "PRAGMA errored: $ic"
[ "$ic" = "ok" ] || fail "integrity" "integrity_check returned: $ic"

# 3. Encrypted push to B2 (restic encrypts at rest).
if ! restic backup "$TMP/ft.db" --tag ft-db --host jarvis; then fail "push" "restic backup failed"; fi

# 4. Retention prune (7 daily / 4 weekly / 6 monthly).
if ! restic forget --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune; then fail "prune" "restic forget failed"; fi

# 5. Clean temp snapshot.
rm -f "$TMP/ft.db"
echo "ft-backup OK $(date -u +%FT%TZ)"
