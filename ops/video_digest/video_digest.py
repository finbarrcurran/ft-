#!/usr/bin/env python3
"""SC-43 Video Digest P0 — Jarvis-side package builder (Jordi Visser, Benjamin Cowen).

Per video it writes an immutable package, the future FT ingest contract:

    /var/lib/video_digest/<source>/<YYYY-MM-DD>_<video_id>/
        transcript.txt   clean plain text
        transcript.vtt   timed cues (caption cues, or Whisper segments)
        frames/f_<secs>.jpg
        meta.json

No LLM calls (D4). Captions come from the existing yt_transcribe/pull_transcript
engine (D5); only the Whisper fallback is local here, because it must emit a
.vtt from segment timings (A3). Handover: FT_Build_Handover_Video_Digest_P0_2026-09-21.md
(v1.1 — §13 supersedes earlier sections).

Usage:
    video_digest.py --source {jordi|cowen|all} [--backfill N] [--since YYYY-MM-DD]
                    [--video-id ID] [--keep-video] [--force]

SC-52 P3 (range backfill, captions only, no frames):
    video_digest.py --source cowen --from 2025-01-01 --transcripts-only --max-videos 100
    video_digest.py --source jordi --from 2025-10-27 --no-frames
  --from/--to            publish-date range (newest first; stops at --from). Video metadata is cached in
                         <package_root>/cache/<source>_infos.json so a run is resumable.
  --no-frames            no video download, no frames (meta.extract stays true: FT's sweep digests it)
  --transcripts-only     implies --no-frames and writes meta.extract=false: FT stores and indexes the
                         transcript but the sweep never spends LLM money on it (`ft video-digest-extract`
                         digests such packages on request)
  --max-videos N         build at most N packages this run (nightly batches of ~100)
  --sleep S              seconds to wait between videos (default 6; polite pacing)
  --captions-only        never fall back to Whisper (a video without captions is reported and skipped)
Stops at once and reports if YouTube rate-limits (HTTP 429 / "Sign in to confirm").
"""

import argparse
import datetime as dt
import json
import logging
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from zoneinfo import ZoneInfo

import imagehash
import yaml
from PIL import Image

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(Path.home() / "scripts"))
import yt_transcribe  # noqa: E402  — also puts ~/.local/bin on PATH

YT_DLP = yt_transcribe.YT_DLP
log = logging.getLogger("video_digest")

TS_LINE = re.compile(r"^\s*((?:\d+:)?\d{1,2}:\d{2}\.\d{3})\s*-->\s*((?:\d+:)?\d{1,2}:\d{2}\.\d{3})")
PTS_TIME = re.compile(r"pts_time:(\d+(?:\.\d+)?)")
PACKAGE_DIR = re.compile(r"^\d{4}-\d{2}-\d{2}_(.+)$")


# --------------------------------------------------------------------------- helpers

def run(cmd, timeout=1800):
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)


def tool_version(cmd):
    try:
        return run(cmd, timeout=30).stdout.strip().splitlines()[0]
    except Exception as e:                                   # noqa: BLE001
        return f"unavailable ({e})"


def dir_bytes(path: Path) -> int:
    return sum(p.stat().st_size for p in path.rglob("*") if p.is_file())


def vtt_ts(s: str) -> float:
    parts = s.split(":")
    secs = float(parts[-1])
    mins = int(parts[-2]) if len(parts) >= 2 else 0
    hrs = int(parts[-3]) if len(parts) >= 3 else 0
    return hrs * 3600 + mins * 60 + secs


def fmt_ts(t: float) -> str:
    h, rem = divmod(t, 3600)
    m, s = divmod(rem, 60)
    return f"{int(h):02d}:{int(m):02d}:{s:06.3f}"


def parse_vtt(path: Path):
    """Return [(start, end, clean_text)]. Tolerates YouTube auto-caption inline tags."""
    cues, cur = [], None
    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        m = TS_LINE.match(line)
        if m:
            cur = [vtt_ts(m.group(1)), vtt_ts(m.group(2)), []]
            cues.append(cur)
            continue
        if cur is None:
            continue
        if not line.strip():
            cur = None
            continue
        text = re.sub(r"<[^>]+>", "", line).replace("&nbsp;", " ").strip()
        if text:
            cur[2].append(text)
    return [(s, e, " ".join(t)) for s, e, t in cues if t]


