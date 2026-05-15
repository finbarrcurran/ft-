#!/usr/bin/env bash
# install.sh — provision the FT service on Ubuntu 24.04.
# Run AS A SUDOER from inside the project directory:
#
#   sudo FT_DOMAIN=ft.curranhouse.dev ./deploy/install.sh
#
# Idempotent: safe to re-run after edits.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "must be run as root (sudo)"; exit 1
fi

DOMAIN="${FT_DOMAIN:-ft.curranhouse.dev}"
SRC_DIR="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> using domain: $DOMAIN"
echo "==> source dir:   $SRC_DIR"

# 1) System user (no shell, no home).
if ! id ft >/dev/null 2>&1; then
  echo "==> creating system user 'ft'"
  useradd --system --no-create-home --shell /usr/sbin/nologin ft
fi

# 2) Directories.
echo "==> creating directories"
install -d -o ft -g ft -m 0750 /opt/ft
install -d -o ft -g ft -m 0750 /opt/ft/bin
install -d -o ft -g ft -m 0750 /var/lib/ft

# 3) Build the binary if not already present.
if [[ ! -x "$SRC_DIR/bin/ft" ]]; then
  echo "==> building ft binary"
  if ! command -v go >/dev/null; then
    echo "go toolchain not found. install with: apt install golang-go"
    exit 1
  fi
  (cd "$SRC_DIR" && go mod tidy && make build)
fi

echo "==> installing binary"
install -o ft -g ft -m 0755 "$SRC_DIR/bin/ft" /opt/ft/bin/ft

# 4) Install systemd unit, substituting the domain.
echo "==> installing systemd unit"
sed "s|ft.curranhouse.dev|$DOMAIN|g" "$SRC_DIR/deploy/ft.service" > /etc/systemd/system/ft.service
systemctl daemon-reload
systemctl enable ft
systemctl restart ft

# 5) Wait for healthz.
echo "==> waiting for /healthz"
for i in $(seq 1 20); do
  if curl -fsS --max-time 2 http://127.0.0.1:8081/healthz >/dev/null 2>&1; then
    echo "==> ok"
    break
  fi
  sleep 0.5
done

# 6) Install backup script + cron + log dir
echo "==> installing nightly backup"
install -o ft -g ft -m 0755 "$SRC_DIR/deploy/backup-db.sh" /opt/ft/bin/backup-db.sh
install -o ft -g ft -m 0755 "$SRC_DIR/deploy/deploy.sh"    /opt/ft/bin/deploy.sh
install -d -o ft -g ft -m 0750 /var/log/ft
install -d -o ft -g ft -m 0750 /var/backups/ft
install -o root -g root -m 0644 "$SRC_DIR/deploy/ft-backup.cron" /etc/cron.d/ft-backup

echo
echo "FT is running. Cloudflare Tunnel is already configured to route"
echo "https://${DOMAIN} → http://127.0.0.1:8081 on this box."
echo
echo "Next steps:"
echo "  - sudo -u ft /opt/ft/bin/ft help          # subcommand reference"
echo "  - sudo -u ft /opt/ft/bin/deploy.sh        # subsequent deploys (git pull + rebuild)"
echo "  - sudo -u ft /opt/ft/bin/backup-db.sh     # run a backup manually"
echo "  - sudo journalctl -u ft -f                # follow logs"
