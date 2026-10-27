# ops/video_digest — Jarvis-side video package builder (SC-43 P0, SC-52 P3)

`video_digest.py`, `sources.yaml`, `run_weekly.sh` are what runs on Jarvis as `curran` from
`~curran/scripts/video_digest/` (the weekly `video-digest.timer`). This directory is the version-controlled
copy; **installing a change is a manual copy** (`scp` to `~curran/scripts/video_digest/`), nothing deploys it.

SC-52 P3 added a range backfill (captions only, no frames):

```
~/.venvs/video_digest/bin/python ~/scripts/video_digest/video_digest.py --source jordi --from 2025-10-27 --no-frames --max-videos 25
~/.venvs/video_digest/bin/python ~/scripts/video_digest/video_digest.py --source cowen --from 2025-01-01 --transcripts-only --max-videos 100
```
`--transcripts-only` writes `meta.extract=false`: FT stores and indexes the transcript but its sweep never
spends LLM money on the package. `ft video-digest-extract --source cowen --flagship-only --newest-first --max-cost N`
digests those packages on request, with a hard ceiling. Video metadata is cached in
`/var/lib/video_digest/cache/<source>_infos.json` (a run resumes without asking YouTube again); a run stops at
once and reports if YouTube rate-limits.
