#!/usr/bin/env bash
#
# Weekly jarvis-config backup to Cloudflare R2.
#
# Captures everything needed to rebuild jarvis on a fresh Ubuntu box,
# combined with the daily FT DB backup. Run as root via
# /etc/cron.d/ft-backup at 03:30 UTC every Sunday.
#
# Tarball lands at r2:ft-backups/jarvis-config/jarvis-YYYY-MM-DD.tar.gz
# 365-day retention on R2 (enough for "I let this lapse for a year, did
# the box really change much?" scenarios).
#
# Run manually any time: sudo /opt/ft/bin/backup-jarvis-config.sh

set -euo pipefail

BACKUP_NAME="jarvis-$(date -u +%Y-%m-%d).tar.gz"
TMPDIR=$(mktemp -d)
STAGE="$TMPDIR/jarvis-config"
mkdir -p "$STAGE"

R2_REMOTE="r2:ft-backups"
R2_PREFIX="jarvis-config"
R2_RETENTION_DAYS=365
RCLONE_CONFIG=/var/lib/ft/.config/rclone/rclone.conf

# Run from / so any later cd's don't break things.
cd /

# ----- 1. /etc — service envs + tunnel + cron ----------------------------

for path in /etc/ft /etc/ft-bot /etc/cloudflared; do
    if [ -e "$path" ]; then
        cp -a "$path" "$STAGE/"
    fi
done
mkdir -p "$STAGE/cron.d"
if [ -e /etc/cron.d/ft-backup ]; then
    cp -a /etc/cron.d/ft-backup "$STAGE/cron.d/"
fi

# ----- 2. systemd units --------------------------------------------------

mkdir -p "$STAGE/systemd"
for unit in ft.service ft-bot.service hct.service cloudflared.service; do
    src="/etc/systemd/system/$unit"
    if [ -e "$src" ]; then
        cp -aL "$src" "$STAGE/systemd/" 2>/dev/null || true
    fi
done

# ----- 3. /var/lib data — HCT + rclone creds -----------------------------

if [ -d /var/lib/hct ]; then
    cp -a /var/lib/hct "$STAGE/"
fi
if [ -f /var/lib/ft/.config/rclone/rclone.conf ]; then
    mkdir -p "$STAGE/rclone"
    cp -a /var/lib/ft/.config/rclone/rclone.conf "$STAGE/rclone/"
fi

# ----- 4. /opt non-git sources -------------------------------------------

# FT source lives on GitHub — we don't bundle it. ft-bot and HCT
# don't have git remotes set up; bundle their files so they can be
# fully reconstituted.
if [ -d /opt/ft-bot ]; then
    cp -a /opt/ft-bot "$STAGE/"
fi
if [ -d /opt/hct ]; then
    mkdir -p "$STAGE/opt-hct"
    cp -a /opt/hct/. "$STAGE/opt-hct/"
fi

# ----- 5. System metadata ------------------------------------------------

dpkg --get-selections > "$STAGE/packages.txt"
{
    lsb_release -a 2>&1
    echo "---"
    uname -a
    echo "---"
    date -u
} > "$STAGE/os.txt"

# ----- 6. RESTORE.md walkthrough -----------------------------------------

cat > "$STAGE/RESTORE.md" << 'RESTORE_EOF'
# Rebuild jarvis from this tarball

Pair this with the latest `ft-YYYY-MM-DD.db` snapshot from
`r2:ft-backups/`. Together they're enough to bring up an identical
jarvis on any fresh Ubuntu LTS box.

## Step-by-step

1. **Install Ubuntu Server LTS** on the new box. SSH in as your admin user.

2. **Install base packages:**
   ```
   sudo apt update
   sudo apt install -y sqlite3 rclone golang-go nodejs curl git
   sudo dpkg --set-selections < packages.txt
   sudo apt-get dselect-upgrade -y
   ```

3. **Create the three service users (no login):**
   ```
   sudo useradd --system --home /var/lib/ft     --shell /usr/sbin/nologin ft
   sudo useradd --system --home /opt/ft-bot     --shell /usr/sbin/nologin ft-bot
   sudo useradd --system --home /var/lib/hct    --shell /usr/sbin/nologin hct
   ```

4. **Restore /etc files** (env files, tunnel cert, cron):
   ```
   sudo mkdir -p /etc/ft /etc/ft-bot /etc/cloudflared
   sudo cp -a ft/env       /etc/ft/env       && sudo chmod 600 /etc/ft/env
   sudo cp -a ft-bot/env   /etc/ft-bot/env   && sudo chmod 600 /etc/ft-bot/env
   sudo cp -a cloudflared/* /etc/cloudflared/
   sudo cp cron.d/ft-backup /etc/cron.d/ft-backup
   sudo chmod 644 /etc/cron.d/ft-backup
   ```

5. **Restore systemd units:**
   ```
   sudo cp systemd/*.service /etc/systemd/system/
   sudo systemctl daemon-reload
   ```