RATE_LIMIT = re.compile(r"429|Too Many Requests|Sign in to confirm|not a bot", re.I)


class RateLimited(RuntimeError):
    """YouTube is throttling us: stop the run and report (never retry in a loop)."""


# --------------------------------------------------------------------------- discovery

def existing_packages(src_dir: Path) -> dict:
    """video_id -> package dir, for complete packages only."""
    out = {}
    if not src_dir.is_dir():
        return out
    for d in src_dir.iterdir():
        m = PACKAGE_DIR.match(d.name)
        if not (m and d.is_dir()):
            continue
        try:
            if json.loads((d / "meta.json").read_text()).get("status") == "complete":
                out[m.group(1)] = d
        except (OSError, ValueError):
            pass
    return out


def flat_ids(channel_id: str, limit: int) -> list:
    url = f"https://www.youtube.com/channel/{channel_id}/videos"
    r = run([YT_DLP, "--flat-playlist", "--playlist-end", str(limit), "--print", "%(id)s", url], timeout=300)
    if r.returncode != 0:
        raise RuntimeError(f"discovery failed for {channel_id}: {r.stderr.strip()[-600:]}")
    return [l.strip() for l in r.stdout.splitlines() if l.strip() and l.strip() != "NA"]


def video_info(vid: str) -> dict:
    r = run([YT_DLP, "-J", "--skip-download", "--no-playlist", f"https://www.youtube.com/watch?v={vid}"], timeout=300)
    if r.returncode != 0:
        if RATE_LIMIT.search(r.stderr or ""):
            raise RateLimited(f"rate limited on metadata for {vid}: {r.stderr.strip()[-300:]}")
        raise RuntimeError(f"metadata failed for {vid}: {r.stderr.strip()[-600:]}")
    j = json.loads(r.stdout)
    return {
        "id": j["id"],
        "title": j.get("title") or "",
        "channel_id": j.get("channel_id"),
        "upload_date": j.get("upload_date"),          # YYYYMMDD
        "timestamp": j.get("timestamp"),              # epoch, may be None
        "duration": int(j.get("duration") or 0),
        "live_status": j.get("live_status"),
    }


def local_date(info: dict, tz: ZoneInfo) -> dt.date:
    if info.get("timestamp"):
        return dt.datetime.fromtimestamp(info["timestamp"], tz).date()
    return dt.datetime.strptime(info["upload_date"], "%Y%m%d").date()


def assign_flagship(src: dict, new: list, existing_meta: list) -> None:
    """Sets info['is_flagship'] on each new video. One flagship per ISO week (config tz)."""
    rule = src.get("flagship")
    if src.get("select") != "flag_flagship" or not rule:
        for v in new:
            v["is_flagship"] = False
        return
    tz = ZoneInfo(rule.get("timezone", "UTC"))
    want = rule.get("weekday", "sunday").lower()
    pool = [dict(v, _new=True) for v in new] + [
        {"id": m["video_id"], "duration": m.get("duration_sec", 0), "timestamp": m.get("_timestamp"),
         "upload_date": m["published_at"].replace("-", ""), "is_flagship": m.get("is_flagship"), "_new": False}
        for m in existing_meta]
    weeks = {}
    for v in pool:
        weeks.setdefault(local_date(v, tz).isocalendar()[:2], []).append(v)
    for items in weeks.values():
        on_day = [v for v in items if local_date(v, tz).strftime("%A").lower() == want]
        cands = on_day or items          # fallback: longest in the week
        winner = max(cands, key=lambda v: v.get("duration") or 0)
        for v in items:
            if v["_new"]:
                v_orig = next(n for n in new if n["id"] == v["id"])
                v_orig["is_flagship"] = v is winner
        old_flag = [v for v in items if not v["_new"] and v.get("is_flagship")]
        if winner["_new"] and old_flag:
            log.warning("flagship for week moved to %s; immutable package %s keeps its old flag",
                        winner["id"], old_flag[0]["id"])


# --------------------------------------------------------------------------- transcript

