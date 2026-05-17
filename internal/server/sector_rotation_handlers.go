// Spec 9f §5 — Sector Rotation API.
//
//   GET    /api/sector-rotation/metrics     — full payload for the tab
//   GET    /api/sector-rotation/sectors     — universe for dropdowns
//   POST   /api/sector-rotation/ordering    — save manual reorder
//   DELETE /api/sector-rotation/ordering    — reset to auto-ranking
//   POST   /api/sector-rotation/refresh     — manual ingest trigger
//   GET    /api/sector-rotation/digests     — recent weekly digests
//
// Cookie OR bearer token, per existing FT convention.

package server

import (
	"ft/internal/sector_rotation"
	"net/http"
	"strconv"
	"time"
)

// GET /api/sector-rotation/metrics
func (s *Server) handleSectorMetrics(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	rows, err := sector_rotation.ComputeAll(r.Context(), s.store, userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"asOf":    time.Now().UTC().Format(time.RFC3339),
		"sectors": rows,
	})
}

// GET /api/sector-rotation/sectors — flat universe list (for dropdowns).
func (s *Server) handleSectorUniverse(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListSectorUniverse(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sectors": rows})
}

// POST /api/sector-rotation/ordering
// Body: [{id: 5, position: 1}, {id: 12, position: 2}, ...]
func (s *Server) handleSectorOrdering(w http.ResponseWriter, r *http.Request) {
	var req []struct {
		ID       int64 `json:"id"`
		Position int   `json:"position"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	pairs := make([]struct {
		SectorID int64
		Position int
	}, 0, len(req))
	for _, p := range req {
		pairs = append(pairs, struct {
			SectorID int64
			Position int
		}{p.ID, p.Position})
	}
	if err := s.store.SetUserSectorOrdering(r.Context(), pairs); mapStoreError(w, err) {
		return
	}
	sector_rotation.BustCache() // ordering reads from store; safe to clear
	writeJSON(w, http.StatusOK, map[string]any{"saved": len(pairs), "savedAt": time.Now().UTC().Format(time.RFC3339)})
}

// DELETE /api/sector-rotation/ordering
func (s *Server) handleSectorOrderingReset(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearUserSectorOrdering(r.Context()); mapStoreError(w, err) {
		return
	}
	sector_rotation.BustCache()
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}

// POST /api/sector-rotation/refresh — manual ingest trigger.
func (s *Server) handleSectorRefresh(w http.ResponseWriter, r *http.Request) {
	res := sector_rotation.IngestDaily(r.Context(), s.store, time.Now().UTC())
	sector_rotation.BustCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"sectorsTried": res.SectorsTried,
		"sectorsOk":    res.SectorsOK,
		"errors":       res.Errors,
		"tookMs":       res.FinishedAt.Sub(res.StartedAt).Milliseconds(),
	})
}

// GET /api/sector-rotation/digests?limit=8
func (s *Server) handleSectorDigests(w http.ResponseWriter, r *http.Request) {
	limit := 8
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	rows, err := s.store.ListSectorRotationDigests(r.Context(), limit)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digests": rows})
}

// PUT /api/holdings/stocks/{id}/sector
// Body: {"sectorUniverseId": 5} or {"sectorUniverseId": null} to clear
//
// Convenience endpoint that bypasses the full UpdateStockHolding flow for
// the common single-tag retag case (mapping doc says user will re-tag
// individual rows after migration).
func (s *Server) handleUpdateStockSector(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		SectorUniverseID *int64 `json:"sectorUniverseId"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if err := s.store.UpdateStockHoldingSector(r.Context(), userID, id, req.SectorUniverseID); mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": id})
}
