#!/usr/bin/env bash
#
# FT deploy script. Pulls the latest main, rebuilds, installs, restarts.
#
# Run as the ft user:
#
#     sudo -u ft /opt/ft/bin/deploy.sh
#
# First-time setup: see deploy/RUNBOOK.md → "Git-based deploy" section.

set -euo pipefail

SRC_DIR=/opt/ft/src
BIN_DEST=/opt/ft/bin/ft
SERVICE=ft

if [[ ! -d "$SRC_DIR/.git" ]]; then
    echo "ERROR: $SRC_DIR is not a git checkout. First-run setup required." >&2
    echo "See deploy/RUNBOOK.md." >&2
    exit 1
fi

cd "$SRC_DIR"

echo "==> git pull"
git pull --ff-only

echo "==> build"
make build

echo "==> install binary"
# install -m 0755 requires write access to /opt/ft/bin; ft user owns it.
install -m 0755 ./bin/ft "$BIN_DEST"

echo "==> restart service"
sudo systemctl restart "$SERVICE"

# Healthcheck — give the service a moment to come up, then probe.
sleep 2
if curl -fsS --max-time 5 http://127.0.0.1:8081/healthz >/dev/null; then
    echo "==> ok — service responding on /healthz"
else
    echo "ERROR: /healthz did not respond after restart" >&2
    echo "Inspect: sudo journalctl -u ft -n 50" >&2
    exit 4
fi
