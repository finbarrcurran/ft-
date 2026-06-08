// SC-35 Phase 3 — the position_class lever + levels_source toggle.
//
// position_class ('hold'|'trade') is the conviction switch that drives stop
// methodology, take-profit behaviour and alert tone. Flipping it re-defaults
// sl_method (hold→vol_envelope catastrophe stop, trade→technical support−ATR
// stop) unless the caller asks to keep a manual override. levels_source
// ('auto'|'manual') freezes or releases the nightly cron's ownership of the
// numbered S/R columns. Both write a holdings_audit row.

package server

import (
	"errors"
	"ft/internal/store"
	"net/http"
)

// classDefaultSLMethod maps a position_class to the stop-loss method it implies:
// conviction holds get the wide vol-envelope catastrophe stop; trades get the
// tight technical (support_1 − 0.5×ATR_weekly) trade-management stop.
func classDefaultSLMethod(class string) string {
	if class == "trade" {
		return "technical"
	}
	return "vol_envelope"
}

// PUT /api/holdings/stocks/{id}/position-class
// Body: {"positionClass":"trade"[, "keepSlMethod": true]}
//
// Flipping the class re-defaults sl_method unless keepSlMethod=true. The
// frontend sets keepSlMethod after confirming with a user who had deliberately
// overridden the stop method, so a hand-tuned method survives the class change.
func (s *Server) handleUpdateStockPositionClass(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		PositionClass string `json:"positionClass"`
		KeepSLMethod  bool   `json:"keepSlMethod"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.PositionClass != "hold" && req.PositionClass != "trade" {
		writeError(w, http.StatusBadRequest, "positionClass must be 'hold' or 'trade'")
		return
	}

	h, err := s.store.GetStockHolding(r.Context(), userID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}

	oldClass := h.PositionClass
	if oldClass == "" {
		oldClass = "hold"
	}
	oldMethod := "vol_envelope"
	if h.SLMethod != nil && *h.SLMethod != "" {
		oldMethod = *h.SLMethod
	}

	if err := s.store.SetStockPositionClass(r.Context(), userID, id, req.PositionClass); mapStoreError(w, err) {
		return
	}

	changes := map[string]any{
		"positionClass": map[string]string{"from": oldClass, "to": req.PositionClass},
	}

	// Re-default the stop method to the new class's default unless the caller
	// explicitly asked to keep the current (manually overridden) method.
	newMethod := oldMethod
	if !req.KeepSLMethod {
		if d := classDefaultSLMethod(req.PositionClass); d != oldMethod {
			if err := s.store.SetStockSLMethod(r.Context(), userID, id, d); mapStoreError(w, err) {
				return
			}
			newMethod = d
			changes["slMethod"] = map[string]string{"from": oldMethod, "to": d}
		}
	}

	_ = s.store.RecordAudit(r.Context(), userID, "stock", id, h.Ticker, nil, store.AuditUpdate, changes, nil)

	writeJSON(w, http.StatusOK, map[string]any{
		"updated":       id,
		"positionClass": req.PositionClass,
		"slMethod":      newMethod,
	})
}

// PUT /api/holdings/stocks/{id}/levels-source
// Body: {"levelsSource":"manual"}
//
// SC-35 Phase 3 / Decision B — 'manual' freezes support_1/2 + resistance_1/2
// against the nightly cron (the user owns them); 'auto' hands ownership back.
func (s *Server) handleUpdateStockLevelsSource(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req struct {
		LevelsSource string `json:"levelsSource"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.LevelsSource != "auto" && req.LevelsSource != "manual" {
		writeError(w, http.StatusBadRequest, "levelsSource must be 'auto' or 'manual'")
		return
	}

	h, err := s.store.GetStockHolding(r.Context(), userID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	oldSource := h.LevelsSource
	if oldSource == "" {
		oldSource = "auto"
	}

	if err := s.store.SetStockLevelsSource(r.Context(), userID, id, req.LevelsSource); mapStoreError(w, err) {
		return
	}
	if oldSource != req.LevelsSource {
		_ = s.store.RecordAudit(r.Context(), userID, "stock", id, h.Ticker, nil, store.AuditUpdate,
			map[string]any{"levelsSource": map[string]string{"from": oldSource, "to": req.LevelsSource}}, nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": id, "levelsSource": req.LevelsSource})
}
