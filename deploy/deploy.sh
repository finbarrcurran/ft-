#!/usr/bin/env bash
#
# FT deploy script. Pulls latest main, rebuilds, installs binary, restarts.
#
# Usage:
#
#     /opt/ft/bin/deploy.sh
#
# Auto-elevates to root via sudo if needed. Internally shells out to the
# `ft` user for git-pull and build so ownership stays correct in /opt/ft/src.
# Requires the invoking user to have passwordless sudo (configured during
# project setup).

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    exec sudo "$0" "$@"
fi

SRC_DIR=/opt/ft/src
BIN_DEST=/opt/ft/bin/ft
SERVICE=ft

if [[ ! -d "$SRC_DIR/.git" ]]; then
    echo "ERROR: $SRC_DIR is not a git checkout. See deploy/RUNBOOK.md." >&2
    exit 1
fi

echo "==> git pull (as ft)"
sudo -u ft -H git -C "$SRC_DIR" pull --ff-only

echo "==> build (as ft)"
sudo -u ft -H bash -c "cd '$SRC_DIR' && make build"

echo "==> install binary"
install -m 0755 "$SRC_DIR/bin/ft" "$BIN_DEST"
chown ft:ft "$BIN_DEST"

# If deploy/backup scripts or the cron file changed, sync them. Cheap; idempotent.
install -m 0755 "$SRC_DIR/deploy/deploy.sh"                 /opt/ft/bin/deploy.sh
install -m 0755 "$SRC_DIR/deploy/backup-db.sh"              /opt/ft/bin/backup-db.sh
install -m 0755 "$SRC_DIR/deploy/backup-jarvis-config.sh"   /opt/ft/bin/backup-jarvis-config.sh
chown ft:ft /opt/ft/bin/deploy.sh /opt/ft/bin/backup-db.sh /opt/ft/bin/backup-jarvis-config.sh

# Cron entries (root-owned, mode 0644 per /etc/cron.d/ convention)
install -m 0644 -o root -g root "$SRC_DIR/deploy/ft-backup.cron" /etc/cron.d/ft-backup

echo "==> restart service"
systemctl restart "$SERVICE"

sleep 2
if curl -fsS --max-time 5 http://127.0.0.1:8081/healthz >/dev/null; then
    echo "==> ok — service responding on /healthz"
else
    echo "ERROR: /healthz did not respond after restart" >&2
    echo "Inspect: sudo journalctl -u ft -n 50" >&2
    exit 4
fi
