#!/bin/bash
# Install the two Playwright cron scrapers (Farside ETF flows, Capitol Trades) on a (re)built Jarvis.
# Idempotent. Run as root from the repo:  sudo /opt/ft/src/ops/scrapers/install.sh
#
# Everything that silently broke after the 2026-09 Lenovo migration is handled here:
#   1. cron lines log to ~curran/logs (curran cannot create files in /var/log);
#   2. curran can reach and write the scrapers' output dirs under /var/lib/ft/data
#      (curran joins group `ft`; /var/lib/ft is 750 ft:ft and ft.db is mode 644, so the
#      directory is NOT opened to everyone);
#   3. Playwright + Chromium are installed (node_modules and ~/.cache are not in the backups).
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)"; exit 1; }
HERE=$(cd "$(dirname "$0")" && pwd)
U=${SCRAPER_USER:-curran}
H=$(getent passwd "$U" | cut -d: -f6)

install -d -m 755 -o "$U" -g "$U" "$H/logs" "$H/scripts"
install -m 644 -o "$U" -g "$U" "$HERE"/farside-fetch.js "$HERE"/capitol-trades-fetch.js "$HERE"/package.json "$HERE"/package-lock.json "$H/scripts/"
install -m 644 "$HERE/ft-farside-fetch.cron" /etc/cron.d/ft-farside-fetch
install -m 644 "$HERE/ft-capitol-trades.cron" /etc/cron.d/ft-capitol-trades
install -m 644 "$HERE/ft-scrapers.logrotate" /etc/logrotate.d/ft-scrapers

usermod -aG ft "$U"
for d in farside capitol-trades; do
  install -d -m 2775 -o "$U" -g ft "/var/lib/ft/data/$d"
  find "/var/lib/ft/data/$d" -exec chown "$U":ft {} + -exec chmod g+rw {} +
done

cd "$H/scripts"
runuser -u "$U" -- npm ci --no-audit --no-fund
runuser -u "$U" -- npx --no-install playwright install chromium
env "PATH=$PATH" npx --no-install playwright install-deps chromium
echo "done. Run each job once by hand and read the log:"
echo "  sudo -u $U bash -c 'cd ~/scripts && NODE_PATH=\$PWD/node_modules node farside-fetch.js'"
echo "NOTE: capitoltrades.com answers 429 'Vercel Security Checkpoint' to automated clients since ~2026-08-14; that job is expected to log an error until a new source is chosen."
