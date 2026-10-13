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
	"encoding/json"
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
		"period":     etoroPeriodSummary(st.Period),
		"positions":  len(st.Positions),
		"dividends":  len(st.Dividends),
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

	// SC-50: merge, never replace. Positions and dividends are keyed by Position ID, the
	// whole-period lines are kept per statement period, and the per-year rows are re-derived
	// (a year is only REPLACED when the imported statements cover it).
	res, err := s.store.ApplyEtoroStatement(r.Context(), userID, etoroStatementToStore(pending.Statement))
	if err != nil {
		mapStoreError(w, err)
		return
	}
	applied := 0
	for _, y := range res.Years {
		if y.Action != "kept_existing" {
			applied++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"yearsApplied": applied,
		"fileName":     pending.Statement.FileName,
		"outcomes":     res.Years,
		"positionsNew": res.PositionsNew, "positionsKnown": res.PositionsKnown,
		"dividendsNew": res.DividendsNew, "dividendsKnown": res.DividendsKnown,
		"periodAction": res.PeriodAction,
		"warnings":     res.Warnings,
	})
}

// GET /api/etoro/performance — live annual + YTD history.
func (s *Server) handleEtoroPerformance(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	// SC-22 — the eToro performance history + YTD (SC-17) is sensitive net-worth
	// data; in demo mode it is hidden entirely (returns no years), so even an
	// accidental expand during a demo reveals nothing real (handover §2/§6).
	if s.demoModeOn(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"years": []any{}, "demo": true})
		return
	}
	years, err := s.store.ListEtoroPerformance(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years})
}

// etoroPeriodSummary is the whole-period Financial Summary view for the preview dialog.
func etoroPeriodSummary(p etoro.Period) map[string]any {
	return map[string]any{
		"rangeStart": p.RangeStart, "rangeEnd": p.RangeEnd,
		"netUsd": p.NetUSD, "netEur": p.NetEUR,
		"interestUsd": p.InterestUSD, "refundsUsd": p.RefundsUSD, "otherIncomeUsd": p.OtherIncomeUSD,
		"unclassifiedUsd": p.UnclassifiedUSD, "unclassifiedEur": p.UnclassifiedEUR,
		"reconPnlDeltaUsd": p.ReconPnLDeltaUSD, "reconPnlDeltaEur": p.ReconPnLDeltaEUR,
		"reconNetDeltaUsd": p.ReconNetDeltaUSD, "reconNetDeltaEur": p.ReconNetDeltaEUR,
	}
}

// etoroStatementToStore maps the parsed statement onto the store's position-level types.
func etoroStatementToStore(st *etoro.Statement) store.EtoroStatementData {
	lines, _ := json.Marshal(st.Period.Lines)
	p := st.Period
	d := store.EtoroStatementData{
		FileName: st.FileName,
		Period: store.EtoroPeriod{
			RangeStart: p.RangeStart, RangeEnd: p.RangeEnd,
			NetUSD: p.NetUSD, NetEUR: p.NetEUR, RealisedUSD: p.RealisedUSD, RealisedEUR: p.RealisedEUR,
			DividendsUSD: p.DividendsUSD, DividendsEUR: p.DividendsEUR,
			DividendResidualUSD: p.DividendResidualUSD, DividendResidualEUR: p.DividendResidualEUR,
			FeesUSD: p.FeesUSD, FeesEUR: p.FeesEUR, InterestUSD: p.InterestUSD, InterestEUR: p.InterestEUR,
			RefundsUSD: p.RefundsUSD, RefundsEUR: p.RefundsEUR, OtherIncomeUSD: p.OtherIncomeUSD, OtherIncomeEUR: p.OtherIncomeEUR,
			UnclassifiedUSD: p.UnclassifiedUSD, UnclassifiedEUR: p.UnclassifiedEUR,
			ReconPnLDeltaUSD: p.ReconPnLDeltaUSD, ReconPnLDeltaEUR: p.ReconPnLDeltaEUR,
			ReconNetDeltaUSD: p.ReconNetDeltaUSD, ReconNetDeltaEUR: p.ReconNetDeltaEUR,
			FeesByType: p.FeesByType, LinesJSON: string(lines),
		},
	}
	for _, c := range st.Positions {
		d.Positions = append(d.Positions, store.EtoroPosition{PositionID: c.PositionID, CloseDate: c.CloseDate, Year: c.Year,
			AssetType: c.AssetType, IsCopy: c.IsCopy, ProfitUSD: c.ProfitUSD, ProfitEUR: c.ProfitEUR})
	}
	for _, v := range st.Dividends {
		d.Dividends = append(d.Dividends, store.EtoroDividend{Key: v.Key, PositionID: v.PositionID, PayDate: v.PayDate,
			Year: v.Year, AssetType: v.AssetType, USD: v.USD, EUR: v.EUR})
	}
	return d
}
