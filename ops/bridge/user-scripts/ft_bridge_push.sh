#!/bin/bash
# Usage: ft_bridge_push.sh <local file> <path inside FT-Bridge>
# e.g.   ft_bridge_push.sh /tmp/ft-snapshot.md reports/ft-snapshot.md
# Replaces the remote file in place (same Drive file ID, so links keep working).
set -euo pipefail
[ $# -eq 2 ] || { echo "usage: $0 <local file> <remote path>"; exit 2; }
exec rclone copyto "$1" "ftbridge:$2"
