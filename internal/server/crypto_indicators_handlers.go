// Spec 9e Phase 1 — Crypto Indicators Tab endpoints.
//
//	GET /api/crypto-indicators                  list all 11 indicators
//	GET /api/crypto-indicators/composite/latest live composite + bands
//
// Both cookie-only for v1. Bot-side read access (token auth) can be
// added later if the bot needs to surface the composite in /digest.

package server

import (
	"context"
	"ft/internal/cryptoindicators"
	"net/http"
	"time"
)

// contextWithTimeout derives a request-scoped context with the given
// timeout in seconds. Local helper so the handler stays readable.
func contextWithTimeout(r *http.Request, seconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), time.Duration(seconds)*time.Second)
}

// GET /api/crypto-indicators
func (s *Server) handleListCryptoIndicators(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeJSON(w, http.StatusOK, map[string]any{"indicators": []any{}, "available": false})
		return
	}
	rows, err := s.cryptoIndicators.ListIndicators(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"indicators": rows,
		"available":  true,
		"bucketLabels": map[string]string{
			"cowen":     cryptoindicators.BucketLabel("cowen"),
			"pal":       cryptoindicators.BucketLabel("pal"),
			"universal": cryptoindicators.BucketLabel("universal"),
			"sentiment": cryptoindicators.BucketLabel("sentiment"),
		},
	})
}

// POST /api/crypto-indicators/refresh (v1.8.2)
//
// Manual trigger for the same refresh logic the daily 00:30 UTC cron runs.
// Useful for first-load when the user wants indicators to light up
// immediately rather than wait for the next cron tick.
func (s *Server) handleRefreshCryptoIndicators(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	refresher := cryptoindicators.NewRefresher(s.cryptoIndicators, s.cfg.FREDApiKey)
	// 60s timeout — generous; in practice all calls together take ~5s.
	ctx, cancel := contextWithTimeout(r, 60)
	defer cancel()
	if err := refresher.RefreshAll(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/crypto-indicators/composite/latest
func (s *Server) handleCryptoIndicatorsComposite(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	snap, err := s.cryptoIndicators.LatestComposite(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"composite":   snap,
		"bandLabel":   cryptoindicators.BandLabel(snap.ActionBand),
	})
}
