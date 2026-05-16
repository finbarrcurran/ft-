package technicals

import (
	"context"
	"ft/internal/store"
	"log/slog"
	"strings"
	"time"
)

// Refresh runs the daily technicals pipeline for one ticker:
//   1. Read daily_bars from store.
//   2. Aggregate to weekly.
//   3. Compute ATR(14, weekly) + classify vol tier from ATR/price.
//   4. Detect S/R candidates.
//   5. Write back ATR + vol_tier_auto to holdings/watchlist row;
//      replace sr_candidates rows.
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
