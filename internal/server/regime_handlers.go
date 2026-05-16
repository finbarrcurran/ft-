// Spec 9b D2 — regime API.
//
// Routes (cookie OR token auth so the bot's /regime command works):
//
//	GET  /api/regime                  consolidated snapshot
//	POST /api/regime/jordi            {regime, note}; manual set
//	POST /api/regime/cowen/manual     {regime, note}; manual set
//	POST /api/regime/cowen/auto       8-field Cowen form → auto-classify
//	GET  /api/regime/history?framework=&limit=

package server

import (
	"context"
	"encoding/json"
	"errors"
	"ft/internal/regime"
	"net/http"
	"strconv"
	"time"
)

// ----- shared shapes ----------------------------------------------------

type regimeSide struct {
	Regime     string          `json:"regime"`
	SetAt      *time.Time      `json:"set_at,omitempty"`
	Stale      bool            `json:"stale"`
	Source     string          `json:"source"`
	LastInputs json.RawMessage `json:"last_inputs,omitempty"`
}

const staleDays = 14

// loadRegimeSide pulls current state for one framework from user_preferences.
func (s *Server) loadRegimeSide(r *http.Request, framework string) regimeSide {
	keyRegime := "regime_" + framework
	keySetAt := "regime_" + framework + "_set_at"
	keyLastInputs := "regime_" + framework + "_last_inputs"
	keySource := "regime_" + framework + "_source"

	out := regimeSide{Regime: string(regime.Unclassified), Source: "manual"}

	if v, err := s.store.GetPreference(r.Context(), keyRegime); err == nil {
		out.Regime = v
	}
	if v, err := s.store.GetPreference(r.Context(), keySource); err == nil {
		out.Source = v
	}
	if v, err := s.store.GetPreference(r.Context(), keySetAt); err == nil {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			t := time.Unix(ts, 0).UTC()
			out.SetAt = &t
			if time.Since(t) > staleDays*24*time.Hour {
				out.Stale = true
			}
		}
	}
	if framework == "cowen" {
		if v, err := s.store.GetPreference(r.Context(), keyLastInputs); err == nil && v != "" {
			out.LastInputs = json.RawMessage(v)
		}
	}
	return out
}

// GET /api/regime
func (s *Server) handleGetRegime(w http.ResponseWriter, r *http.Request) {
	j := s.loadRegimeSide(r, "jordi")
	c := s.loadRegimeSide(r, "cowen")
	eff := regime.Effective(regime.Regime(j.Regime), regime.Regime(c.Regime))
	mult := regime.AlertMarginMultiplier(eff)
	writeJSON(w, http.StatusOK, map[string]any{
		"jordi":                    j,
		"cowen":                    c,
		"effective":                string(eff),
		"alert_margin_multiplier":  mult,
		"watchlist_alerts_active":  regime.GatesWatchlistEntryZone(eff),
	})
}

// ----- manual set helpers ----------------------------------------------

type manualSetReq struct {
	Regime string `json:"regime"`
	Note   string `json:"note"`
}

// applyManualSet writes the user_preferences entries + a history row.
// Returns (regime applied, error or nil).
func (s *Server) applyManualSet(r *http.Request, framework string, req manualSetReq) (string, error) {
	if !regime.Valid(req.Regime) {
		return "", errBadRegime
	}
	now := time.Now().UTC()
	// Prior regime → captured into inputs_json for history diff context.
	prior, _ := s.store.GetPreference(r.Context(), "regime_"+framework)
	inputsJSON := ""
	if prior != "" {
		inputsJSON = `{"prior":"` + prior + `"}`
	}
	if err := s.store.SetPreference(r.Context(), "regime_"+framework, req.Regime); err != nil {
		return "", err
	}
	if err := s.store.SetPreference(r.Context(), "regime_"+framework+"_set_at",
		strconv.FormatInt(now.Unix(), 10)); err != nil {
		return "", err
	}
	if err := s.store.SetPreference(r.Context(), "regime_"+framework+"_source", "manual"); err != nil {
		return "", err
	}
	if _, err := s.store.RecordRegimeChange(r.Context(), framework, req.Regime,
		"manual", inputsJSON, req.Note); err != nil {
		return "", err
	}
	return req.Regime, nil
}

