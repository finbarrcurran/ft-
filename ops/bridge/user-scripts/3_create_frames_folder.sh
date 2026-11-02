#!/bin/bash
# SC-54 — create the Drive folder FT-Frames OUTSIDE FT-Bridge and a second rclone remote `ftframes`
# pinned to it. Same Google consent and drive.file scope as `ftbridge` (rclone sees only files it
# created itself), so no new consent step. Run by hand when the archive is switched on; the archive
# itself stays off until /etc/ft/frames-archive.enabled exists.
set -euo pipefail
rclone config show ftbridge | grep -q '^scope = drive.file$' || { echo "ftbridge scope is not drive.file — stopping"; exit 1; }
if ! rclone listremotes | grep -q '^ftframes:$'; then
  CID=$(rclone config show ftbridge | sed -n 's/^client_id = //p')
  CSEC=$(rclone config show ftbridge | sed -n 's/^client_secret = //p')
  TOK=$(rclone config show ftbridge | sed -n 's/^token = //p')
  rclone config create ftframes drive scope=drive.file client_id="$CID" client_secret="$CSEC" token="$TOK" --non-interactive >/dev/null
fi
if ! rclone config show ftframes | grep -q '^root_folder_id'; then
  rclone mkdir ftframes:FT-Frames
  ID=$(rclone lsjson ftframes: --dirs-only | python3 -c 'import json,sys; print([d["ID"] for d in json.load(sys.stdin) if d["Name"]=="FT-Frames"][0])')
  rclone config update ftframes root_folder_id="$ID" --non-interactive >/dev/null
  echo "FT-Frames created (outside FT-Bridge) and pinned as the ftframes remote's root"
fi
echo "ready. To switch the archive on:  sudo touch /etc/ft/frames-archive.enabled && sudo systemctl enable --now ft-frames-archive.timer"
