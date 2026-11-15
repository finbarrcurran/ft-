package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ft/internal/videolinks"
)

// GET /api/holdings/{kind}/{id}/commentary?days=90&limit=10 — FT-146 PR B. What Jordi and Cowen said about this
// holding, from the deterministic links (video_links). Context only (D6): nothing reads it for scoring, alerts or
// risk, and it never sets the regime (D7). It stays off for production use until the link audit reaches 98% precision:
// the preference video_links_panel must be "on", otherwise the answer is {"enabled": false} and nothing is read.
func (s *Server) handleHoldingCommentary(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validHoldingKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be stock|crypto")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if on, _ := s.store.GetPreference(r.Context(), "video_links_panel"); on != "on" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	var ticker string
	q := `SELECT ticker FROM stock_holdings WHERE id = ? AND deleted_at IS NULL`
	if kind == "crypto" {
		q = `SELECT symbol FROM crypto_holdings WHERE id = ? AND deleted_at IS NULL`
	}
	if err := s.store.DB.QueryRowContext(r.Context(), q, id).Scan(&ticker); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "commentary": nil})
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	c, err := videolinks.ForTicker(r.Context(), s.store.DB, ticker, days, limit, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "commentary failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "commentary": c})
}

// commentaryChipsEnabled is the gate for the watchlist/universe chips and drawer (FT-149): the commentary panel switch
// AND the chips switch (video_links_chips, default off) must be on, and demo mode never shows them (the Nexus tab is
// universe-only in demo and the watchlist is portfolio-linked).
func (s *Server) commentaryChipsEnabled(ctx context.Context) bool {
	if s.demoModeOn(ctx) {
		return false
	}
	panel, _ := s.store.GetPreference(ctx, "video_links_panel")
	chips, _ := s.store.GetPreference(ctx, "video_links_chips")
	return panel == "on" && chips == "on"
}

// GET /api/commentary?ticker=NVDA&days=90&limit=10 — what Jordi and Cowen said about any ticker FT holds, watches or
// tracks (the drawer opened from a chip). Same payload as the per-holding panel. Context only (D6).
func (s *Server) handleCommentaryByTicker(w http.ResponseWriter, r *http.Request) {
	if !s.commentaryChipsEnabled(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	ticker := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("ticker")))
	if ticker == "" {
		writeError(w, http.StatusBadRequest, "ticker is required")
		return
	}
	if !videolinks.KnownTicker(r.Context(), s.store.DB, ticker) {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "commentary": nil})
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	c, err := videolinks.ForTicker(r.Context(), s.store.DB, ticker, days, limit, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "commentary failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "commentary": c})
}

// GET /api/commentary/summary?set=watchlist|universe — every chip of a table in one call: ticker -> commentator ->
// when they last mentioned it. Only tickers with commentary appear.
func (s *Server) handleCommentarySummary(w http.ResponseWriter, r *http.Request) {
	if !s.commentaryChipsEnabled(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	set := r.URL.Query().Get("set")
	var q string
	switch set {
	case "watchlist":
		q = `SELECT UPPER(ticker) FROM watchlist WHERE deleted_at IS NULL`
	case "universe":
		q = `SELECT UPPER(ticker) FROM nexus_universe WHERE deleted_at IS NULL AND active = 1`
	default:
		writeError(w, http.StatusBadRequest, "set must be watchlist|universe")
		return
	}
	rows, err := s.store.DB.QueryContext(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summary failed")
		return
	}
	var tickers []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tickers = append(tickers, t)
		}
	}
	rows.Close()
	sum, err := videolinks.Summary(r.Context(), s.store.DB, tickers, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summary failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "set": set, "tickers": sum})
}