6. **Restore /var/lib data** (HCT + rclone creds — chicken-and-egg, do BEFORE R2 restore):
   ```
   sudo cp -a hct /var/lib/
   sudo chown -R hct:hct /var/lib/hct

   sudo mkdir -p /var/lib/ft/.config/rclone
   sudo cp rclone/rclone.conf /var/lib/ft/.config/rclone/
   sudo chown -R ft:ft /var/lib/ft/.config
   sudo chmod 600 /var/lib/ft/.config/rclone/rclone.conf
   ```

7. **Restore FT source + binary from GitHub:**
   ```
   sudo mkdir -p /opt/ft/{src,bin}
   sudo chown -R ft:ft /opt/ft
   sudo -u ft git clone https://github.com/finbarrcurran/ft-.git /opt/ft/src
   cd /opt/ft/src && sudo -u ft make build
   sudo install -m 755 /opt/ft/src/bin/ft                              /opt/ft/bin/ft
   sudo install -m 755 /opt/ft/src/deploy/deploy.sh                    /opt/ft/bin/deploy.sh
   sudo install -m 755 /opt/ft/src/deploy/backup-db.sh                 /opt/ft/bin/backup-db.sh
   sudo install -m 755 /opt/ft/src/deploy/backup-jarvis-config.sh      /opt/ft/bin/backup-jarvis-config.sh
   ```

8. **Restore ft-bot and HCT** (their source isn't in git, lives in this tarball):
   ```
   sudo mkdir -p /opt/ft-bot
   sudo cp -a ft-bot/* /opt/ft-bot/
   sudo chown -R ft-bot:ft-bot /opt/ft-bot

   sudo mkdir -p /opt/hct
   sudo cp -a opt-hct/* /opt/hct/
   sudo chown -R hct:hct /opt/hct
   ```

9. **Pull the latest FT DB from R2:**
   ```
   sudo mkdir -p /var/lib/ft
   sudo chown ft:ft /var/lib/ft
   sudo -u ft HOME=/var/lib/ft \
       rclone --config /var/lib/ft/.config/rclone/rclone.conf \
       copyto r2:ft-backups/ft-YYYY-MM-DD.db /var/lib/ft/ft.db
   sudo chown ft:ft /var/lib/ft/ft.db
   ```

10. **Start services:**
    ```
    sudo systemctl enable --now cloudflared ft ft-bot hct
    ```

11. **Verify:**
    ```
    curl -fsS http://127.0.0.1:8081/healthz
    sudo systemctl status ft ft-bot hct cloudflared
    ```

## Critical notes

- The Cloudflare tunnel `cert.pem` + the named-tunnel JSON file (in
  `cloudflared/`) are the **most irreplaceable items** here. Without them,
  the tunnel ID is lost and you'd have to create a new tunnel + re-point
  Cloudflare DNS records.
- If you rebuild on a different OS version (e.g. Ubuntu noble → next LTS)
  the `packages.txt` step may fail on packages that no longer exist. Skip
  that step and `apt install` the obvious ones manually.
- FT's `internal/store/migrations/` runs on startup, so a fresh binary
  against the restored DB will just verify the schema is current.
RESTORE_EOF

# ----- 7. Tarball + gzip -------------------------------------------------

TARBALL="$TMPDIR/$BACKUP_NAME"
tar -czf "$TARBALL" -C "$TMPDIR" jarvis-config

SIZE=$(stat -c%s "$TARBALL")
echo "Tarball OK: $TARBALL ($SIZE bytes)"

# ----- 8. Upload to R2 ---------------------------------------------------

if ! command -v rclone >/dev/null 2>&1; then
    echo "ERROR: rclone not installed" >&2
    rm -rf "$TMPDIR"
    exit 4
fi
if [ ! -f "$RCLONE_CONFIG" ]; then
    echo "ERROR: $RCLONE_CONFIG not present" >&2
    rm -rf "$TMPDIR"
    exit 5
fi

set +e
rclone --config "$RCLONE_CONFIG" copyto \
    "$TARBALL" "$R2_REMOTE/$R2_PREFIX/$BACKUP_NAME" \
    --s3-no-check-bucket \
    --stats=0
RC=$?
set -e
if [[ $RC -eq 0 ]]; then
    echo "R2 upload OK: $R2_REMOTE/$R2_PREFIX/$BACKUP_NAME"
else
    echo "WARN: R2 upload failed (rc=$RC)" >&2
fi

# Prune R2 backups older than the retention window.
rclone --config "$RCLONE_CONFIG" delete \
    "$R2_REMOTE/$R2_PREFIX/" \
    --min-age "${R2_RETENTION_DAYS}d" \
    --include 'jarvis-*.tar.gz' \
    --s3-no-check-bucket \
    --stats=0 2>/dev/null || true

# Cleanup
rm -rf "$TMPDIR"
echo "Done."
