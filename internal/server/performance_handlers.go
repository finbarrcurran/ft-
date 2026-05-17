// Spec 9d — Performance tab handlers.
//
// Endpoints (all token-or-cookie):
//   GET /api/performance/overview     headline + R histogram + equity curve
//   GET /api/performance/cohorts      grid: per-cohort metrics for one window
//   GET /api/performance/calibration  monotonic check per framework (D6)
//   GET /api/performance/cohort/{key} drill-down trade list (D8)
//   GET /api/performance/export.csv   CSV download of closed_trades (D10)

package server

import (
	"encoding/csv"
	"fmt"
	"ft/internal/performance"
	"ft/internal/store"
	"net/http"
	"sort"
	"time"
)

// GET /api/performance/overview?window=all|365d|90d|30d
func (s *Server) handlePerformanceOverview(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "all"
	}
	cutoff := windowCutoff(window)

	trades, err := s.store.ListClosedTrades(r.Context(), 100000)
	if mapStoreError(w, err) {
		return
	}
	filtered := filterByWindow(trades, cutoff)
	cts := toPerfTrades(filtered)
	sort.Slice(cts, func(i, j int) bool { return cts[i].ClosedAt.Before(cts[j].ClosedAt) })

	metrics := performance.ComputeMetrics(cts)
	histogram := performance.HistogramOf(cts)

	// Equity curve: combine portfolio_value_history with overlay of trades
	// opened/closed per day + realized R sum.
	pvh, _ := s.store.GetPortfolioValueHistory(r.Context(), 730)
	openedByDate := map[string]int{}
	closedByDate := map[string]int{}
	rByDate := map[string]float64{}
	for _, t := range cts {
		openedByDate[t.OpenedAt.Format("2006-01-02")]++
		dateKey := t.ClosedAt.Format("2006-01-02")
		closedByDate[dateKey]++
		rByDate[dateKey] += t.RealizedRMultiple
	}
	equity := make([]performance.EquityPoint, 0, len(pvh))
	var peak float64
	for _, p := range pvh {
		if p.Total > peak {
			peak = p.Total
		}
		dd := 0.0
		if peak > 0 {
			dd = (p.Total - peak) / peak * 100
		}
		equity = append(equity, performance.EquityPoint{
			Date:              p.Date,
			PortfolioValue:    p.Total,
			DrawdownFromPeak:  dd,
			TradesOpenedToday: openedByDate[p.Date],
			TradesClosedToday: closedByDate[p.Date],
			RealizedR:         rByDate[p.Date],
		})
	}

	// Underwater fraction (Spec 9d D7).
	totalDays := len(equity)
	underwaterDays := 0
	for _, e := range equity {
		if e.DrawdownFromPeak < 0 {
			underwaterDays++
		}
	}
	underwaterPct := 0.0
	if totalDays > 0 {
		underwaterPct = float64(underwaterDays) / float64(totalDays) * 100
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"window":        window,
		"metrics":       metrics,
		"histogram":     histogram,
		"equity":        equity,
		"underwaterPct": underwaterPct,
		"sampleSize":    len(cts),
	})
}

// GET /api/performance/cohorts?window=all|365d|90d|30d
//
// Returns the per-cohort breakdown. Read directly from
// performance_snapshots (cached) so this is a single query → render.
func (s *Server) handlePerformanceCohorts(w http.ResponseWriter, r *http.Request) {
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "all"
	}
	rows, err := s.store.GetLatestPerformanceSnapshots(r.Context())
	if mapStoreError(w, err) {
		return
	}
	type cohortOut struct {
		Key         string  `json:"key"`
		Label       string  `json:"label"`
		TradeCount  int     `json:"tradeCount"`
		WinRate     float64 `json:"winRate"`
		AvgWinR     float64 `json:"avgWinR"`
		AvgLossR    float64 `json:"avgLossR"`
		Expectancy  float64 `json:"expectancy"`
		TotalPnLUSD float64 `json:"totalPnlUsd"`
		LowConfidence bool  `json:"lowConfidence"` // n<5
	}
	out := []cohortOut{}
	for _, row := range rows {
		if row.Window != window {
			continue
		}
		out = append(out, cohortOut{
			Key:           row.CohortKey,
			Label:         performance.CohortDisplayLabel(row.CohortKey),
			TradeCount:    row.TradeCount,
			WinRate:       row.WinRate,
			AvgWinR:       row.AvgWinnerR,
			AvgLossR:      row.AvgLoserR,
			Expectancy:    row.ExpectancyR,
			TotalPnLUSD:   row.TotalRealizedPnLUSD,
			LowConfidence: row.TradeCount < 5,
		})
	}
	// Sort by trade count desc so 'all' surfaces first.
	sort.Slice(out, func(i, j int) bool { return out[i].TradeCount > out[j].TradeCount })
	writeJSON(w, http.StatusOK, map[string]any{
		"window":  window,
		"cohorts": out,
	})
}