def transcript(url: str, vid: str, stage: Path, slug: str, whisper_model: str, captions_only: bool = False) -> dict:
    """Captions first (reused engine), Whisper fallback with segment-level .vtt (A3)."""
    txt = yt_transcribe.try_captions(url, stage, slug)
    # pull_transcript prefers the verbatim "en-orig" track; "en" can be machine-translated
    # when a logged-in session is used.
    vtt = next((stage / f"{slug}_{vid}.{lang}.vtt" for lang in ("en-orig", "en")
                if (stage / f"{slug}_{vid}.{lang}.vtt").exists()), None)
    if txt and Path(txt).exists() and Path(txt).stat().st_size > 40 and vtt:
        track = vtt.name.split(".")[-2]
        Path(txt).rename(stage / "transcript.txt")
        vtt.rename(stage / "transcript.vtt")
        return {"method": "captions", "cue_granularity": "caption_cue", "caption_track": track}

    if captions_only:
        raise RuntimeError("no captions available (captions-only run: Whisper fallback disabled)")
    log.info("%s: no captions — Whisper fallback (%s)", vid, whisper_model)
    for f in stage.glob(f"{slug}_{vid}*"):
        f.unlink()
    r = run([YT_DLP, "-f", "bestaudio[ext=m4a]/bestaudio", "--no-playlist",
             "-o", str(stage / "audio.%(ext)s"), url])
    audio = next(iter(stage.glob("audio.*")), None)
    if r.returncode != 0 or audio is None:
        raise RuntimeError(f"audio download failed: {r.stderr.strip()[-600:]}")
    from faster_whisper import WhisperModel
    t0 = time.monotonic()
    try:
        model = WhisperModel(whisper_model, device="cpu", compute_type="int8")
        segs, info = model.transcribe(str(audio), vad_filter=True)
        segs = list(segs)
    finally:
        audio.unlink(missing_ok=True)
    elapsed = time.monotonic() - t0
    if not segs:
        raise RuntimeError("Whisper produced no segments")
    with open(stage / "transcript.vtt", "w", encoding="utf-8") as fh:
        fh.write("WEBVTT\n\n")
        for s in segs:
            fh.write(f"{fmt_ts(s.start)} --> {fmt_ts(s.end)}\n{s.text.strip()}\n\n")
    (stage / "transcript.txt").write_text(" ".join(s.text.strip() for s in segs), encoding="utf-8")
    return {"method": "whisper", "cue_granularity": "segment", "whisper_model": whisper_model,
            "whisper_sec": round(elapsed, 1), "audio_sec": round(info.duration, 1),
            "whisper_min_per_hour": round(elapsed / 60 / (info.duration / 3600), 1)}


# --------------------------------------------------------------------------- frames

def frame_config(cfg: dict, src: dict) -> dict:
    """Global frame settings, with per-source overrides from sources.<key>.frames.
    `extra_cue_phrases` is appended to the global phrase list rather than replacing it."""
    over = dict(src.get("frames") or {})
    extra = over.pop("extra_cue_phrases", [])
    fc = {**cfg["frames"], **over}
    fc["cue_phrases"] = list(fc["cue_phrases"]) + [p for p in extra if p not in fc["cue_phrases"]]
    return fc


def scene_times(video: Path, fc: dict) -> list:
    vf = f"fps={fc['scene_sample_fps']},scale=320:-2,select='gt(scene,{fc['scene_threshold']})',showinfo"
    r = run(["ffmpeg", "-hide_banner", "-nostats", "-i", str(video), "-vf", vf, "-an", "-f", "null", "-"])
    return sorted({round(float(t), 2) for t in PTS_TIME.findall(r.stderr)})


def cue_times(cues: list, fc: dict) -> list:
    phrases = [p.lower() for p in fc["cue_phrases"]]
    out = []
    for start, _end, text in cues:
        low = text.lower()
        if any(p in low for p in phrases):
            out.append((start + fc["cue_offset_sec"], text[:160]))
    return out


def grab(video: Path, t: float, png: Path) -> bool:
    r = run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-ss", f"{t:.2f}", "-i", str(video),
             "-frames:v", "1", "-y", str(png)], timeout=120)
    return r.returncode == 0 and png.exists()


