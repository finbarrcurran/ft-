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
		Universe:     r.URL.Query().Get("universe"),
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

// POST /api/signals/refresh-insiders — kicks off the ingest in the
// background and returns immediately. Ingest takes 30-60s which exceeds
// reverse-proxy gateway timeouts. Frontend polls /api/signals to see
// new rows appear.
func (s *Server) handleRefreshInsiders(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	if !s.signals.TryStartInsiderIngest() {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"started": false,
			"running": true,
			"message": "ingest already in progress",
		})
		return
	}
	go func() {
		defer s.signals.FinishInsiderIngest()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// v1.21A — run firehose first (latest filings across market),
		// then per-ticker (every Form 4 for our universe). Per-ticker
		// catches NOW + AVGO + everything we explicitly watch.
		if _, err := s.signals.IngestInsiders(ctx); err != nil {
			_ = err
		}
		if _, err := s.signals.IngestInsidersPerTicker(ctx); err != nil {
			_ = err
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"running": true,
		"message": "insider ingest started (firehose + per-ticker) — refresh in 60-90s",
	})
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

// POST /api/signals/upload-oge — JSON upload of an OGE Form 278e filing.
// Body shape: signals.OGEUploadPayload. v1.21C.
//
// Each disclosed position becomes one signal_event row with
// signal_type='oge'. Idempotent — re-uploading the same filer + filing
// date + ticker overwrites rather than duplicates.
func (s *Server) handleUploadOGE(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	var p signals.OGEUploadPayload
	if !decodeJSON(r, w, &p) {
		return
	}
	inserted, err := s.signals.IngestOGE(r.Context(), &p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"inserted":   inserted,
		"positions":  len(p.Positions),
		"filer":      p.Filer,
		"filingDate": p.FilingDate,
	})
}

// POST /api/signals/upload-278t — JSON upload of an OGE Form 278-T periodic
// transaction report (SC-24). Body shape: signals.OGE278TPayload. Each
// transaction becomes one signal_type='oge_278t' row (value band stored as a
// band, never a point value). After ingest we run the EO-coincidence
// cross-link so any trade landing in the same window as an EO touching its
// sector is promoted to ALARM. Idempotent on re-upload.
func (s *Server) handleUpload278T(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	var p signals.OGE278TPayload
	if !decodeJSON(r, w, &p) {
		return
	}
	inserted, flagged, err := s.signals.Ingest278T(r.Context(), &p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	promoted, _ := s.signals.PromoteEOCoincident(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"inserted":        inserted,
		"flaggedUnparsed": flagged,
		"eoCoincidentAlarms": promoted,
		"transactions":    len(p.Transactions),
		"filer":           p.Filer,
		"filingDate":      p.FilingDate,
		"caveat":          signals.Caveat278T,
	})
}

// POST /api/signals/refresh-278t-eo-link — re-run the EO-coincidence join
// over existing 278-T + EO rows (e.g. after a fresh EO ingest). Returns the
// count of newly-promoted ALARMs.
func (s *Server) handleRefresh278TEOLink(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	promoted, err := s.signals.PromoteEOCoincident(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "promoted": promoted})
}

// GET /api/signals/tracked-individuals — the SC-24 named watchlist + the
// mandatory 278-T caveat label.
func (s *Server) handleListTrackedIndividuals(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeJSON(w, http.StatusOK, map[string]any{"individuals": []any{}, "caveat": signals.Caveat278T})
		return
	}
	people, err := s.signals.ListTrackedIndividuals(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"individuals": people,
		"caveat":      signals.Caveat278T,
	})
}

// POST /api/signals/tracked-individuals — add/update a tracked name.
func (s *Server) handleAddTrackedIndividual(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	var body struct {
		Name             string `json:"name"`
		Role             string `json:"role"`
		DisclosureRegime string `json:"disclosureRegime"`
		Notes            string `json:"notes"`
	}
	if !decodeJSON(r, w, &body) {
		return
	}
	if err := s.signals.AddTrackedIndividual(r.Context(), body.Name, body.Role, body.DisclosureRegime, body.Notes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- SC-23 13F Institutional Tracker -------------------------------------

// GET /api/signals/tracked-funds — the fund watchlist + per-fund diff summary
// + the mandatory 13F caveat label.
func (s *Server) handleListTrackedFunds(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeJSON(w, http.StatusOK, map[string]any{"funds": []any{}, "caveat": signals.Caveat13F})
		return
	}
	funds, err := s.signals.ListTrackedFunds(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"funds":  funds,
		"caveat": signals.Caveat13F,
	})
}

// POST /api/signals/tracked-funds — add/reactivate a fund CIK.
func (s *Server) handleAddTrackedFund(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	var body struct {
		CIK   string `json:"cik"`
		Name  string `json:"name"`
		Notes string `json:"notes"`
	}
	if !decodeJSON(r, w, &body) {
		return
	}
	cik, err := s.signals.AddTrackedFund(r.Context(), body.CIK, body.Name, body.Notes)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cik": cik})
}

// POST /api/signals/tracked-funds/remove — soft-deactivate a fund.
func (s *Server) handleRemoveTrackedFund(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	var body struct {
		CIK string `json:"cik"`
	}
	if !decodeJSON(r, w, &body) {
		return
	}
	if err := s.signals.RemoveTrackedFund(r.Context(), body.CIK); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/signals/fund-13f-diffs?cik=... — the latest-period diff detail for
// one fund (AC3 visibility) + the caveat.
func (s *Server) handleListFund13FDiffs(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	cik := r.URL.Query().Get("cik")
	if cik == "" {
		writeError(w, http.StatusBadRequest, "cik query param required")
		return
	}
	diffs, period, err := s.signals.ListFund13FDiffs(r.Context(), cik)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"diffs":  diffs,
		"period": period,
		"caveat": signals.Caveat13F,
	})
}

// POST /api/signals/refresh-13f — background quarterly EDGAR pull for all
// tracked funds (submissions → latest 13F-HR → info-table XML → diff). Returns
// 202; refresh the panel in ~30-60s.
func (s *Server) handleRefresh13F(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, _, err := s.signals.RefreshAllFunds(ctx); err != nil {
			_ = err // RefreshAllFunds already logs per-fund
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"message": "13F EDGAR pull started in background — refresh in 30-60s",
	})
}

// POST /api/signals/refresh-congress — background ingest, returns 202.
func (s *Server) handleRefreshCongress(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := s.signals.IngestCongress(ctx); err != nil {
			_ = err // IngestCongress already logs
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"message": "congress ingest started in background — refresh in 30-60s",
	})
}

// POST /api/signals/refresh-eo — background ingest, returns 202.
func (s *Server) handleRefreshEO(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := s.signals.IngestEOs(ctx); err != nil {
			_ = err
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"message": "EO ingest started in background — refresh in 30-60s",
	})
}

// POST /api/signals/refresh-committees — quarterly legislator + committee refresh.
func (s *Server) handleRefreshCommittees(w http.ResponseWriter, r *http.Request) {
	if s.signals == nil {
		writeError(w, http.StatusNotFound, "signals not initialised")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, _, err := s.signals.IngestLegislators(ctx); err != nil {
			_ = err
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": true,
		"message": "legislator + committee refresh started in background",
	})
}
