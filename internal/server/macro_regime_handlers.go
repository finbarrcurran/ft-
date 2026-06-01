// Spec 9p — Macro Regime & Sector Rotation endpoints.
//
//	GET    /api/macro/regime        latest regime + indicators(+sparkline) + playbook + ISM + suggested Jordi
//	POST   /api/macro/refresh       manual FRED re-fetch + reclassify
//	POST   /api/macro/ism           set manual ISM headline override
//	GET    /api/macro/playbook?regime=Qx   doctrine rows
//	POST   /api/macro/playbook      upsert one doctrine row
//	DELETE /api/macro/playbook/{id} delete one doctrine row
//
// The regime read is deterministic (classifier in internal/macroregime).
// Divergence + the suggest-a-Jordi affordance are computed client-side from
// this payload joined with /api/sector-rotation/metrics.
package server

import (
	"ft/internal/macroregime"
	"net/http"
	"strconv"
	"time"
)

// GET /api/macro/regime?historyDays=N
func (s *Server) handleMacroRegime(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	days := 90
	if d := r.URL.Query().Get("historyDays"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 800 {
			days = n
		}
	}
	ctx := r.Context()
	indicators, err := s.macroRegime.ListIndicatorsWithHistory(ctx, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	regime, haveRegime, err := s.macroRegime.LatestRegime(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ism, _ := s.macroRegime.LatestISM(ctx)
	var playbook []macroregime.PlaybookRow
	if haveRegime && regime.Quadrant != "" && regime.Quadrant != "unclassified" {
		playbook, _ = s.macroRegime.ListPlaybook(ctx, regime.Quadrant)
	}
	resp := map[string]any{
		"indicators":  indicators,
		"ism":         ism,
		"playbook":    playbook,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"hasRegime":   haveRegime,
	}
	if haveRegime {
		resp["regime"] = regime
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/macro/refresh — manual FRED re-fetch + reclassify.
func (s *Server) handleMacroRefresh(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	ctx, cancel := contextWithTimeout(r, 90)
	defer cancel()
	refresher := macroregime.NewRefresher(s.macroRegime, s.cfg.FREDApiKey)
	if err := refresher.RefreshAll(ctx); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	regime, _, _ := s.macroRegime.LatestRegime(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "regime": regime})
}

// POST /api/macro/ism  { "value": 48.7 }
func (s *Server) handleMacroSetISM(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	var body struct {
		Value float64 `json:"value"`
	}
	if !decodeJSON(r, w, &body) {
		return
	}
	if body.Value < 0 || body.Value > 100 {
		writeError(w, http.StatusBadRequest, "ISM value must be 0–100")
		return
	}
	ctx := r.Context()
	if err := s.macroRegime.SetISMManual(ctx, body.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Reclassify immediately so the override takes effect now.
	if byID, err := s.macroRegime.ListIndicators(ctx); err == nil {
		ism, _ := s.macroRegime.LatestISM(ctx)
		st := macroregime.Classify(byID, ism)
		_, _ = s.macroRegime.WriteRegime(ctx, st)
	}
	ism, _ := s.macroRegime.LatestISM(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ism": ism})
}

// GET /api/macro/playbook?regime=Qx
func (s *Server) handleMacroListPlaybook(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	rows, err := s.macroRegime.ListPlaybook(r.Context(), r.URL.Query().Get("regime"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"playbook": rows})
}

// POST /api/macro/playbook — upsert one doctrine row (id==0 inserts).
func (s *Server) handleMacroUpsertPlaybook(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	var p macroregime.PlaybookRow
	if !decodeJSON(r, w, &p) {
		return
	}
	switch p.RegimeKey {
	case "Q1", "Q2", "Q3", "Q4":
	default:
		writeError(w, http.StatusBadRequest, "regimeKey must be Q1–Q4")
		return
	}
	switch p.Stance {
	case "favored", "neutral", "avoid":
	default:
		writeError(w, http.StatusBadRequest, "stance must be favored|neutral|avoid")
		return
	}
	if p.AssetOrSector == "" {
		writeError(w, http.StatusBadRequest, "assetOrSector required")
		return
	}
	if p.Source == "" {
		p.Source = "user"
	}
	id, err := s.macroRegime.UpsertPlaybookRow(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// DELETE /api/macro/playbook/{id}
func (s *Server) handleMacroDeletePlaybook(w http.ResponseWriter, r *http.Request) {
	if s.macroRegime == nil {
		writeError(w, http.StatusServiceUnavailable, "macro regime not available")
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.macroRegime.DeletePlaybookRow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
