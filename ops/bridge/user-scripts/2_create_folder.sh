#!/bin/bash
# SC-45 step 2 — create FT-Bridge (rclone must create it itself under
# drive.file), pin the remote to it, and write the placeholder files.
set -euo pipefail
R=ftbridge
rclone config show $R | grep -q '^scope = drive.file$' || { echo "scope is not drive.file — stopping"; exit 1; }
if ! rclone config show $R | grep -q '^root_folder_id'; then
  rclone mkdir $R:FT-Bridge
  ID=$(rclone lsjson $R: --dirs-only | python3 -c 'import json,sys; print([d["ID"] for d in json.load(sys.stdin) if d["Name"]=="FT-Bridge"][0])')
  rclone config update $R root_folder_id="$ID" --non-interactive >/dev/null
  echo "FT-Bridge created and pinned as the remote's root"
fi
rclone mkdir $R:reports; rclone mkdir $R:notes
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
T=$(mktemp -d)
cat > $T/ft-snapshot.md <<MD
---
generated_at: $NOW
generator: placeholder (SC-45 transport test — the real report is P5)
---

# FT snapshot

Placeholder written by Jarvis on $NOW to prove the Drive bridge works.
No portfolio data here yet.
MD
rclone copyto $T/ft-snapshot.md $R:reports/ft-snapshot.md
if ! rclone lsf $R:notes | grep -qx 'shared-notes.md'; then
cat > $T/shared-notes.md <<MD
# FT shared notes

Running notes shared between Fin, Claude Code (cc) and Claude.ai (ai).
Append new entries at the bottom: date, who, note.

- $NOW — cc — File created by Jarvis (SC-45).
MD
rclone copyto $T/shared-notes.md $R:notes/shared-notes.md
fi
rm -rf $T
echo "--- contents of FT-Bridge:"; rclone lsf -R --format pst $R:
echo "--- read-back of the snapshot header:"; rclone cat $R:reports/ft-snapshot.md | head -4