def build_frames(url: str, stage: Path, duration: int, cues: list, fc: dict, keep_video: bool) -> dict:
    tmp = Path(tempfile.mkdtemp(prefix="vd_", dir="/var/tmp"))
    try:
        r = run([YT_DLP, "-f", fc["video_format"], "--no-playlist", "-o", str(tmp / "video.%(ext)s"), url])
        video = next((p for p in tmp.glob("video.*") if not p.name.endswith(".part")), None)
        if r.returncode != 0 or video is None:
            raise RuntimeError(f"video download failed: {r.stderr.strip()[-600:]}")
        video_bytes = video.stat().st_size

        scenes = scene_times(video, fc)
        cue_hits = cue_times(cues, fc)
        # Candidate order = preference order: cue-anchored first, then scene changes.
        cands = [("cue", t, txt) for t, txt in cue_hits] + [("scene", t, None) for t in scenes]
        gap_ok = []
        for origin, t, txt in cands:
            if 0 <= t < max(duration - 1, 1) and all(abs(t - g[1]) >= fc["min_gap_sec"] for g in gap_ok):
                gap_ok.append((origin, t, txt))

        fdir = stage / "frames"
        fdir.mkdir()
        kept = []                       # (origin, t, txt, phash, candidate jpg in tmp)
        dropped_dupe = 0
        for origin, t, txt in gap_ok:
            png = tmp / "grab.png"
            if not grab(video, t, png):
                continue
            with Image.open(png) as im:
                img = im.convert("RGB")
            h = imagehash.phash(img)
            if any(h - k[3] <= fc["phash_max_distance"] for k in kept):
                dropped_dupe += 1
                continue
            cand = tmp / f"cand_{t:.2f}.jpg"       # spill to disk; don't hold 720p frames in RAM
            img.save(cand, "JPEG", quality=fc["jpeg_quality"])
            kept.append((origin, t, txt, h, cand))

        # Cap: keep every cue frame (up to cap), fill the rest with scene frames spread
        # evenly across the video rather than the first N.
        cue_k = [k for k in kept if k[0] == "cue"][: fc["max_frames"]]
        scene_k = [k for k in kept if k[0] == "scene"]
        room = fc["max_frames"] - len(cue_k)
        if len(scene_k) > room:
            step = len(scene_k) / room if room > 0 else 0
            scene_k = [scene_k[int(i * step)] for i in range(room)] if room > 0 else []
        final = sorted(cue_k + scene_k, key=lambda k: k[1])

        frames = []
        for origin, t, txt, _h, cand in final:
            name = f"frames/f_{int(t)}.jpg"
            # copy, not move: a rename from /var/tmp would keep group curran and bypass
            # the package dir's setgid, leaving ft unable to read the frame (AC11).
            shutil.copyfile(cand, stage / name)
            frames.append({"file": name, "ts_sec": int(t), "origin": origin, "cue_text": txt})
        return {"frames": frames,
                "counts": {"scene_candidates": len(scenes), "cue_candidates": len(cue_hits),
                           "after_min_gap": len(gap_ok), "near_dupes_dropped": dropped_dupe,
                           "kept": len(frames)},
                "temp_video_bytes": video_bytes}
    finally:
        if keep_video:
            log.info("--keep-video: temp video left at %s", tmp)
        else:
            shutil.rmtree(tmp, ignore_errors=True)


# --------------------------------------------------------------------------- package

