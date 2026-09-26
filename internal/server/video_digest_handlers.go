package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"ft/internal/videodigest"
)

// SC-43 P1 — Video Digest tab + ingest.
//
//	GET  /api/video-digest?source=all|jordi|cowen   digests, newest first
//	POST /api/video-digest/ingest                    {"videoId":"…","force":bool}
//	                                                 empty videoId = sweep all new packages
//
// FT reads packages read-only from /var/lib/video_digest (written by the Jarvis
// P0 pipeline) and never re-fetches from YouTube. Extraction goes through the
// internal/llm governor (feature "video_digest").

var vdSweepRunning atomic.Bool

func (s *Server) handleVideoDigestList(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("source")
	switch src {
	case "", "all", "jordi", "cowen":
	default:
		writeError(w, http.StatusBadRequest, "source must be all, jordi or cowen")
		return
	}
	ds, err := s.videoDigest.List(r.Context(), src)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// SC-22 demo doctrine, SC-36.3 pattern: universe-only, enforced server-side.
	// The only portfolio-derived fields on this surface are the P3 held/watchlist
	// flags on mentions (always NULL in P1) — stripped here so P3's join needs no
	// second demo pass. Summaries/mentions themselves are public video content.
	demo := s.demoModeOn(r.Context())
	if demo {
		for i := range ds {
			for j := range ds[i].Mentions {
				ds[i].Mentions[j].Held, ds[i].Mentions[j].Watchlist = nil, nil
			}
		}
	}
	if ds == nil {
		ds = []videodigest.Digest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"digests": ds, "demo": demo, "sweepRunning": vdSweepRunning.Load()})
}

func (s *Server) handleVideoDigestIngest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VideoID string `json:"videoId"`
		Force   bool   `json:"force"`
	}
	if r.ContentLength != 0 && !decodeJSON(r, w, &req) {
		return
	}
	if req.VideoID != "" {
		// One video: synchronous (~10–40 s), well inside the tunnel's timeout.
		res := s.videoDigest.IngestVideo(r.Context(), req.VideoID, req.Force)
		status := http.StatusOK
		if res.Status == "failed" {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, res)
		return
	}
	// Sweep: can take minutes, longer than Cloudflare's request timeout, so it
	// runs in the background and logs its results.
	if !vdSweepRunning.CompareAndSwap(false, true) {
		writeJSON(w, http.StatusAccepted, map[string]any{"started": false, "reason": "a sweep is already running"})
		return
	}
	go func() {
		defer vdSweepRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		logVideoDigestSweep("manual", s.videoDigest, ctx)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// logVideoDigestSweep runs one sweep and logs a line per package plus a summary.
// Shared by the manual endpoint and the scheduled job in cmd/ft.
func logVideoDigestSweep(trigger string, vd *videodigest.Service, ctx context.Context) {
	res, err := vd.Sweep(ctx)
	if err != nil {
		slog.Error("video digest sweep", "trigger", trigger, "err", err)
		return
	}
	var ingested, failed int
	var cost float64
	for _, r := range res {
		cost += r.CostUSD
		if r.Status == "failed" {
			failed++
			slog.Warn("video digest ingest failed", "trigger", trigger, "source", r.Source, "video", r.VideoID, "err", r.Error)
			continue
		}
		ingested++
		slog.Info("video digest ingested", "trigger", trigger, "source", r.Source, "video", r.VideoID,
			"published", r.PublishedAt, "mentions", r.Mentions, "mentionsUngrounded", r.Ungrounded,
			"quotes", r.Quotes, "quotesRejected", r.QuotesRejected, "frames", r.Frames, "costUsd", r.CostUSD)
		for _, q := range r.RejectedQuotes {
			slog.Info("video digest quote rejected (not verbatim)", "video", r.VideoID, "quote", q)
		}
	}
	slog.Info("video digest sweep", "trigger", trigger, "ingested", ingested, "failed", failed, "costUsd", cost)
}

// RunVideoDigestSweep is the scheduled-job entry point (cmd/ft). It shares the
// in-flight guard with the manual endpoint so the two never overlap.
func (s *Server) RunVideoDigestSweep(ctx context.Context) {
	if !vdSweepRunning.CompareAndSwap(false, true) {
		slog.Info("video digest sweep skipped: already running")
		return
	}
	defer vdSweepRunning.Store(false)
	logVideoDigestSweep("scheduled", s.videoDigest, ctx)
}
