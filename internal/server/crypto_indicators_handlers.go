// Spec 9e Phase 1 — Crypto Indicators Tab endpoints.
//
//	GET /api/crypto-indicators                  list all 11 indicators
//	GET /api/crypto-indicators/composite/latest live composite + bands
//
// Both cookie-only for v1. Bot-side read access (token auth) can be
// added later if the bot needs to surface the composite in /digest.

package server

import (
	"ft/internal/cryptoindicators"
	"net/http"
)

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
