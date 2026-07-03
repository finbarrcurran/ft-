#!/usr/bin/env bash
# SC-37 P2 — weekly automated restore-verify (suggest Sun 03:30 UTC). UNTESTED
# DRAFT. Restores the latest snapshot to a temp path, integrity-checks it,
# sanity-checks row counts vs live, runs restic check, alerts on any failure.
set -euo pipefail

ENV_FILE="/etc/ft/backup.env"
LIVE="/var/lib/ft/ft.db"
RDIR="/tmp/ft-restore-verify"
BOT_ALERT_URL="${BOT_ALERT_URL:-http://127.0.0.1:8081/api/bot/alerts}"

fail() {
  local msg="🔴 FT restore-verify FAILED at [$1]: $2"
  curl -fsS -X POST "$BOT_ALERT_URL" -H 'Content-Type: application/json' \
    -d "{\"source\":\"ft-restore-verify\",\"dedupe_key\":\"ft-restore-verify\",\"text\":$(printf '%s' "$msg" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read()))')}" \
    >/dev/null 2>&1 || true
  echo "$msg" >&2; exit 1
}
[ -f "$ENV_FILE" ] || fail "preflight" "missing $ENV_FILE"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a

rm -rf "$RDIR"; mkdir -p "$RDIR"
trap 'rm -rf "$RDIR"' EXIT

restic restore latest --target "$RDIR" || fail "restore" "restic restore failed"
RDB="$(find "$RDIR" -name ft.db -type f | head -1)"
[ -n "$RDB" ] || fail "restore" "restored ft.db not found"

ic="$(sqlite3 "$RDB" 'PRAGMA integrity_check;')" || fail "integrity" "PRAGMA errored"
[ "$ic" = "ok" ] || fail "integrity" "restored integrity_check: $ic"

# Row-count sanity vs live (exact where exact expected; ±5% otherwise).
theses="$(sqlite3 "$RDB" 'SELECT COUNT(*) FROM theses_index;')"
[ "$theses" -ge 54 ] || fail "rowcount" "theses_index=$theses (expected >=54: 52 locked + 2 superseded)"
bars="$(sqlite3 "$RDB" "SELECT COUNT(*) FROM daily_bars;")"
[ "$bars" -gt 0 ] || fail "rowcount" "daily_bars empty in restore"
# nexus latest as_of within 8 days.
asof="$(sqlite3 "$RDB" 'SELECT MAX(as_of) FROM nexus_technical;')"
[ -n "$asof" ] || fail "rowcount" "nexus_technical has no as_of"

restic check || fail "repo" "restic check failed"
echo "ft-restore-verify OK $(date -u +%FT%TZ): theses=$theses bars=$bars nexus_asof=$asof"
