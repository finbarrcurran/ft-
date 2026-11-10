// FT-081 — stock-split guard for the sector ETF series.
//
// sector_snapshots stores the close seen on the day it was ingested. When an ETF splits, older rows keep the
// pre-split price, so a 1W/3M return or the ratio vs SPY compares across the split (SOXX 450 -> 132 on 2026-11-05
// read as -71%). Yahoo's daily history is split-adjusted, so a repair re-reads it and replaces the stored closes.
// No ETF moves 40% or more in one day, so a single-day ratio outside the band below is treated as a split.

package sector_rotation

import (
	"context"
	"fmt"
	"ft/internal/market"
	"ft/internal/store"
	"log/slog"
	"math"
	"sort"
	"time"
)

// Day-over-day close ratios outside [stepLow, stepHigh] are treated as a split or a data break.
const (
	stepLow  = 0.6
	stepHigh = 1.7
)

// FindStep reports the index of the first close whose ratio to the previous close is outside the split band.
func FindStep(closes []float64) (idx int, ratio float64, ok bool) {
	for i := 1; i < len(closes); i++ {
		if closes[i-1] <= 0 || closes[i] <= 0 {
			continue
		}
		r := closes[i] / closes[i-1]
		if r < stepLow || r > stepHigh {
			return i, r, true
		}
	}
	return 0, 0, false
}

// RepairChange is one stored close that differs from the adjusted history.
type RepairChange struct {
	SectorID int64
	Code     string
	Ticker   string
	Date     string
	Old, New float64
}

// RepairResult summarises a repair pass. Changes is empty when nothing needed fixing.
type RepairResult struct {
	TickersWithStep []string
	Changes         []RepairChange
	Applied         bool
	Errors          []string
}

// DailySource returns split-adjusted daily closes for a ticker (market.FetchYahooDailyCloses in production).
type DailySource func(ctx context.Context, ticker, rng string) ([]market.DailyClose, error)

// RepairSplits looks for a split step in each sector's stored primary-ETF series. For every ETF with a step it
// reads the adjusted history and plans an update for each stored date whose close differs by more than 0.5%.
// With apply=false nothing is written (dry run).
func RepairSplits(ctx context.Context, st *store.Store, src DailySource, apply bool) *RepairResult {
	res := &RepairResult{}
	sectors, err := st.ListSectorUniverse(ctx)
	if err != nil {
		res.Errors = append(res.Errors, "list sectors: "+err.Error())
		return res
	}
	type snapSet struct {
		sector store.SectorUniverseRow
		snaps  []store.SectorSnapshot
	}
	byTicker := map[string][]snapSet{}
	stepped := map[string]bool{}
	for _, s := range sectors {
		snaps, err := st.ListRecentSnapshots(ctx, s.ID, 800)
		if err != nil || len(snaps) == 0 {
			continue
		}
		closes := make([]float64, len(snaps))
		for i, sn := range snaps {
			closes[i] = sn.ClosePrimary
		}
		byTicker[s.ETFTickerPrimary] = append(byTicker[s.ETFTickerPrimary], snapSet{s, snaps})
		if _, _, ok := FindStep(closes); ok {
			stepped[s.ETFTickerPrimary] = true
		}
	}
	for t := range stepped {
		res.TickersWithStep = append(res.TickersWithStep, t)
	}
	sort.Strings(res.TickersWithStep)
	for _, ticker := range res.TickersWithStep {
		daily, err := src(ctx, ticker, "5y")
		if err != nil || len(daily) == 0 {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: no adjusted history (%v)", ticker, err))
			continue
		}
		adj := make(map[string]float64, len(daily))
		dates := make([]string, 0, len(daily))
		for _, d := range daily {
			adj[d.Date] = d.Close
			dates = append(dates, d.Date)
		}
		sort.Strings(dates)
		for _, set := range byTicker[ticker] {
			for _, sn := range set.snaps {
				nv, ok := adjustedClose(adj, dates, sn.Date)
				if !ok || nv <= 0 || math.Abs(nv-sn.ClosePrimary)/sn.ClosePrimary <= 0.005 {
					continue
				}
				res.Changes = append(res.Changes, RepairChange{
					SectorID: set.sector.ID, Code: set.sector.Code, Ticker: ticker,
					Date: sn.Date, Old: sn.ClosePrimary, New: nv,
				})
			}
		}
	}
	if apply {
		for _, c := range res.Changes {
			if err := st.UpdateSectorPrimaryClose(ctx, c.SectorID, c.Date, c.New); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s %s: %v", c.Code, c.Date, err))
			}
		}
		res.Applied = len(res.Errors) == 0
		if len(res.Changes) > 0 {
			slog.Info("sector_rotation split repair", "tickers", res.TickersWithStep, "rows", len(res.Changes), "errs", len(res.Errors))
		}
	}
	return res
}

// adjustedClose returns the adjusted close for a stored date. Weekend and holiday rows were stored by carrying the
// last close forward, so they have no bar of their own: use the latest bar on or before the date, if it is within 7 days.
func adjustedClose(adj map[string]float64, sortedDates []string, date string) (float64, bool) {
	if v, ok := adj[date]; ok {
		return v, true
	}
	i := sort.SearchStrings(sortedDates, date) // first index with sortedDates[i] >= date
	if i == 0 {
		return 0, false
	}
	prev := sortedDates[i-1]
	pt, e1 := time.Parse("2006-01-02", prev)
	dt, e2 := time.Parse("2006-01-02", date)
	if e1 != nil || e2 != nil || dt.Sub(pt) > 7*24*time.Hour {
		return 0, false
	}
	return adj[prev], true
}
