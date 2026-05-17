// Package server is the HTTP surface: routes, handlers, middleware.
//
// Phase 8 skeleton: only /healthz is wired up. Auth/holdings/bot handlers
// will be added as the port progresses.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/config"
	"ft/internal/domain"
	"ft/internal/refresh"
	"ft/internal/store"
	"ft/internal/web"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "ft_session"

type Server struct {
	cfg     *config.Config
	store   *store.Store
	refresh *refresh.Service
	mux     *http.ServeMux
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:     cfg,
		store:   st,
		refresh: refresh.New(st),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("panic in handler", "err", rec, "path", r.URL.Path)
			writeError(w, http.StatusInternalServerError, "internal error")
		}
	}()
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Health
	s.mux.HandleFunc("GET /healthz", s.healthz)

	// Auth — public (no cookie required).
	s.mux.HandleFunc("GET /api/auth/state", s.handleAuthState)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)

	// Auth — requires session cookie.
	s.mux.HandleFunc("POST /api/auth/logout", s.requireUser(s.handleLogout))
	s.mux.HandleFunc("GET /api/auth/me", s.requireUser(s.handleMe))

	// Holdings — requires session cookie.
	s.mux.HandleFunc("GET /api/holdings/stocks", s.requireUser(s.handleListStocks))
	s.mux.HandleFunc("GET /api/holdings/crypto", s.requireUser(s.handleListCrypto))

	// Holdings CRUD (Spec 3) — every mutation writes one audit row.
	s.mux.HandleFunc("POST /api/holdings/stocks", s.requireUser(s.handleCreateStock))
	s.mux.HandleFunc("PUT /api/holdings/stocks/{id}", s.requireUser(s.handleUpdateStock))
	s.mux.HandleFunc("DELETE /api/holdings/stocks/{id}", s.requireUser(s.handleSoftDeleteStock))
	s.mux.HandleFunc("POST /api/holdings/stocks/{id}/restore", s.requireUser(s.handleRestoreStock))
	s.mux.HandleFunc("GET /api/holdings/stocks/deleted", s.requireUser(s.handleListDeletedStocks))
	s.mux.HandleFunc("POST /api/holdings/crypto", s.requireUser(s.handleCreateCrypto))
	s.mux.HandleFunc("PUT /api/holdings/crypto/{id}", s.requireUser(s.handleUpdateCrypto))
	s.mux.HandleFunc("DELETE /api/holdings/crypto/{id}", s.requireUser(s.handleSoftDeleteCrypto))
	s.mux.HandleFunc("POST /api/holdings/crypto/{id}/restore", s.requireUser(s.handleRestoreCrypto))
	s.mux.HandleFunc("GET /api/holdings/crypto/deleted", s.requireUser(s.handleListDeletedCrypto))
	s.mux.HandleFunc("GET /api/audit", s.requireUser(s.handleListAudit))

	// Summary — aggregate KPIs + donut SVGs.
	s.mux.HandleFunc("GET /api/summary", s.requireUser(s.handleSummary))

	// Market status — /api/marketstatus is the back-compat shape (US only);
	// /api/marketstatus/all is the multi-exchange Spec 5 endpoint.
	s.mux.HandleFunc("GET /api/marketstatus", s.requireUser(s.handleMarketStatus))
	s.mux.HandleFunc("GET /api/marketstatus/all", s.requireUser(s.handleMarketStatusAll))

	// Refresh — accepts cookie OR bearer token (humans + bot both call this).
	s.mux.HandleFunc("POST /api/refresh", s.requireUserOrToken(s.handleRefresh))
	s.mux.HandleFunc("GET /api/refresh-status", s.requireUserOrToken(s.handleRefreshStatus))

	// Import / export — requires session cookie.
	s.mux.HandleFunc("POST /api/import/preview", s.requireUser(s.handleImportPreview))
	s.mux.HandleFunc("POST /api/import/apply", s.requireUser(s.handleImportApply))
	s.mux.HandleFunc("GET /api/export.xlsx", s.requireUser(s.handleExport))

	// Heatmap — requires session cookie. Returns an SVG document.
	s.mux.HandleFunc("GET /api/heatmap.svg", s.requireUser(s.handleHeatmap))

	// News + Fear&Greed — requires session cookie.
	s.mux.HandleFunc("GET /api/news/market", s.requireUser(s.handleMarketNews))
	s.mux.HandleFunc("GET /api/news/crypto", s.requireUser(s.handleCryptoNews))
	s.mux.HandleFunc("GET /api/feargreed", s.requireUser(s.handleFearGreed))
	s.mux.HandleFunc("GET /api/feargreed/stocks", s.requireUser(s.handleFearGreedStocks))

	// Spec 9b: regime overlay. Cookie OR token auth so the bot's /regime
	// command can read state without holding a session.
	s.mux.HandleFunc("GET /api/regime", s.requireUserOrToken(s.handleGetRegime))
	s.mux.HandleFunc("POST /api/regime/jordi", s.requireUser(s.handleSetJordiRegime))
	s.mux.HandleFunc("POST /api/regime/cowen/manual", s.requireUser(s.handleSetCowenManual))
	s.mux.HandleFunc("POST /api/regime/cowen/auto", s.requireUser(s.handleSetCowenAuto))
	s.mux.HandleFunc("GET /api/regime/history", s.requireUser(s.handleListRegimeHistory))

	// Spec 9b D9: screener — S&P sample with live overlay + filters.
	s.mux.HandleFunc("GET /api/screener", s.requireUser(s.handleScreener))

	// Spec 9c: per-holding levels (S/R candidates + suggested SL/TP/R-mult).
	// Token-or-cookie so the bot's /levels command works.
	s.mux.HandleFunc("GET /api/holdings/stocks/{id}/levels", s.requireUserOrToken(s.handleStockLevels))
	s.mux.HandleFunc("GET /api/holdings/crypto/{id}/levels", s.requireUserOrToken(s.handleCryptoLevels))
	// Spec 9c D9: Percoco auto-scoring (6/8 questions).
	s.mux.HandleFunc("POST /api/holdings/stocks/{id}/autoscore", s.requireUser(s.handleStockAutoscore))
	s.mux.HandleFunc("POST /api/holdings/crypto/{id}/autoscore", s.requireUser(s.handleCryptoAutoscore))
	// Spec 9c D12/D13/D16: portfolio risk dashboard + daily snapshot.
	s.mux.HandleFunc("GET /api/risk/dashboard", s.requireUserOrToken(s.handleRiskDashboard))
	s.mux.HandleFunc("POST /api/risk/snapshot", s.requireUserOrToken(s.handleRiskSnapshot))

	// Spec 9b D11: macro economics calendar (embedded JSON).
	s.mux.HandleFunc("GET /api/macro", s.requireUser(s.handleMacro))

	// Spec 6: per-user preferences (key/value). Token-or-cookie so the
	// FT bot can read/write the Sunday-nudge skip flag (Spec 9b D12).
	s.mux.HandleFunc("GET /api/preferences", s.requireUserOrToken(s.handleListPreferences))
	s.mux.HandleFunc("GET /api/preferences/{key}", s.requireUserOrToken(s.handleGetPreference))
	s.mux.HandleFunc("PUT /api/preferences/{key}", s.requireUserOrToken(s.handleSetPreference))

	// Spec 4: Watchlist + Framework Scoring — all require cookie auth.
	s.mux.HandleFunc("GET /api/watchlist", s.requireUser(s.handleListWatchlist))
	s.mux.HandleFunc("POST /api/watchlist", s.requireUser(s.handleCreateWatchlist))
	s.mux.HandleFunc("PUT /api/watchlist/{id}", s.requireUser(s.handleUpdateWatchlist))
	s.mux.HandleFunc("DELETE /api/watchlist/{id}", s.requireUser(s.handleSoftDeleteWatchlist))
	s.mux.HandleFunc("POST /api/watchlist/{id}/promote", s.requireUser(s.handlePromoteWatchlist))
	s.mux.HandleFunc("GET /api/frameworks", s.requireUser(s.handleListFrameworks))
	s.mux.HandleFunc("GET /api/frameworks/{id}", s.requireUser(s.handleGetFramework))
	s.mux.HandleFunc("GET /api/scores", s.requireUser(s.handleGetScores))
	s.mux.HandleFunc("POST /api/scores", s.requireUser(s.handleCreateScore))

	// Bot-facing endpoints — cookie OR bearer token. Designed for the OpenClaw
	// skill but curl-friendly for humans too.
	s.mux.HandleFunc("GET /api/bot/alerts", s.requireUserOrToken(s.handleBotAlerts))
	s.mux.HandleFunc("POST /api/bot/alerts/ack", s.requireUserOrToken(s.handleBotAlertsAck))
	s.mux.HandleFunc("GET /api/bot/holdings/summary", s.requireUserOrToken(s.handleBotSummary))
	s.mux.HandleFunc("GET /api/bot/holdings/movers", s.requireUserOrToken(s.handleBotMovers))
	// Refresh aliases under /api/bot/* so the skill's URLs are consistent.
	s.mux.HandleFunc("POST /api/bot/refresh", s.requireUserOrToken(s.handleRefresh))
	s.mux.HandleFunc("GET /api/bot/refresh-status", s.requireUserOrToken(s.handleRefreshStatus))

	// Static frontend (catch-all; must be registered last).
	s.mux.Handle("/", web.Handler())
}

