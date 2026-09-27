package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"ft/internal/etorosync"
)

// SC-42 — eToro ↔ FT direct sync.
//
//	GET  /api/etoro/sync   last run + effective per-ticker SL/TP (shown alongside FT's own)
//	POST /api/etoro/sync   run a sync now (~1–3 s)
//
// No LLM involvement anywhere in this path (internal/etorosync does not import
// internal/llm).

func (s *Server) handleEtoroSyncStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.etoroSync.Current(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// SC-22 demo doctrine: broker positions (units, cost basis, stop levels) are
	// portfolio data — never served in demo mode, enforced server-side.
	if s.demoModeOn(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"configured": st.Configured, "demo": true, "effective": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleEtoroSyncRun(w http.ResponseWriter, r *http.Request) {
	res := s.etoroSync.Sync(r.Context())
	logEtoroSync("manual", res)
	status := http.StatusOK
	if res.Status == "failed" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, res)
}

// RunEtoroSync is the scheduled-job entry point (cmd/ft, every 30 min). Silent
// until the key pair is configured.
func (s *Server) RunEtoroSync(ctx context.Context) {
	if !s.etoroSync.Configured() {
		return
	}
	logEtoroSync("scheduled", s.etoroSync.Sync(ctx))
}

func logEtoroSync(trigger string, r etorosync.Result) {
	if r.Status == "failed" {
		slog.Warn("etoro sync failed", "trigger", trigger, "run", r.RunID, "err", r.Error)
		return
	}
	slog.Info("etoro sync", "trigger", trigger, "run", r.RunID, "status", r.Status, "positions", r.Positions,
		"ownLots", r.OwnLots, "copyLots", r.CopyLots, "lotsNew", r.LotsNew, "lotsClosed", r.LotsClosed, "tickers", r.Tickers,
		"valuesUpdated", r.ValuesUpdated, "proposalsPending", r.ProposalsPending, "proposalsNew", r.ProposalsNew)
	if r.ReconcileError != "" {
		slog.Warn("etoro reconcile failed", "trigger", trigger, "run", r.RunID, "err", r.ReconcileError)
	}
}

// SC-44 — holdings reconcile approval queue.
//
//	GET  /api/etoro/reconcile/proposals?status=pending|dismissed|approved|resolved
//	POST /api/etoro/reconcile/proposals/{id}/approve
//	POST /api/etoro/reconcile/proposals/{id}/dismiss
//
// Value changes on matched holdings apply silently inside the sync; only
// changes to WHICH holdings exist come through here.

func (s *Server) handleEtoroProposals(w http.ResponseWriter, r *http.Request) {
	if s.demoModeOn(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"demo": true, "proposals": []any{}})
		return
	}
	ps, err := s.etoroSync.Proposals(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": ps})
}

func (s *Server) handleEtoroProposalDecision(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.demoModeOn(r.Context()) {
			writeError(w, http.StatusForbidden, "not available in demo mode")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad proposal id")
			return
		}
		var msg string
		if approve {
			msg, err = s.etoroSync.Approve(r.Context(), id)
		} else {
			err = s.etoroSync.Dismiss(r.Context(), id)
			msg = "dismissed"
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "proposal not found")
		case errors.Is(err, etorosync.ErrProposalNotPending):
			writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			writeError(w, http.StatusInternalServerError, err.Error())
		default:
			slog.Info("etoro proposal decided", "id", id, "approve", approve, "result", msg)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": msg})
		}
	}
}
