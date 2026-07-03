package technicals

import (
	"context"
	"ft/internal/store"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Refresh runs the daily technicals pipeline for one ticker:
//  1. Read daily_bars from store.
//  2. Aggregate to weekly.
//  3. Compute ATR(14, weekly) + classify vol tier from ATR/price.
//  4. Detect S/R candidates.
//  5. Write back ATR + vol_tier_auto to holdings/watchlist row;
//     replace sr_candidates rows.
//
// Returns (atrWeekly, volTier) for caller telemetry. Returns zeros + nil
// error if data is too thin to compute (typical for newly-added tickers
// that haven't been backfilled).
func Refresh(ctx context.Context, st *store.Store, ticker, kind string, currentPrice float64) (atrWeekly float64, volTier string, err error) {
	bars, err := st.GetDailyBars(ctx, ticker, kind)
	if err != nil {
		return 0, "", err
	}
	if len(bars) < 30 {
		// Not enough history for meaningful ATR; skip silently.
		return 0, "", nil
	}

	// Daily → weekly.
	daily := make([]Bar, 0, len(bars))
	for _, b := range bars {
		t, err := time.Parse("2006-01-02", b.Date)
		if err != nil {
			continue
		}
		daily = append(daily, Bar{Date: t, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
	}
	weekly := AggregateToWeekly(daily)
	if len(weekly) < 15 {
		return 0, "", nil
	}

	// SC-35 W5 — persist the weekly aggregation. Three consumers now read the
	// weekly series (ATR, MA50W, runner swing-low logic); persisting once beats
	// re-deriving each. Ticker-keyed, so no holding ID needed here.
	wkRows := make([]store.WeeklyBarRow, 0, len(weekly))
	for _, b := range weekly {
		wkRows = append(wkRows, store.WeeklyBarRow{
			WeekStart: b.Date.Format("2006-01-02"),
			Open:      b.Open, High: b.High, Low: b.Low, Close: b.Close,
		})
	}
	if err := st.ReplaceWeeklyBars(ctx, strings.ToUpper(ticker), kind, wkRows); err != nil {
		slog.Warn("technicals: ReplaceWeeklyBars failed", "ticker", ticker, "err", err)
	}

	atrWeekly = ATR(weekly, 14)
	if atrWeekly == 0 {
		return 0, "", nil
	}
	volTier = VolTier(atrWeekly, currentPrice)

	// Detect S/R candidates if we have enough history.
	supports, resistances := DetectSR(weekly, atrWeekly, currentPrice, 3, 3)
	srRows := make([]store.SRCandidateRow, 0, len(supports)+len(resistances))
	for _, s := range supports {
		srRows = append(srRows, srRowFromCandidate(ticker, kind, s))
	}
	for _, s := range resistances {
		srRows = append(srRows, srRowFromCandidate(ticker, kind, s))
	}
	if err := st.ReplaceSRCandidates(ctx, strings.ToUpper(ticker), kind, srRows); err != nil {
		slog.Warn("technicals: ReplaceSRCandidates failed", "ticker", ticker, "err", err)
	}
	return atrWeekly, volTier, nil
}

// AutoFillHoldingLevels projects the nightly sr_candidates onto a holding's
// numbered support/resistance columns and writes the bar-computed trend MAs
// (SC-35 W1 + W2). Called by the daily cron AFTER Refresh, which has already
// persisted weekly_bars + sr_candidates for this ticker. Holding-ID-keyed
// because it writes stock_holdings columns.
//
//	Levels (W2 + SC-35.1): support_1/2 = the two NEAREST qualifying supports
//	BELOW price (closest first); resistance_1/2 = the two nearest qualifying
//	resistances ABOVE price (closest first). Qualification = presence in
//	sr_candidates; score DESC breaks ties on equal price. Classic weekly-pivot
//	fallback (off the last completed weekly bar) fills a side that has no
//	candidate. SetStockLevels enforces the manual-wins guard, so this
//	no-ops on rows the user pinned (levels_source='manual').
//	MAs (W1): always written (measurements, not chosen levels) — NULL when
//	history is too thin (<50 weekly / <200 daily bars).
func AutoFillHoldingLevels(ctx context.Context, st *store.Store, holdingID int64, ticker, kind string, currentPrice float64) error {
	up := strings.ToUpper(ticker)

	// ---- Trend MAs (always written) ----
	var ma50w, ma200d *float64
	if wk, err := st.GetWeeklyBars(ctx, up, kind); err == nil && len(wk) >= 50 {
		wb := make([]Bar, 0, len(wk))
		for _, b := range wk {
			t, _ := time.Parse("2006-01-02", b.WeekStart)
			wb = append(wb, Bar{Date: t, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
		}
		if v, ok := MA50Weekly(wb); ok {
			ma50w = v
		}
	}
	if db, err := st.GetDailyBars(ctx, up, kind); err == nil && len(db) >= 200 {
		dbar := make([]Bar, 0, len(db))
		for _, b := range db {
			t, _ := time.Parse("2006-01-02", b.Date)
			dbar = append(dbar, Bar{Date: t, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close})
		}
		if v, ok := MA200Daily(dbar); ok {
			ma200d = v
		}
	}
	if err := st.SetStockTrendMAs(ctx, holdingID, ma50w, ma200d); err != nil {
		slog.Warn("technicals: SetStockTrendMAs failed", "ticker", ticker, "err", err)
	}

	// ---- Numbered levels from sr_candidates ----
	cands, err := st.GetSRCandidates(ctx, up, kind)
	if err != nil {
		return err
	}
	// SC-35.1: numbered levels select the NEAREST qualifying candidate to price
	// (support_1 = closest support below, resistance_1 = closest above) rather
	// than the highest-scored on each side. Qualification = presence in
	// sr_candidates; score DESC breaks ties on equal price.
	type lvl struct {
		price float64
		score float64
	}
	var sup, res []lvl
	for _, c := range cands {
		switch c.LevelType {
		case "support":
			if currentPrice <= 0 || c.Price < currentPrice {
				sup = append(sup, lvl{c.Price, c.Score})
			}
		case "resistance":
			if currentPrice <= 0 || c.Price > currentPrice {
				res = append(res, lvl{c.Price, c.Score})
			}
		}
	}

	// Classic weekly-pivot fallback when a side is empty (score 0 — deterministic).
	if len(sup) == 0 || len(res) == 0 {
		if s1p, s2p, r1p, r2p, ok := classicWeeklyPivots(ctx, st, up, kind); ok {
			if len(sup) == 0 {
				if currentPrice <= 0 || s1p < currentPrice {
					sup = append(sup, lvl{s1p, 0})
				}
				if currentPrice <= 0 || s2p < currentPrice {
					sup = append(sup, lvl{s2p, 0})
				}
			}
			if len(res) == 0 {
				if currentPrice <= 0 || r1p > currentPrice {
					res = append(res, lvl{r1p, 0})
				}
				if currentPrice <= 0 || r2p > currentPrice {
					res = append(res, lvl{r2p, 0})
				}
			}
		}
	}

	// Nearest-first ordering: supports by price DESC (closest below = highest
	// price), resistances by price ASC (closest above = lowest price); score DESC
	// on equal price. Stable sort preserves score-DESC candidate order on ties.
	sort.SliceStable(sup, func(i, j int) bool {
		if sup[i].price != sup[j].price {
			return sup[i].price > sup[j].price
		}
		return sup[i].score > sup[j].score
	})
	sort.SliceStable(res, func(i, j int) bool {
		if res[i].price != res[j].price {
			return res[i].price < res[j].price
		}
		return res[i].score > res[j].score
	})

	supP := make([]float64, len(sup))
	for i, l := range sup {
		supP[i] = l.price
	}
	resP := make([]float64, len(res))
	for i, l := range res {
		resP[i] = l.price
	}
	s1, s2 := pickTwo(supP)
	r1, r2 := pickTwo(resP)
	return st.SetStockLevels(ctx, holdingID, s1, s2, r1, r2)
}

// classicWeeklyPivots computes floor-trader pivots off the last completed
// weekly bar: PP=(H+L+C)/3, R1=2·PP−L, S1=2·PP−H, R2=PP+(H−L), S2=PP−(H−L).
// The deterministic seed/fallback when sr_candidates is sparse (e.g. thin
// non-US history). ok=false when there are no weekly bars.
func classicWeeklyPivots(ctx context.Context, st *store.Store, ticker, kind string) (s1, s2, r1, r2 float64, ok bool) {
	wk, err := st.GetWeeklyBars(ctx, ticker, kind)
	if err != nil || len(wk) == 0 {
		return 0, 0, 0, 0, false
	}
	b := wk[len(wk)-1] // last available weekly bar
	pp := (b.High + b.Low + b.Close) / 3
	rng := b.High - b.Low
	return pp - (b.High - pp), pp - rng, pp + (pp - b.Low), pp + rng, true
}

// pickTwo returns pointers to the first two elements of xs (or nil), preserving
// the caller's ordering (nearest-to-price first for the sr_candidates path).
func pickTwo(xs []float64) (first, second *float64) {
	if len(xs) >= 1 {
		v := xs[0]
		first = &v
	}
	if len(xs) >= 2 {
		v := xs[1]
		second = &v
	}
	return first, second
}

func srRowFromCandidate(ticker, kind string, c SRCandidate) store.SRCandidateRow {
	return store.SRCandidateRow{
		Ticker:      strings.ToUpper(ticker),
		Kind:        kind,
		LevelType:   c.LevelType,
		Price:       c.Price,
		Touches:     c.Touches,
		LastTouchAt: c.LastTouchAt.Format("2006-01-02"),
		Score:       c.Score,
	}
}
