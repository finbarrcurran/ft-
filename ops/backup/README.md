# ops/backup — SC-37 Backup / DR (UNTESTED DRAFTS)

Staged 2026-07-03 per `FT_Build_Handover_SC37_Backup_DR_2026-07-03.md`. **Nothing
here runs yet** — blocked on §P0 (Fin's Backblaze B2 bucket + key → `/etc/ft/backup.env`).

- `ft-backup.sh` — P1 nightly: `.backup` → `PRAGMA integrity_check` → `restic backup` → `restic forget` (7d/4w/6m).
- `ft-restore-verify.sh` — P2 weekly: `restic restore` → integrity → row-count sanity vs live → `restic check`.
- `RESTORE_RUNBOOK.md` — P3 human runbook + quarterly drill (§P4 closing AC).

Wire-up when creds land: create `/etc/ft/backup.env` (600, root), `restic init`,
`chmod +x *.sh`, add the two systemd timers (03:00 / Sun 03:30 UTC), then run the
kill-tests (bad path / corrupt snapshot / revoked key / unreachable B2 → alert
fires, names the stage, clears on revert). SC-37 does not close until Fin passes
the manual drill.
