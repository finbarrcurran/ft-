package server

import (
	"net/http"
	"strconv"
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
