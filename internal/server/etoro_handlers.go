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
	"strconv"
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

	// SC-51: where this statement starts relative to what is already imported.
	cov, err := s.store.EtoroCoverageFor(r.Context(), userID, time.Now())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"coverage":      cov,
		"startCheck":    store.ClassifyStatementStart(cov, st.Period.RangeStart),
		"fileName":   st.FileName,
		"years":      st.Years,
		"warnings":   st.Warnings,
		"period":     etoroPeriodSummary(st.Period),
		"positions":  len(st.Positions),
		"flows":      len(st.Flows),
		"flowSummary": st.FlowSum,
		"dividends":  len(st.Dividends),
		"ttlSeconds": int(pendingEtoroTTL.Seconds()),
	})
}

// POST /api/etoro/import/apply — applies the staged statement.
func (s *Server) handleEtoroImportApply(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	// SC-51: a statement that starts after the coverage end leaves days with no data; apply
	// only on explicit confirmation (the staged statement stays available for the retry).
	var body struct {
		ConfirmGap bool `json:"confirmGap"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	pendingEtorosMu.Lock()
	peek := pendingEtoros[userID]
	pendingEtorosMu.Unlock()
	if peek != nil && peek.Statement != nil && time.Since(peek.Stored) <= pendingEtoroTTL {
		cov, err := s.store.EtoroCoverageFor(r.Context(), userID, time.Now())
		if mapStoreError(w, err) {
			return
		}
		if sc := store.ClassifyStatementStart(cov, peek.Statement.Period.RangeStart); sc.Kind == "gap" && !body.ConfirmGap {
			writeJSON(w, http.StatusConflict, map[string]any{"error": sc.Message + " Confirm to import anyway.", "needsConfirm": true, "startCheck": sc})
			return
		}
	}

	pending := popPendingEtoro(userID)
	if pending == nil || pending.Statement == nil {
		writeError(w, http.StatusBadRequest, "no pending statement (expired or never previewed)")
		return
	}

	// SC-50: merge, never replace. Positions and dividends are keyed by Position ID, the
	// whole-period lines are kept per statement period, and the per-year rows are re-derived
	// (a year is only REPLACED when the imported statements cover it).
	res, err := s.store.ApplyEtoroStatement(r.Context(), userID, EtoroStatementToStore(pending.Statement))
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
		"flowsNew": res.FlowsNew, "flowsKnown": res.FlowsKnown,
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
	cov, err := s.store.EtoroCoverageFor(r.Context(), userID, time.Now())
	if mapStoreError(w, err) {
		return
	}
	flows, err := s.store.CapitalFlowSummary(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"years": years, "coverage": cov, "capitalFlows": flows})
}

// handleEtoroTradeStats — SC-51 Part A. Reads closed positions only (USD); never the dividend or
// Income & other tables. ?scope=discretionary|copy|all (default discretionary), ?year=2026|all,
// ?minAmount=N (exclude positions with less than $N invested; 0 = off).
func (s *Server) handleEtoroTradeStats(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	if s.demoModeOn(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"demo": true})
		return
	}
	q := r.URL.Query()
	year := q.Get("year")
	if year == "" {
		year = "all"
	}
	if year != "all" {
		if y, err := strconv.Atoi(year); err != nil || y < 2000 || y > 2100 {
			writeError(w, http.StatusBadRequest, "year must be a 4-digit year or 'all'")
			return
		}
	}
	minAmount := 0.0
	if v := q.Get("minAmount"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			writeError(w, http.StatusBadRequest, "minAmount must be a number >= 0")
			return
		}
		minAmount = f
	}
	res, err := s.store.EtoroTradeStats(r.Context(), userID, q.Get("scope"), year, minAmount)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
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

// EtoroStatementToStore maps the parsed statement onto the store's position-level types.
func EtoroStatementToStore(st *etoro.Statement) store.EtoroStatementData {
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
			AssetType: c.AssetType, IsCopy: c.IsCopy, ProfitUSD: c.ProfitUSD, ProfitEUR: c.ProfitEUR,
			OpenDate: c.OpenDate, AmountUSD: c.AmountUSD})
	}
	for _, fl := range st.Flows {
		d.Flows = append(d.Flows, store.EtoroFlow{Key: fl.Key, OccurredAt: fl.OccurredAt, Day: fl.Day, Kind: fl.Kind, Details: fl.Details, AmountUSD: fl.AmountUSD})
	}
	if st.FlowSum.Present || len(st.Flows) > 0 {
		b, _ := json.Marshal(st.FlowSum)
		d.FlowsJSON = string(b)
	}
	for _, v := range st.Dividends {
		d.Dividends = append(d.Dividends, store.EtoroDividend{Key: v.Key, PositionID: v.PositionID, PayDate: v.PayDate,
			Year: v.Year, AssetType: v.AssetType, USD: v.USD, EUR: v.EUR})
	}
	return d
}

// GET /api/etoro/capital-flows?from=&to= — SC-52 P4. Dated deposits, withdrawals and transfers
// from the statements' Account Activity (plus any manual rows), with year totals.
func (s *Server) handleEtoroCapitalFlows(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	if s.demoModeOn(r.Context()) {
		writeJSON(w, http.StatusOK, map[string]any{"demo": true, "flows": []any{}})
		return
	}
	q := r.URL.Query()
	for _, d := range []string{q.Get("from"), q.Get("to")} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				writeError(w, http.StatusBadRequest, "from/to must be YYYY-MM-DD")
				return
			}
		}
	}
	fl, err := s.store.ListCapitalFlows(r.Context(), userID, q.Get("from"), q.Get("to"))
	if mapStoreError(w, err) {
		return
	}
	sum, err := s.store.CapitalFlowSummary(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": fl, "summary": sum})
}

// POST /api/etoro/capital-flows {"day","direction":"in|out","amountUsd","note"} — the manual
// fallback for a period without a statement. DELETE /api/etoro/capital-flows/{id} removes a
// manual row only.
func (s *Server) handleEtoroCapitalFlowAdd(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req struct {
		Day       string  `json:"day"`
		Direction string  `json:"direction"`
		AmountUSD float64 `json:"amountUsd"`
		Note      string  `json:"note"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	id, err := s.store.AddManualFlow(r.Context(), userID, req.Day, req.Direction, req.AmountUSD, req.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleEtoroCapitalFlowDelete(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	ok, err := s.store.DeleteManualFlow(r.Context(), userID, id)
	if mapStoreError(w, err) {
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no manual flow with that id (statement rows cannot be deleted)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}
