# FT deploy runbook

## First-time setup on a fresh jarvis

1. Install dependencies:
   ```bash
   sudo apt-get update
   sudo apt-get install -y golang-go sqlite3 build-essential make git curl
   go version  # need 1.22+; toolchain auto-bumps if go.mod requires newer
   ```

2. Clone the repo as the `ft` user. (Spec 1 D5 — the ft user needs an SSH key
   added to GitHub as a deploy key first; see `Adding a deploy key` below.)
   ```bash
   sudo useradd --system --no-create-home --shell /usr/sbin/nologin ft || true
   sudo install -d -o ft -g ft -m 0750 /opt/ft /opt/ft/bin /var/lib/ft

   # First clone (creates /opt/ft/src):
   sudo -u ft git clone git@github.com:finbarrcurran/ft-.git /opt/ft/src
   ```

3. Build and install:
   ```bash
   cd /opt/ft/src
   sudo -u ft make build
   sudo install -o ft -g ft -m 0755 ./bin/ft /opt/ft/bin/ft
   ```

4. Install systemd unit, backup script, deploy script:
   ```bash
   sudo FT_DOMAIN=ft.curranhouse.dev ./deploy/install.sh
   ```

5. Verify:
   ```bash
   curl -fsS http://127.0.0.1:8081/healthz
   sudo systemctl status ft --no-pager
   ```

## Subsequent deploys

```bash
sudo -u ft /opt/ft/bin/deploy.sh
```

That does `git pull` → `make build` → `install binary` → `systemctl restart ft` → `/healthz` check.

## Rollback to a known-good commit

```bash
cd /opt/ft/src
sudo -u ft git log --oneline -10        # find the last-good SHA
sudo -u ft git checkout <SHA>
sudo -u ft make build
sudo install -o ft -g ft -m 0755 ./bin/ft /opt/ft/bin/ft
sudo systemctl restart ft
```

## Backup + restore

Backups land in `/var/backups/ft/ft-YYYY-MM-DD.db`, owned by `ft:ft`, retained
14 days. Cron entry at `/etc/cron.d/ft-backup` runs the script at 03:15 UTC daily.

Restore:
```bash
sudo systemctl stop ft
sudo -u ft cp /var/backups/ft/ft-2026-05-15.db /var/lib/ft/ft.db
sudo systemctl start ft
```

Verify a backup manually any time:
```bash
sudo -u ft /opt/ft/bin/backup-db.sh
ls -la /var/backups/ft/
```

## Adding a deploy key (one-time, for `ft` user → GitHub)

The `ft` system user needs a separate SSH key trusted by the GitHub repo so
`git pull` works under cron / deploy.sh.

```bash
# Generate a key with no passphrase, in /var/lib/ft/.ssh/
sudo install -d -o ft -g ft -m 0700 /var/lib/ft/.ssh
sudo -u ft ssh-keygen -t ed25519 -N "" -C "ft@jarvis" -f /var/lib/ft/.ssh/id_ed25519

# Print public key:
sudo cat /var/lib/ft/.ssh/id_ed25519.pub
```

Add that public key at https://github.com/finbarrcurran/ft-/settings/keys →
*Add deploy key* → title `Jarvis`, paste the key, **Allow write access:** OFF
(read-only is enough — we only pull, never push from jarvis).

Then:
```bash
# Trust github.com host key on first connection
sudo -u ft ssh -o StrictHostKeyChecking=accept-new -T git@github.com || true
```

## Cloudflare WAF login rate limit

Configured in the dashboard. See `docs/WAF.md` for the rule definition.

## Troubleshooting

- **`systemctl restart ft` succeeds but service is unhealthy**:
  `sudo journalctl -u ft -n 100 --no-pager`
- **Backup cron silent**:
  `tail -50 /var/log/ft/backup.log`
- **deploy.sh fails on `git pull`**: usually a deploy-key problem; re-do the
  Adding a deploy key section.
