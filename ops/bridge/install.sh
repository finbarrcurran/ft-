#!/bin/bash
# SC-45 — install the Google Drive spec mirror on a (re)built Jarvis. Idempotent.
# Run as root from the repo:  sudo /opt/ft/src/ops/bridge/install.sh
#
# The exporter is copied to /usr/local/sbin (root-owned) rather than run from
# /opt/ft/src: it runs as root, and the source tree is writable by the `ft` user.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)"; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)
RUSER=${RUSER:-curran}                       # owns the rclone remote
id "$RUSER" >/dev/null

install -m 755 -o root -g root "$HERE/ft-bridge-spec" /usr/local/sbin/ft-bridge-spec
install -m 644 "$HERE/ft-bridge-spec.service" "$HERE/ft-bridge-spec.timer" /etc/systemd/system/
install -d -m 755 -o "$RUSER" -g "$RUSER" "/home/$RUSER/scripts/ft_bridge"
install -m 750 -o "$RUSER" -g "$RUSER" "$HERE"/user-scripts/*.sh "/home/$RUSER/scripts/ft_bridge/"
systemctl daemon-reload
systemctl enable --now ft-bridge-spec.timer
echo "installed: /usr/local/sbin/ft-bridge-spec, ft-bridge-spec.{service,timer}, ~$RUSER/scripts/ft_bridge/*.sh"

if runuser -u "$RUSER" -- rclone listremotes 2>/dev/null | grep -q '^ftbridge:$'; then
  echo "rclone remote 'ftbridge' is present for $RUSER — the mirror will upload on its next tick."
else
  cat <<MSG
NOTE: no rclone remote 'ftbridge' for $RUSER yet (it is not in the repo or the backups' reach of
a rebuild unless ~/.config/rclone was restored). One-time Google consent needs a human:
  ssh -t -L 53682:127.0.0.1:53682 $RUSER@<jarvis>  '~/scripts/ft_bridge/1_authorize.sh'
  then:  ~/scripts/ft_bridge/2_create_folder.sh
(see README.md)
MSG
fi
