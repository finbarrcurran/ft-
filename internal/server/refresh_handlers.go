package server

import (
	"net/http"
	"strconv"
	"time"
)

// POST /api/refresh
//
// Triggers a synchronous refresh: FX, stocks, crypto. Returns counts + errors.
// Authentication: session cookie OR bearer token (so the bot can also kick it).
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	result := s.refresh.RefreshAll(r.Context(), userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"startedAt":       result.StartedAt.Format(time.RFC3339),
		"finishedAt":      result.FinishedAt.Format(time.RFC3339),
		"tookMs":          result.FinishedAt.Sub(result.StartedAt).Milliseconds(),
		"stocksAttempted": result.StocksAttempted,
		"stocksUpdated":   result.StocksUpdated,
		"cryptoAttempted": result.CryptoAttempted,
		"cryptoUpdated":   result.CryptoUpdated,
		"fxUpdated":       result.FXUpdated,
		"errors":          result.Errors,
	})
}

// GET /api/refresh-status
//
// Reads the last_refreshed_at + last_partial_failure_at + fx_snapshot from meta.
// Auth: same as /api/refresh.
func (s *Server) handleRefreshStatus(w http.ResponseWriter, r *http.Request) {
	lastRefreshed := metaUnixToString(s, r, "last_refreshed_at")
	lastFailure := metaUnixToString(s, r, "last_partial_failure_at")
	fxStr, _ := s.store.GetMeta(r.Context(), "fx_snapshot_eur_usd")
	writeJSON(w, http.StatusOK, map[string]any{
		"lastRefreshedAt":      lastRefreshed,
		"lastPartialFailureAt": lastFailure,
		"fxSnapshotEURUSD":     fxStr,
	})
}

func metaUnixToString(s *Server, r *http.Request, key string) string {
	v, err := s.store.GetMeta(r.Context(), key)
	if err != nil || v == "" {
		return ""
	}
	ts, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}