// ----- request context ----------------------------------------------------

type ctxKey int

const (
	ctxUserID ctxKey = iota + 1
	ctxSessionToken
)

func userIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(ctxUserID).(int64)
	return v, ok
}

// ----- JSON helpers -------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON limits the body and returns 400 on parse errors.
func decodeJSON(r *http.Request, w http.ResponseWriter, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %s", err.Error()))
		return false
	}
	return true
}

func pathID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	if raw == "" {
		return 0, errors.New("missing id")
	}
	return strconv.ParseInt(raw, 10, 64)
}

func mapStoreError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return true
	}
	slog.Error("store error", "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
	return true
}

// ----- health -------------------------------------------------------------

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"time": time.Now().UTC().Format(time.RFC3339),
	})
}

// ----- shared types used by handlers --------------------------------------

type userResp struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName,omitempty"`
}

func userToResp(u *domain.User) userResp {
	return userResp{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName}
}

// ----- shared helpers used by handlers ------------------------------------

func stringNotBlank(s string) bool { return strings.TrimSpace(s) != "" }

// clientIP best-effort extracts the originating client IP. Honours Cloudflare's
// CF-Connecting-IP first, then X-Forwarded-For, then falls back to RemoteAddr.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("CF-Connecting-IP"); fwd != "" {
		return fwd
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if i := strings.Index(fwd, ","); i >= 0 {
			return strings.TrimSpace(fwd[:i])
		}
		return strings.TrimSpace(fwd)
	}
	return r.RemoteAddr
}
