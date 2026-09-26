package server

import (
	"context"
	"log/slog"
	"net/http"

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
		"ownLots", r.OwnLots, "copyLots", r.CopyLots, "lotsNew", r.LotsNew, "lotsClosed", r.LotsClosed, "tickers", r.Tickers)
}
