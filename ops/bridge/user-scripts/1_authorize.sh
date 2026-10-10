#!/bin/bash
# SC-45 step 1 — one-time Google consent for the FT-Bridge rclone remote.
# Run by Fin (needs a browser login). Scope is drive.file: rclone can only
# see files and folders it creates itself, never the rest of the Drive.
set -euo pipefail
if rclone listremotes | grep -q '^ftbridge:$'; then
  echo "Remote 'ftbridge' already exists. To redo: rclone config delete ftbridge"; exit 1
fi
echo "A Google sign-in link will appear below. Open it in the browser on your laptop,"
echo "sign in with your own Google account and click Allow."
echo
rclone config create ftbridge drive scope=drive.file
chmod 600 ~/.config/rclone/rclone.conf
echo
echo "Done. Scope recorded:"; rclone config show ftbridge | grep -E '^(type|scope)'
