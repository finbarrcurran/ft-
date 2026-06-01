// SC-17 Phase 1 — eToro statement importer (performance history).
//
// Endpoints (token-or-cookie via requireUser):
//   POST /api/etoro/import/preview   multipart .xlsx → parsed Statement (staged)
//   POST /api/etoro/import/apply     apply the staged statement (supersede/insert)
//   GET  /api/etoro/performance      live annual + YTD performance history
//
// Propose-and-apply: preview parses + stages in memory; apply writes rows.
// Re-uploading a statement for a year supersedes that year's prior rows.

package server

import (
	"ft/internal/etoro"
	"ft/internal/store"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pendingEtoro holds a parsed-but-not-yet-applied statement for one user.
type pendingEtoro struct {
	Statement *etoro.Statement
	Stored    time.Time
}

var (
	pendingEtoros   = map[int64]*pendingEtoro{}
	pendingEtorosMu sync.Mutex
	pendingEtoroTTL = 10 * time.Minute
)

func storePendingEtoro(userID int64, p *pendingEtoro) {
	pendingEtorosMu.Lock()
	defer pendingEtorosMu.Unlock()
	cutoff := time.Now().Add(-pendingEtoroTTL)
	for k, v := range pendingEtoros {
		if v.Stored.Before(cutoff) {
			delete(pendingEtoros, k)
		}
	}
	pendingEtoros[userID] = p
}

func popPendingEtoro(userID int64) *pendingEtoro {
	pendingEtorosMu.Lock()
	defer pendingEtorosMu.Unlock()
	p, ok := pendingEtoros[userID]
	if !ok {
		return nil
	}
	delete(pendingEtoros, userID)
	if time.Since(p.Stored) > pendingEtoroTTL {
		return nil
	}
	return p
}

// POST /api/etoro/import/preview — multipart upload of an eToro .xlsx.
func (s *Server) handleEtoroImportPreview(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MiB cap
		writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' form field")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") {
		writeError(w, http.StatusBadRequest,
			"file must be an eToro .xlsx statement (got "+header.Filename+")")
		return
	}

	st, err := etoro.Parse(file, header.Filename, time.Now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "parse: "+err.Error())
		return
	}

	storePendingEtoro(userID, &pendingEtoro{Statement: st, Stored: time.Now()})

	writeJSON(w, http.StatusOK, map[string]any{
		"fileName":   st.FileName,
		"years":      st.Years,
		"warnings":   st.Warnings,
		"ttlSeconds": int(pendingEtoroTTL.Seconds()),
	})
}

// POST /api/etoro/import/apply — applies the staged statement.
func (s *Server) handleEtoroImportApply(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	pending := popPendingEtoro(userID)
	if pending == nil || pending.Statement == nil {
		writeError(w, http.StatusBadRequest, "no pending statement (expired or never previewed)")
		return
	}

	applied := 0
	for _, y := range pending.Statement.Years {
		row := etoroYearToStore(y)
		if err := s.store.UpsertEtoroPerformanceYear(r.Context(), userID, row, pending.Statement.FileName); err != nil {
			mapStoreError(w, err)
			return
		}
		applied++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"yearsApplied": applied,
		"fileName":     pending.Statement.FileName,
	})
}

// GET /api/etoro/performance — live annual + YTD history.
func (s *Server) handleEtoroPerformance(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	years, err := s.store.ListEtoroPerformance(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years})
}

// etoroYearToStore maps the etoro package's compute result onto the store row
// type (keeps the store package free of an excelize dependency).
func etoroYearToStore(y etoro.YearPerf) store.EtoroYearRow {
	row := store.EtoroYearRow{
		Year:           y.Year,
		RangeStart:     y.RangeStart,
		RangeEnd:       y.RangeEnd,
		IsYTD:          y.IsYTD,
		RealisedPnLUSD: y.RealisedPnLUSD,
		RealisedPnLEUR: y.RealisedPnLEUR,
		DividendsUSD:   y.DividendsUSD,
		DividendsEUR:   y.DividendsEUR,
		FeesUSD:        y.FeesUSD,
		FeesEUR:        y.FeesEUR,
		InterestUSD:    y.InterestUSD,
		InterestEUR:    y.InterestEUR,
		NetUSD:         y.NetUSD,
		NetEUR:         y.NetEUR,
		ComputedPnLUSD: y.ComputedPnLUSD,
		ComputedPnLEUR: y.ComputedPnLEUR,
		ReconDeltaUSD:  y.ReconDeltaUSD,
		ReconDeltaEUR:  y.ReconDeltaEUR,
	}
	for _, a := range y.Assets {
		row.Assets = append(row.Assets, store.EtoroAssetRow{
			AssetType:       a.AssetType,
			RealisedPnLUSD:  a.RealisedPnLUSD,
			RealisedPnLEUR:  a.RealisedPnLEUR,
			RealisedDiscUSD: a.RealisedDiscUSD,
			RealisedDiscEUR: a.RealisedDiscEUR,
			RealisedCopyUSD: a.RealisedCopyUSD,
			RealisedCopyEUR: a.RealisedCopyEUR,
			DividendsUSD:    a.DividendsUSD,
			DividendsEUR:    a.DividendsEUR,
			FeesUSD:         a.FeesUSD,
			FeesEUR:         a.FeesEUR,
			TradeCount:      a.TradeCount,
		})
	}
	return row
}