// POST /api/regime/jordi
func (s *Server) handleSetJordiRegime(w http.ResponseWriter, r *http.Request) {
	var req manualSetReq
	if !decodeJSON(r, w, &req) {
		return
	}
	applied, err := s.applyManualSet(r, "jordi", req)
	if errors.Is(err, errBadRegime) {
		writeError(w, http.StatusBadRequest, "regime must be stable|shifting|defensive|unclassified")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"regime": applied})
}

// POST /api/regime/cowen/manual
func (s *Server) handleSetCowenManual(w http.ResponseWriter, r *http.Request) {
	var req manualSetReq
	if !decodeJSON(r, w, &req) {
		return
	}
	applied, err := s.applyManualSet(r, "cowen", req)
	if errors.Is(err, errBadRegime) {
		writeError(w, http.StatusBadRequest, "regime must be stable|shifting|defensive|unclassified")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"regime": applied})
}

// POST /api/regime/cowen/auto — 8-field Cowen weekly capture.
func (s *Server) handleSetCowenAuto(w http.ResponseWriter, r *http.Request) {
	var inputs regime.CowenFormInputs
	if !decodeJSON(r, w, &inputs) {
		return
	}
	// Validation: cycle phase 1-4, risk indicator 0-1, all radios populated.
	if inputs.CyclePhase < 1 || inputs.CyclePhase > 4 {
		writeError(w, http.StatusBadRequest, "cycle_phase must be 1..4")
		return
	}
	if inputs.RiskIndicator < 0 || inputs.RiskIndicator > 1 {
		writeError(w, http.StatusBadRequest, "risk_indicator must be 0.0..1.0")
		return
	}

	prior, _ := s.store.MostRecentCyclePhase(r.Context())
	classified, reason := regime.ClassifyCowen(inputs, prior)

	// Persist current state + history row.
	now := time.Now().UTC()
	inputsJSON, _ := json.Marshal(inputs)
	if err := s.store.SetPreference(r.Context(), "regime_cowen", string(classified)); err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.SetPreference(r.Context(), "regime_cowen_set_at",
		strconv.FormatInt(now.Unix(), 10)); err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.SetPreference(r.Context(), "regime_cowen_source", "auto_cowen_form"); err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.SetPreference(r.Context(), "regime_cowen_last_inputs", string(inputsJSON)); err != nil {
		mapStoreError(w, err)
		return
	}
	if _, err := s.store.RecordRegimeChange(r.Context(), "cowen",
		string(classified), "auto_cowen_form", string(inputsJSON), inputs.Note); err != nil {
		mapStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"regime": string(classified),
		"reason": reason,
		"inputs": inputs,
	})
}

// GET /api/regime/history?framework=jordi&limit=50
func (s *Server) handleListRegimeHistory(w http.ResponseWriter, r *http.Request) {
	framework := r.URL.Query().Get("framework")
	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	rows, err := s.store.ListRegimeHistory(r.Context(), framework, limit)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": rows})
}

// ----- sentinel errors -------------------------------------------------

var errBadRegime = errors.New("bad regime value")

// currentEffectiveRegime is the helper alert.go and bot endpoints use to
// figure out which margin / suppression rules apply. Regime changes are
// rare so we don't memoise; two GetPreference calls is ~no overhead.
func (s *Server) currentEffectiveRegime(ctx context.Context) regime.Regime {
	jr, _ := s.store.GetPreference(ctx, "regime_jordi")
	cr, _ := s.store.GetPreference(ctx, "regime_cowen")
	if jr == "" {
		jr = string(regime.Unclassified)
	}
	if cr == "" {
		cr = string(regime.Unclassified)
	}
	return regime.Effective(regime.Regime(jr), regime.Regime(cr))
}
