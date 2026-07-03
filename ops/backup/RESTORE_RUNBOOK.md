# FT — DB Restore Runbook (SC-37)

> **UNTESTED DRAFT** — becomes live once Fin provisions the Backblaze B2 bucket
> + application key and creates `/etc/ft/backup.env` (root-owned, mode 600).
> Written so it can be executed unaided, surviving Claude context loss.

## What is backed up
The live FT database `/var/lib/ft/ft.db` only (v1 scope, ruling D). Git covers
all code + doctrine. Ad-hoc `ft.db.bak_*` files on jarvis are **same-disk and
untested — they are NOT the backup** and never count as one.

## Credentials (`/etc/ft/backup.env`, mode 600, root-owned)
```
RESTIC_REPOSITORY=b2:ft-backup-jarvis:/
RESTIC_PASSWORD=<restic repo passphrase — store in your password manager too>
B2_ACCOUNT_ID=<application keyID scoped to the ft-backup-jarvis bucket>
B2_ACCOUNT_KEY=<application key>
```
Nothing here goes in git, the DB, or logs.

## One-time repo init (after creds land)
```
set -a; . /etc/ft/backup.env; set +a
restic init            # first time only
sudo -u ft ops/backup/ft-backup.sh   # first snapshot; confirm restic snapshots lists it
```

## Restore procedure (the drill)
1. `set -a; . /etc/ft/backup.env; set +a`
2. `restic snapshots`  — pick the snapshot (usually `latest`).
3. `restic restore latest --target /tmp/ft-restore`
4. Verify: `sqlite3 /tmp/ft-restore/ft.db 'PRAGMA integrity_check;'` → must be `ok`.
5. Sanity: `sqlite3 /tmp/ft-restore/ft.db 'SELECT COUNT(*) FROM theses_index;'` → ≥54.
6. Prove usable: point a throwaway FT instance at the restored copy on a temp
   port (read-only), confirm it boots and `/healthz` is 200:
   `FT_DB_PATH=/tmp/ft-restore/ft.db FT_ADDR=127.0.0.1:8099 /opt/ft/bin/ft &`
   then `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8099/healthz`.
   (Confirm the exact env var names against cmd/ft at drill time.)
7. To go live on the restored copy: stop `ft`, back up the current
   `/var/lib/ft/ft.db`, `cp /tmp/ft-restore/ft.db /var/lib/ft/ft.db` (as `ft`),
   restart. Only if a real disaster — otherwise this is verify-only.

## Cadence (ruling E)
- Nightly backup 03:00 UTC (`ft-backup.sh`, timer TBD).
- Weekly automated restore-verify Sun 03:30 UTC (`ft-restore-verify.sh`).
- Quarterly **manual** drill: Fin runs steps 1–6 unaided. Next drill: set on close.