// GET /api/performance/calibration
//
// Spec 9d D6: "Does scoring trades higher actually produce better
// outcomes?". For each framework (jordi, cowen, percoco), pulls the
// three cohort expectancies (≤8, 9-12, 13-16) and runs MonotonicCheck.
func (s *Server) handlePerformanceCalibration(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.GetLatestPerformanceSnapshots(r.Context())
	if mapStoreError(w, err) {
		return
	}
	allWindow := map[string]float64{}
	allWindowCount := map[string]int{}
	for _, row := range rows {
		if row.Window == "all" {
			allWindow[row.CohortKey] = row.ExpectancyR
			allWindowCount[row.CohortKey] = row.TradeCount
		}
	}
	type calibFW struct {
		Framework  string             `json:"framework"`
		Buckets    map[string]float64 `json:"buckets"`
		Counts     map[string]int     `json:"counts"`
		Monotonic  bool               `json:"monotonic"`
		Warning    string             `json:"warning,omitempty"`
		Sufficient bool               `json:"sufficient"` // all 3 buckets have n>=5
	}
	out := []calibFW{}
	for _, fw := range []string{"jordi", "cowen", "percoco"} {
		le8 := allWindow[fw+":le-8"]
		mid := allWindow[fw+":9-12"]
		top := allWindow[fw+":13-16"]
		mono, warn := performance.MonotonicCheck(le8, mid, top)
		out = append(out, calibFW{
			Framework: fw,
			Buckets: map[string]float64{
				"le-8":  le8,
				"9-12":  mid,
				"13-16": top,
			},
			Counts: map[string]int{
				"le-8":  allWindowCount[fw+":le-8"],
				"9-12":  allWindowCount[fw+":9-12"],
				"13-16": allWindowCount[fw+":13-16"],
			},
			Monotonic:  mono,
			Warning:    warn,
			Sufficient: allWindowCount[fw+":le-8"] >= 5 && allWindowCount[fw+":9-12"] >= 5 && allWindowCount[fw+":13-16"] >= 5,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"frameworks": out})
}

// GET /api/performance/cohort/{key}?window=all|...
//
// Drill-down: list of trades belonging to one cohort, sorted by closed_at DESC.
func (s *Server) handlePerformanceCohortDrill(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "cohort key required")
		return
	}
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "all"
	}
	cutoff := windowCutoff(window)
	trades, err := s.store.ListClosedTrades(r.Context(), 100000)
	if mapStoreError(w, err) {
		return
	}
	filtered := filterByWindow(trades, cutoff)
	cts := toPerfTrades(filtered)
	out := []performance.ClosedTrade{}
	for _, t := range cts {
		for _, c := range performance.AssignCohorts(t) {
			if c == key {
				out = append(out, t)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClosedAt.After(out[j].ClosedAt) })
	writeJSON(w, http.StatusOK, map[string]any{
		"key":    key,
		"label":  performance.CohortDisplayLabel(key),
		"window": window,
		"trades": out,
	})
}