def build_package(key: str, src: dict, info: dict, cfg: dict, args) -> dict:
    root = Path(cfg["package_root"]) / key
    root.mkdir(parents=True, exist_ok=True)
    vid = info["id"]
    date = dt.datetime.strptime(info["upload_date"], "%Y%m%d").strftime("%Y-%m-%d")
    final = root / f"{date}_{vid}"
    stage = root / f".partial-{vid}"
    shutil.rmtree(stage, ignore_errors=True)
    stage.mkdir()
    url = f"https://www.youtube.com/watch?v={vid}"
    t0 = time.monotonic()
    try:
        tr = transcript(url, vid, stage, key, cfg["whisper_model"], getattr(args, "captions_only", False))
        cues = parse_vtt(stage / "transcript.vtt")
        if not cues:
            raise RuntimeError("transcript.vtt has no cues")
        coverage = round(cues[-1][1] / info["duration"], 3) if info["duration"] else None
        fc = frame_config(cfg, src)
        no_frames = getattr(args, "no_frames", False) or getattr(args, "transcripts_only", False)
        if no_frames:                         # SC-52 P3: captions only, no video download
            (stage / "frames").mkdir()
            fr = {"frames": [], "temp_video_bytes": 0,
                  "counts": {"scene_candidates": 0, "cue_candidates": 0, "after_min_gap": 0, "near_dupes_dropped": 0, "kept": 0}}
        else:
            fr = build_frames(url, stage, info["duration"], cues, fc, args.keep_video)

        meta = {
            "schema_version": 1,
            "source": key,
            "channel_id": src["channel_id"],
            "video_id": vid,
            "title": info["title"],
            "url": f"https://youtu.be/{vid}",
            "published_at": date,
            "duration_sec": info["duration"],
            "is_flagship": info.get("is_flagship", False),
            "transcript_method": tr["method"],
            "transcript_caption_track": tr.get("caption_track"),
            "transcript_cue_granularity": tr["cue_granularity"],
            "transcript_cue_count": len(cues),
            "transcript_coverage": coverage,
            "frames": fr["frames"],
            "frame_counts": fr["counts"],
            "frame_params": {"scene_threshold": fc["scene_threshold"], "phash_max_distance": fc["phash_max_distance"],
                             "cue_phrases": fc["cue_phrases"]},
            "pipeline_version": cfg["pipeline_version"],
            "extract": not getattr(args, "transcripts_only", False),
            "status": "incomplete",
            "created_at": dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        if tr["method"] == "whisper":
            meta["whisper"] = {k: tr[k] for k in ("whisper_model", "whisper_sec", "audio_sec", "whisper_min_per_hour")}
        required = [stage / "transcript.txt", stage / "transcript.vtt", stage / "frames"] + \
                   [stage / f["file"] for f in fr["frames"]]
        missing = [str(p.name) for p in required if not p.exists()]
        if missing:
            raise RuntimeError(f"missing files: {missing}")
        meta["status"] = "complete"
        (stage / "meta.json").write_text(json.dumps(meta, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

        if final.exists():                       # only reachable with --force
            old = root / f".replaced-{vid}"
            shutil.rmtree(old, ignore_errors=True)
            final.rename(old)
            stage.rename(final)
            shutil.rmtree(old, ignore_errors=True)
        else:
            stage.rename(final)
        return {"source": key, "video_id": vid, "title": info["title"], "status": "complete",
                "package": str(final), "bytes": dir_bytes(final), "method": tr["method"],
                "caption_track": tr.get("caption_track"),
                "coverage": coverage, "cues": len(cues), "frames": fr["counts"],
                "is_flagship": meta["is_flagship"], "temp_video_bytes": fr["temp_video_bytes"],
                "elapsed_sec": round(time.monotonic() - t0, 1), "whisper": meta.get("whisper")}
    except Exception as e:                                   # noqa: BLE001
        shutil.rmtree(stage, ignore_errors=True)
        log.error("%s/%s failed: %s", key, vid, e)
        if RATE_LIMIT.search(str(e)):
            return {"source": key, "video_id": vid, "title": info.get("title"), "status": "rate_limited",
                    "error": str(e)[-800:], "elapsed_sec": round(time.monotonic() - t0, 1)}
        return {"source": key, "video_id": vid, "title": info.get("title"), "status": "failed",
                "error": str(e)[-800:], "elapsed_sec": round(time.monotonic() - t0, 1)}


def collect_range(key: str, src: dict, cfg: dict, args) -> tuple:
    """SC-52 P3: video metadata for every video published in [--from, --to], newest first. Cached on
    disk so an interrupted run resumes without asking YouTube again. Returns (infos, results)."""
    cache_dir = Path(cfg["package_root"]) / "cache"
    cache_dir.mkdir(parents=True, exist_ok=True)
    cache_path = cache_dir / f"{key}_infos.json"
    try:
        cache = json.loads(cache_path.read_text())
    except (OSError, ValueError):
        cache = {}
    ids = flat_ids(src["channel_id"], 5000)
    lo = args.from_date.replace("-", "")
    hi = (args.to_date or "9999-12-31").replace("-", "")
    infos, results, fetched = [], [], 0
    for v in ids:
        info = cache.get(v)
        if info is None:
            try:
                info = video_info(v)
            except RateLimited as e:
                results.append({"source": key, "video_id": v, "status": "rate_limited", "error": str(e)})
                break
            except Exception as e:                           # noqa: BLE001
                results.append({"source": key, "video_id": v, "status": "failed", "error": str(e)[-800:]})
                continue
            cache[v] = info
            fetched += 1
            if fetched % 20 == 0:
                cache_path.write_text(json.dumps(cache))
            time.sleep(max(1, args.sleep / 3))
        if info["channel_id"] != src["channel_id"] or info.get("live_status") in ("is_upcoming", "is_live", "post_live"):
            continue
        if info["upload_date"] < lo:
            break                                            # newest first: everything after is older
        if info["upload_date"] <= hi:
            infos.append(info)
    cache_path.write_text(json.dumps(cache))
    log.info("%s: %d video(s) in %s..%s (%d metadata call(s) this run)", key, len(infos), args.from_date, args.to_date or "now", fetched)
    return infos, results


def process_range(key: str, src: dict, cfg: dict, args) -> list:
    root = Path(cfg["package_root"]) / key
    have = existing_packages(root)
    infos, results = collect_range(key, src, cfg, args)
    existing_meta = []
    for d in have.values():
        try:
            existing_meta.append(json.loads((d / "meta.json").read_text()))
        except (OSError, ValueError):
            pass
    assign_flagship(src, infos, [m for m in existing_meta if m["video_id"] not in {i["id"] for i in infos}])
    todo = [i for i in infos if args.force or i["id"] not in have]
    for i in infos:
        if i["id"] in have and not args.force:
            results.append({"source": key, "video_id": i["id"], "status": "skipped (package exists)", "package": str(have[i["id"]]), "bytes": 0})
    built = 0
    for info in todo:
        if args.max_videos and built >= args.max_videos:
            results.append({"source": key, "video_id": "-", "status": f"stopped (batch limit {args.max_videos}); {len(todo) - built} left"})
            break
        log.info("%s: packaging %s (%s) — %s", key, info["id"], info["upload_date"], info["title"])
        r = build_package(key, src, info, cfg, args)
        results.append(r)
        built += 1
        if r["status"] == "rate_limited":
            log.error("rate limited: stopping the run")
            break
        time.sleep(args.sleep)
    return results


def process_source(key: str, src: dict, cfg: dict, args) -> list:
    if getattr(args, "from_date", None):
        return process_range(key, src, cfg, args)
    root = Path(cfg["package_root"]) / key
    have = existing_packages(root)
    results = []

    if args.video_id:
        ids = [args.video_id]
    else:
        limit = args.backfill or src["max_videos_per_week"] * 2
        ids = flat_ids(src["channel_id"], limit)
    todo = [v for v in ids if args.force or v not in have]
    for v in ids:
        if v in have and not args.force:
            results.append({"source": key, "video_id": v, "status": "skipped (package exists)",
                            "package": str(have[v]), "bytes": dir_bytes(have[v])})
    if not todo:
        return results

    infos = []
    for v in todo:
        try:
            info = video_info(v)
        except Exception as e:                               # noqa: BLE001
            results.append({"source": key, "video_id": v, "status": "failed", "error": str(e)[-800:]})
            continue
        if info["channel_id"] != src["channel_id"]:
            results.append({"source": key, "video_id": v, "status": "failed",
                            "error": f"channel mismatch: {info['channel_id']} != pinned {src['channel_id']}"})
            continue
        if info["live_status"] in ("is_upcoming", "is_live", "post_live"):
            results.append({"source": key, "video_id": v, "status": f"skipped ({info['live_status']})"})
            continue
        infos.append(info)

    if not args.backfill and not args.video_id:          # weekly mode: window + per-week cap
        since = args.since or (dt.date.today() - dt.timedelta(days=cfg["lookback_days"])).isoformat()
        infos = [i for i in infos if i["upload_date"] >= since.replace("-", "")]
        per_week = {}
        for i in sorted(infos, key=lambda i: i["upload_date"], reverse=True):
            wk = dt.datetime.strptime(i["upload_date"], "%Y%m%d").isocalendar()[:2]
            per_week.setdefault(wk, []).append(i)
        infos = [i for wk in per_week.values() for i in wk[: src["max_videos_per_week"]]]

    existing_meta = []
    for d in have.values():
        try:
            existing_meta.append(json.loads((d / "meta.json").read_text()))
        except (OSError, ValueError):
            pass
    assign_flagship(src, infos, [m for m in existing_meta if m["video_id"] not in {i["id"] for i in infos}])

    for info in infos:
        log.info("%s: packaging %s — %s", key, info["id"], info["title"])
        results.append(build_package(key, src, info, cfg, args))
    return results


# --------------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--source", required=True, choices=["jordi", "cowen", "all"])
    ap.add_argument("--backfill", type=int, default=0, help="package the last N videos per source")
    ap.add_argument("--since", help="weekly mode window start YYYY-MM-DD (default: lookback_days ago)")
    ap.add_argument("--video-id", help="package one video (with --source jordi|cowen)")
    ap.add_argument("--keep-video", action="store_true")
    ap.add_argument("--force", action="store_true", help="rebuild packages that already exist")
    ap.add_argument("--config", default=str(HERE / "sources.yaml"))
    ap.add_argument("--from", dest="from_date", help="SC-52 P3: range backfill, publish date on/after YYYY-MM-DD")
    ap.add_argument("--to", dest="to_date", help="SC-52 P3: range backfill, publish date on/before YYYY-MM-DD")
    ap.add_argument("--no-frames", action="store_true", help="captions only: no video download, no frames")
    ap.add_argument("--transcripts-only", action="store_true", help="implies --no-frames; meta.extract=false (FT never sweeps it)")
    ap.add_argument("--captions-only", action="store_true", help="never fall back to Whisper")
    ap.add_argument("--max-videos", type=int, default=0, help="range mode: build at most N packages this run")
    ap.add_argument("--sleep", type=float, default=6.0, help="range mode: seconds between videos")
    args = ap.parse_args()
    if args.transcripts_only or args.no_frames or args.from_date:
        args.captions_only = True
    if args.video_id and args.source == "all":
        ap.error("--video-id needs a single --source")

    os.umask(0o027)                                    # files 640, dirs 750 (+setgid inherited) — A4
    cfg = yaml.safe_load(open(args.config))
    logs = Path(cfg["package_root"]) / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s",
                        handlers=[logging.StreamHandler(sys.stderr), logging.FileHandler(logs / f"run_{stamp}.log")])

    keys = ["jordi", "cowen"] if args.source == "all" else [args.source]
    summary = {
        "run_at": stamp, "args": vars(args),
        "yt_dlp_version": tool_version([YT_DLP, "--version"]),
        "ffmpeg_version": tool_version(["ffmpeg", "-version"]),
        "channel_ids": {k: cfg["sources"][k]["channel_id"] for k in keys},
        "results": [],
    }
    t0 = time.monotonic()
    for k in keys:
        try:
            summary["results"] += process_source(k, cfg["sources"][k], cfg, args)
        except Exception as e:                               # noqa: BLE001
            log.error("%s: source failed: %s", k, e)
            summary["results"].append({"source": k, "status": "failed", "error": str(e)[-800:]})
    summary["elapsed_sec"] = round(time.monotonic() - t0, 1)
    (logs / f"run_{stamp}.json").write_text(json.dumps(summary, indent=2, ensure_ascii=False) + "\n")

    print(f"\n=== SC-43 video_digest run {stamp} ({summary['elapsed_sec']}s) ===")
    print(f"yt-dlp {summary['yt_dlp_version']} | {summary['ffmpeg_version']}")
    for k, v in summary["channel_ids"].items():
        print(f"channel {k}: {v}")
    for r in summary["results"]:
        line = f"[{r['source']}] {r.get('video_id', '-')}  {r['status']}"
        if r["status"] == "complete":
            fc = r["frames"]
            line += (f"  {r['method']}[{r.get('caption_track') or '-'}] cov={r['coverage']} cues={r['cues']} frames={fc['kept']}"
                     f" (scene {fc['scene_candidates']}, cue {fc['cue_candidates']}, dupes -{fc['near_dupes_dropped']})"
                     f" flagship={r['is_flagship']} {r['bytes']/1e6:.1f}MB ({r['elapsed_sec']}s)")
            if r.get("whisper"):
                line += f"  whisper={r['whisper']['whisper_min_per_hour']} min/hr"
        elif "bytes" in r:
            line += f"  {r['bytes']/1e6:.1f}MB"
        if r.get("error"):
            line += f"\n      ERROR: {r['error'][-300:]}"
        print(line)
    print(f"summary: {logs / f'run_{stamp}.json'}")
    sys.exit(0 if all(r["status"] not in ("failed", "rate_limited") for r in summary["results"]) else 1)


if __name__ == "__main__":
    main()
