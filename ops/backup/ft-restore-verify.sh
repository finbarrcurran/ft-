#!/usr/bin/env bash
# SC-37 P2 — weekly restore-verify (proves the backup is restorable, not just present).
# Runs as root (systemd). Restores latest -> temp, integrity + row-count sanity vs live,
# restic check. Any failure fires a Telegram alert; green = silent.
set -uo pipefail

ENV_FILE="/etc/ft/backup.env"
BOT_ENV="/etc/ft-bot/env"
RDIR="/tmp/ft-restore-verify"

ft_alert() {
  [ -f "$BOT_ENV" ] || { echo "alert: missing $BOT_ENV" >&2; return 0; }
  local TOK CHAT
  TOK=$(grep -E '^TELEGRAM_BOT_TOKEN=' "$BOT_ENV" | cut -d= -f2-)
  CHAT=$(grep -E '^TELEGRAM_CHAT_ID=' "$BOT_ENV" | cut -d= -f2-)
  [ -n "$TOK" ] && [ -n "$CHAT" ] || { echo "alert: no token/chat" >&2; return 0; }
  curl -fsS -m 20 "https://api.telegram.org/bot${TOK}/sendMessage" \
    --data-urlencode "chat_id=${CHAT}" --data-urlencode "text=$1" >/dev/null 2>&1 \
    || echo "alert: telegram send failed" >&2
}
fail() { ft_alert "FT restore-verify FAILED at [$1]: $2"; echo "FT restore-verify FAIL [$1]: $2" >&2; rm -rf "$RDIR"; exit 1; }

[ -f "$ENV_FILE" ] || fail "preflight" "missing $ENV_FILE"
set -a; . "$ENV_FILE"; set +a

rm -rf "$RDIR"; mkdir -p "$RDIR"

if ! restic restore latest --target "$RDIR"; then fail "restore" "restic restore failed"; fi
RDB=$(find "$RDIR" -name ft.db -type f | head -1)
[ -n "$RDB" ] || fail "restore" "restored ft.db not found"

ic=$(sqlite3 "$RDB" 'PRAGMA integrity_check;' 2>&1) || fail "integrity" "PRAGMA errored: $ic"
[ "$ic" = "ok" ] || fail "integrity" "restored integrity_check: $ic"

locked=$(sqlite3 "$RDB" "SELECT COUNT(*) FROM theses_index WHERE status='locked';")
sup=$(sqlite3 "$RDB" "SELECT COUNT(*) FROM theses_index WHERE status='superseded';")
[ "${locked:-0}" -ge 50 ] || fail "rowcount" "theses_index locked=$locked (expected >=50; live baseline 52+2)"
bars=$(sqlite3 "$RDB" "SELECT COUNT(*) FROM daily_bars;")
[ "${bars:-0}" -gt 0 ] || fail "rowcount" "daily_bars empty in restore"
asof=$(sqlite3 "$RDB" "SELECT COALESCE(MAX(as_of),'') FROM nexus_technical;")
[ -n "$asof" ] || fail "rowcount" "nexus_technical has no as_of"

if ! restic check; then fail "repo" "restic check failed"; fi

rm -rf "$RDIR"
echo "ft-restore-verify OK $(date -u +%FT%TZ): theses locked=$locked superseded=$sup daily_bars=$bars nexus_asof=$asof"