// GET /api/performance/export.csv — Spec 9d D10.
func (s *Server) handlePerformanceExport(w http.ResponseWriter, r *http.Request) {
	trades, err := s.store.ListClosedTrades(r.Context(), 100000)
	if mapStoreError(w, err) {
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ft-closed-trades-%s.csv"`, time.Now().UTC().Format("2006-01-02")))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"ticker", "kind", "opened_at", "closed_at", "holding_period_days",
		"setup_type", "regime_effective", "jordi_score", "cowen_score", "percoco_score",
		"entry_price", "sl_at_entry", "tp1_at_entry", "tp2_at_entry",
		"position_size_units", "position_size_usd", "per_trade_risk_pct",
		"exit_reason", "exit_price_avg", "realized_pnl_usd", "realized_pnl_pct", "realized_r_multiple",
		"atr_weekly_at_entry", "vol_tier_at_entry",
	})
	for _, t := range trades {
		_ = cw.Write([]string{
			t.Ticker, t.Kind,
			t.OpenedAt.UTC().Format(time.RFC3339), t.ClosedAt.UTC().Format(time.RFC3339), fmt.Sprintf("%d", t.HoldingPeriodDays),
			t.SetupType, t.RegimeEffective,
			intPtrStr(t.JordiScore), intPtrStr(t.CowenScore), intPtrStr(t.PercocoScore),
			fmt.Sprintf("%.4f", t.EntryPrice), fmt.Sprintf("%.4f", t.SLAtEntry),
			fmt.Sprintf("%.4f", t.TP1AtEntry), fmt.Sprintf("%.4f", t.TP2AtEntry),
			fmt.Sprintf("%.6f", t.PositionSizeUnits), fmt.Sprintf("%.2f", t.PositionSizeUSD), fmt.Sprintf("%.2f", t.PerTradeRiskPct),
			t.ExitReason, fmt.Sprintf("%.4f", t.ExitPriceAvg),
			fmt.Sprintf("%.2f", t.RealizedPnLUSD), fmt.Sprintf("%.2f", t.RealizedPnLPct), fmt.Sprintf("%.4f", t.RealizedRMultiple),
			fmt.Sprintf("%.4f", t.ATRWeeklyAtEntry), t.VolTierAtEntry,
		})
	}
}

// ----- helpers -----------------------------------------------------------

func windowCutoff(window string) time.Time {
	now := time.Now().UTC()
	switch window {
	case "30d":
		return now.AddDate(0, 0, -30)
	case "90d":
		return now.AddDate(0, 0, -90)
	case "365d":
		return now.AddDate(-1, 0, 0)
	}
	return time.Time{} // 'all' window
}

func filterByWindow(trades []*store.ClosedTradeRow, cutoff time.Time) []*store.ClosedTradeRow {
	if cutoff.IsZero() {
		return trades
	}
	out := make([]*store.ClosedTradeRow, 0, len(trades))
	for _, t := range trades {
		if t.ClosedAt.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

func toPerfTrades(rows []*store.ClosedTradeRow) []performance.ClosedTrade {
	out := make([]performance.ClosedTrade, 0, len(rows))
	for _, r := range rows {
		out = append(out, performance.ClosedTrade{
			Ticker: r.Ticker, Kind: r.Kind, HoldingID: r.HoldingID,
			OpenedAt: r.OpenedAt, ClosedAt: r.ClosedAt,
			SetupType: r.SetupType, RegimeEffective: r.RegimeEffective,
			JordiScore: r.JordiScore, CowenScore: r.CowenScore, PercocoScore: r.PercocoScore,
			ATRWeeklyAtEntry: r.ATRWeeklyAtEntry, VolTierAtEntry: r.VolTierAtEntry,
			Support1AtEntry: r.Support1AtEntry, Resistance1AtEntry: r.Resistance1AtEntry,
			Resistance2AtEntry: r.Resistance2AtEntry,
			EntryPrice: r.EntryPrice, SLAtEntry: r.SLAtEntry,
			TP1AtEntry: r.TP1AtEntry, TP2AtEntry: r.TP2AtEntry,
			RMultipleTP1Planned: r.RMultipleTP1Planned, RMultipleTP2Planned: r.RMultipleTP2Planned,
			PositionSizeUnits: r.PositionSizeUnits, PositionSizeUSD: r.PositionSizeUSD,
			PerTradeRiskPct: r.PerTradeRiskPct, PerTradeRiskUSD: r.PerTradeRiskUSD,
			PortfolioValueAtEntry: r.PortfolioValueAtEntry,
			ExitReason: r.ExitReason, ExitPriceAvg: r.ExitPriceAvg,
			HoldingPeriodDays: r.HoldingPeriodDays,
			RealizedPnLUSD: r.RealizedPnLUSD, RealizedPnLPct: r.RealizedPnLPct,
			RealizedRMultiple: r.RealizedRMultiple,
			SourceAuditOpenID: r.SourceAuditOpenID, SourceAuditCloseID: r.SourceAuditCloseID,
			DerivedAt: r.DerivedAt,
		})
	}
	return out
}

func intPtrStr(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d", *p)
}

