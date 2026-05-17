package performance

import (
	"context"
	"ft/internal/store"
	"sort"
	"time"
)

// GenerateSnapshots runs the cohort-aggregation pass and UPSERTs one row
// per (cohort × window) into performance_snapshots. Spec 9d D4.
//
// Called nightly from the daily cron AND manually via `ft perf-derive`
// (after a derivation pass, to refresh aggregates).
//
// Windows: "all" | "365d" | "90d" | "30d". The frontend's time-window
// selector reads from these pre-computed rows so it's instant.
func GenerateSnapshots(ctx context.Context, st *store.Store) error {
	trades, err := st.ListClosedTrades(ctx, 100000)
	if err != nil {
		return err
	}
	// Convert store rows to performance.ClosedTrade.
	cts := make([]ClosedTrade, 0, len(trades))
	for _, r := range trades {
		cts = append(cts, fromStoreRow(r))
	}
	// Sort by closed_at ASC for max-drawdown calc.
	sort.Slice(cts, func(i, j int) bool { return cts[i].ClosedAt.Before(cts[j].ClosedAt) })

	now := time.Now().UTC()
	date := now.Format("2006-01-02")
	windows := map[string]time.Time{
		"all":  time.Time{},
		"365d": now.AddDate(-1, 0, 0),
		"90d":  now.AddDate(0, 0, -90),
		"30d":  now.AddDate(0, 0, -30),
	}

	for window, cutoff := range windows {
		// Filter trades within window.
		filtered := cts
		if !cutoff.IsZero() {
			filtered = make([]ClosedTrade, 0, len(cts))
			for _, t := range cts {
				if t.ClosedAt.After(cutoff) {
					filtered = append(filtered, t)
				}
			}
		}
		// Group + roll up.
		grouped := GroupByCohort(filtered)
		for cohortKey, cohortTrades := range grouped {
			m := ComputeMetrics(cohortTrades)
			row := store.PerformanceSnapshotRow{
				Date:                 date,
				CohortKey:            cohortKey,
				Window:               window,
				TradeCount:           m.Count,
				WinCount:             m.WinCount,
				WinRate:              m.WinRate,
				AvgWinnerR:           m.AvgWinnerR,
				AvgLoserR:            m.AvgLoserR,
				ExpectancyR:          m.Expectancy,
				TotalRealizedPnLUSD:  m.TotalPnLUSD,
				AvgHoldingPeriodDays: m.AvgHoldDays,
				MaxDrawdownPct:       m.MaxDrawdownPct,
			}
			if err := st.UpsertPerformanceSnapshot(ctx, row); err != nil {
				return err
			}
		}
	}
	return nil
}

// fromStoreRow converts the store-layer ClosedTradeRow into the
// performance-layer ClosedTrade. (Two structs intentional: store stays
// flat for SQL clarity; performance carries JSON tags for handler use.)
func fromStoreRow(r *store.ClosedTradeRow) ClosedTrade {
	return ClosedTrade{
		Ticker:                r.Ticker,
		Kind:                  r.Kind,
		HoldingID:             r.HoldingID,
		OpenedAt:              r.OpenedAt,
		SetupType:             r.SetupType,
		RegimeEffective:       r.RegimeEffective,
		JordiScore:            r.JordiScore,
		CowenScore:            r.CowenScore,
		PercocoScore:          r.PercocoScore,
		ATRWeeklyAtEntry:      r.ATRWeeklyAtEntry,
		VolTierAtEntry:        r.VolTierAtEntry,
		Support1AtEntry:       r.Support1AtEntry,
		Resistance1AtEntry:    r.Resistance1AtEntry,
		Resistance2AtEntry:    r.Resistance2AtEntry,
		EntryPrice:            r.EntryPrice,
		SLAtEntry:             r.SLAtEntry,
		TP1AtEntry:            r.TP1AtEntry,
		TP2AtEntry:            r.TP2AtEntry,
		RMultipleTP1Planned:   r.RMultipleTP1Planned,
		RMultipleTP2Planned:   r.RMultipleTP2Planned,
		PositionSizeUnits:     r.PositionSizeUnits,
		PositionSizeUSD:       r.PositionSizeUSD,
		PerTradeRiskPct:       r.PerTradeRiskPct,
		PerTradeRiskUSD:       r.PerTradeRiskUSD,
		PortfolioValueAtEntry: r.PortfolioValueAtEntry,
		ClosedAt:              r.ClosedAt,
		ExitReason:            r.ExitReason,
		ExitPriceAvg:          r.ExitPriceAvg,
		HoldingPeriodDays:     r.HoldingPeriodDays,
		RealizedPnLUSD:        r.RealizedPnLUSD,
		RealizedPnLPct:        r.RealizedPnLPct,
		RealizedRMultiple:     r.RealizedRMultiple,
		SourceAuditOpenID:     r.SourceAuditOpenID,
		SourceAuditCloseID:    r.SourceAuditCloseID,
		DerivedAt:             r.DerivedAt,
	}
}
