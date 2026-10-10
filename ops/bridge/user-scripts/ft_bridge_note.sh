#!/bin/bash
# Usage: ft_bridge_note.sh <who> <note text>
# Appends one dated line to FT-Bridge/notes/shared-notes.md (same Drive file).
set -euo pipefail
[ $# -ge 2 ] || { echo "usage: $0 <who> <note>"; exit 2; }
who=$1; shift
T=$(mktemp); trap "rm -f $T" EXIT
rclone cat ftbridge:notes/shared-notes.md > "$T"
[ -s "$T" ] || { echo "could not read shared-notes.md — not writing"; exit 1; }
printf -- "- %s — %s — %s\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$who" "$*" >> "$T"
rclone copyto "$T" ftbridge:notes/shared-notes.md
