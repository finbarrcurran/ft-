package server

// SC-48 — daily warm-up of the request-driven feeds (CNN Fear & Greed, market news,
// crypto news, crypto Fear & Greed). Those feeds are only fetched when a tab or card
// is opened (then cached 60 minutes), so on a quiet week they look stale and nobody
// notices when one silently breaks. The warm-up calls the real handlers (the same code
// path a user request takes, including cache and stale fallback) once a day, so each
// feed is always fresh and the connectivity monitor means something.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"ft/internal/monitor"
)

// WarmFeeds runs each feed handler once and alerts (Telegram, once per run) for any
// feed that still comes back empty or errored.
func (s *Server) WarmFeeds(ctx context.Context) {
	type feed struct {
		name    string
		path    string
		handler http.HandlerFunc
	}
	feeds := []feed{
		{"CNN Fear & Greed", "/api/feargreed/stocks", s.handleFearGreedStocks},
		{"Crypto Fear & Greed", "/api/feargreed", s.handleFearGreed},
		{"Market news", "/api/news/market", s.handleMarketNews},
		{"Crypto news", "/api/news/crypto", s.handleCryptoNews},
	}
	var failed []string
	for _, f := range feeds {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, f.path, nil).WithContext(ctx)
		f.handler(rec, req)
		if reason := feedProblem(rec.Body.Bytes()); reason != "" {
			slog.Warn("feed warm-up: feed still failing", "feed", f.name, "reason", reason)
			failed = append(failed, f.name+" ("+reason+")")
		} else {
			slog.Info("feed warm-up: ok", "feed", f.name)
		}
	}
	if len(failed) > 0 {
		_ = (&monitor.Telegram{}).Send(ctx, "🟠 FT: the daily feed warm-up found "+strings.Join(failed, "; ")+
			". The feed could not be refreshed (a provider may have changed or blocked FT).")
	}
}

// feedProblem inspects a handler's JSON body: a fear/greed body with a null value or an
// "error", or a news feed with no articles, counts as failing.
func feedProblem(body []byte) string {
	var v map[string]any
	if json.Unmarshal(body, &v) != nil {
		return "unreadable response"
	}
	if e, ok := v["error"].(string); ok && e != "" {
		if len(e) > 80 {
			e = e[:80]
		}
		return e
	}
	if _, has := v["value"]; has && v["value"] == nil {
		return "no value"
	}
	if arts, has := v["articles"]; has {
		if a, _ := arts.([]any); len(a) == 0 {
			if src, _ := v["source"].(string); src == "unconfigured" {
				return "not configured"
			}
			return "no articles"
		}
	}
	return ""
}

// ScheduleWarmup runs WarmFeeds every day at hh:mm UTC until ctx ends.
func (s *Server) ScheduleWarmup(ctx context.Context, hour, minute int) {
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		wctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		s.WarmFeeds(wctx)
		cancel()
	}
}
