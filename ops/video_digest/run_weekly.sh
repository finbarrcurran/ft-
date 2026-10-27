#!/bin/bash
# SC-43 weekly wrapper. Installed 2026-09-26 as video-digest.timer (AC1–12 passed; P1 handover §0).
# Schedule: Sun 20:00 + Mon 08:00 Europe/Dublin (idempotent). Logs: /var/lib/video_digest/logs/.
set -euo pipefail
export PATH="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin"

# A2: refresh yt-dlp first — stale yt-dlp is a recurring failure cause.
# yt-dlp lives in pipx (injected curl_cffi survives upgrades).
pipx upgrade yt-dlp || echo "WARN: yt-dlp upgrade failed; continuing with $(yt-dlp --version)" >&2

exec "$HOME/.venvs/video_digest/bin/python" "$HOME/scripts/video_digest/video_digest.py" --source all "$@"
