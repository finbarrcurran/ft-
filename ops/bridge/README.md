# ops/bridge — SC-45 Google Drive bridge (Jarvis ↔ Claude)

Moves what Claude (claude.ai) needs to read onto a dedicated Drive folder, `FT-Bridge/`,
using rclone with the **`drive.file`** scope (rclone can only see files it created itself —
never the rest of Fin's Drive). Infrastructure only: no FT binary involved.

| File | What | Installed to |
|---|---|---|
| `ft-bridge-spec` | exports the **live** master spec (the DB row) and the archive in `docs/spec-archive/` as Google Docs into `FT-Bridge/spec/` whenever either (or this script) changes | `/usr/local/sbin/ft-bridge-spec` (root-owned) |
| `ft-bridge-spec.service` / `.timer` | runs it every 10 minutes (uploads only on change) | `/etc/systemd/system/` |
| `ft-bridge-library` (+ `.service` / `.timer`) | **SC-47:** mirrors the research library (repo `ft_specs/`, `handovers/`, `crypto/`, scoring logs, methodology registry) plus two DB-generated live tallies and `FT-library-INDEX` to `FT-Bridge/library/` (one-way; Claude reads only). Every 10 min; uploads only changed docs; `flock` guards overlap; touches `/var/lib/ft/data/bridge/library.ok` for the connectivity monitor | `/usr/local/sbin/ft-bridge-library` |
| `user-scripts/1_authorize.sh` | **one-time** Google consent for the rclone remote `ftbridge` (needs a human + a browser) | `~curran/scripts/ft_bridge/` |
| `user-scripts/2_create_folder.sh` | creates `FT-Bridge/`, pins the remote to it, writes placeholders | same |
| `user-scripts/ft_bridge_push.sh <local> <remote path>` | replace one file in place (same Drive file ID) — the P5 report will use this | same |
| `user-scripts/ft_bridge_note.sh <who> <text>` | append a dated line to `notes/shared-notes.md` | same |

## Install / rebuild

```
sudo /opt/ft/src/ops/bridge/install.sh
```

Idempotent. If the rclone remote is missing it says so; recreate it with
`1_authorize.sh` then `2_create_folder.sh` (the consent step cannot be automated).
`~/.config/rclone/rclone.conf` is inside the jarvis-wide restic scope, so a restore
normally brings the remote back with it.

## Why the exporter is copied, not run from the repo

It runs as **root** (to read the FT database) and then drops to `curran` for the upload.
`/opt/ft/src` is writable by the `ft` user, so running a root job straight out of it would let
`ft` edit what root executes. `install.sh` copies it to `/usr/local/sbin` instead.

## What lands on Drive

`FT-master-spec` (whole working spec), `FT-spec-current` (index + freshness),
`archive/FT-spec-archive-{INDEX,changelog-NN,history-NN}`. Every file ends with a short tail
line after its END marker (readers sometimes clip the last 10–40 characters). The exporter
refuses to sync if the archive directory is missing (it would delete the Drive copies).

## Check it

```
systemctl list-timers ft-bridge-spec.timer
sudo journalctl -u ft-bridge-spec -n 5 --no-pager -o cat      # "uploaded FT spec vX at …" on a change
sudo /usr/local/sbin/ft-bridge-spec --force                   # re-upload now
rclone lsf -R ftbridge:                                       # as curran
```

## Library (SC-47)

```
sudo journalctl -u ft-bridge-library -n 5 --no-pager -o cat    # "library synced at repo <sha>: N uploaded, M deleted"
sudo /usr/local/sbin/ft-bridge-library --force                  # re-upload every document
rclone lsf -R ftbridge:library --drive-export-formats txt       # as curran
```
Source of truth is the research clone `/var/lib/ft/research` (FT's own GitHub sync keeps it current) and the FT database; never edit on Drive. Per-document hashes are in `/var/lib/ft_bridge/library/library.state`; delete that file to force a full re-sync. If a run fails the state is not advanced and the next tick retries; the monitor alerts when `library.ok` is older than 2 h. A first full upload (~140 docs) takes about 10 minutes.
