// Spec 9k — Political & Insider Signal endpoints.
//
// v1.10.0 (Phase A): insider Form 4 ingest + read endpoints + ack.
// v1.11.0 (Phase B) will add Congress + EO ingest paths.

package server

import (
	"context"
	"ft/internal/signals"
	"net/http"
	"strconv"
	"time"
)

// GET /api/signals?tier=&type=&range=&include_acked=
func (s *Server) handleListSignals(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeJSON(w, http.StatusOK, map[string]any{"signals": []any{}, "counts": map[string]int{}})
		return
	}
	rangeDays, _ := strconv.Atoi(r.URL.Query().Get("range"))
	if rangeDays == 0 {
		rangeDays = 30 // default window
	}
	f := signals.ListFilter{
		Tier:         r.URL.Query().Get("tier"),
		Type:         r.URL.Query().Get("type"),
		RangeDays:    rangeDays,
		IncludeAcked: r.URL.Query().Get("include_acked") == "1",
	}
	rows, err := s.signals.List(r.Context(), f)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	counts, _ := s.signals.Counts(r.Context(), rangeDays)
	writeJSON(w, http.StatusOK, map[string]any{
		"signals":   rows,
		"counts":    counts,
		"rangeDays": rangeDays,
	})
}

// POST /api/signals/{id}/ack
func (s *Server) handleAckSignal(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.signals.Acknowledge(r.Context(), id); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/signals/refresh-insiders — manual trigger for the daily ingest.
func (s *Server) handleRefreshInsiders(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	inserted, err := s.signals.IngestInsiders(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"inserted": inserted})
}

// GET /api/signals/universe — debug snapshot of current universe.
func (s *Server) handleSignalsUniverse(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	snap := s.signals.Snapshot(r.Context())
	writeJSON(w, http.StatusOK, snap)
}
