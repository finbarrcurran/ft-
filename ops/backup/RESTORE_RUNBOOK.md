# FT — DB Restore Runbook (SC-37)

> **Status: LIVE — wired + tested 2026-07-04.** Nightly backup + weekly restore-verify
> run under systemd; the B2 repo is initialised and holds snapshots. This runbook is
> the human procedure for a real restore + the quarterly manual drill (the closing AC).

## What is backed up
The live FT database `/var/lib/ft/ft.db` only (v1 scope). Git covers all code + doctrine.
Ad-hoc `ft.db.bak_*` files on jarvis are same-disk and untested — **they are NOT the backup.**

## Where things live
- Backup script:  `/opt/ft/src/ops/backup/ft-backup.sh`  (systemd `ft-backup.timer`, 03:00 UTC nightly)
- Verify script:  `/opt/ft/src/ops/backup/ft-restore-verify.sh`  (`ft-restore-verify.timer`, Sun 03:30 UTC)
- Credentials:    `/etc/ft/backup.env`  (root, mode 600 — B2 key + `RESTIC_REPOSITORY` + `RESTIC_PASSWORD`)
- Repo:           `b2:ft-backup-jarvis-cur:/`  (Backblaze B2, restic-encrypted)
- Retention:      7 daily / 4 weekly / 6 monthly (auto-pruned)
- Alerts:         Telegram `@FinsFTAlerts_bot` on any failure; **silent when green**.

## ⚠️ The one thing you must keep off-box
The **restic passphrase** (`RESTIC_PASSWORD` in `/etc/ft/backup.env`). If jarvis dies AND
this passphrase is lost, the backups are **unrecoverable**. It must live in your password
manager. To read it to save it:  `sudo grep '^RESTIC_PASSWORD=' /etc/ft/backup.env`

## Restore procedure (and the quarterly drill)
Run as root on jarvis. **NOTE:** `restic restore --target DIR` recreates the file at its
*original* absolute path under DIR (i.e. `DIR/tmp/ft-backup/ft.db`) — so we use `restic dump`
to extract the single DB to a flat path instead. (Verified 2026-07-05.)
```
set -a; . /etc/ft/backup.env; set +a
restic snapshots                                               # pick one (usually 'latest')
restic dump latest /tmp/ft-backup/ft.db > /tmp/ft-restore.db   # extract the DB to a flat file
sqlite3 /tmp/ft-restore.db 'PRAGMA integrity_check;'           # must print: ok
sqlite3 /tmp/ft-restore.db "SELECT COUNT(*) FROM theses_index WHERE status='locked';"   # >= 50
```
Prove it boots against the restored copy (read-only, throwaway port):
```
FT_DB_PATH=/tmp/ft-restore.db FT_ADDR=127.0.0.1:8099 /opt/ft/bin/ft &
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8099/healthz   # expect 200
kill %1; rm -f /tmp/ft-restore.db
```
Real disaster recovery only (otherwise this is verify-only): stop `ft`, back up the current
`/var/lib/ft/ft.db`, then `restic dump latest /tmp/ft-backup/ft.db > /var/lib/ft/ft.db`
(chown ft:ft), restart `ft`.

## Cadence
- Nightly backup 03:00 UTC · Weekly automated restore-verify Sun 03:30 UTC.
- **Quarterly manual drill:** Fin runs the restore procedure above unaided. Next drill: set on close.
