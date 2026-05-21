// v1.9.0 — Hourly earnings refresh.
//
// The full daily price-history job (RunDailyJob) is heavy: 30-day price
// pulls per holding, sparkline computation, volatility math. We don't want
// to run that hourly. RefreshEarningsOnly is the slim version — for each
// stock holding with a ticker, hit Yahoo's calendar endpoint and update
// earnings_date / ex_dividend_date. Nothing else.
//
// One Yahoo call per holding. ~25 holdings × ~3s per call ≈ 75s worst case
// with the current 250ms inter-call gap. Fits comfortably within the
// hourly schedule. Yahoo free tier handles this volume.

package refresh

import (
	"context"
	"ft/internal/market"
	"log/slog"
	"time"
)

// RefreshEarningsOnly walks active stock holdings and refreshes only
// earnings_date + ex_dividend_date via Yahoo. Used by the hourly cron
// (added v1.9.0) to catch new earnings prints as soon as Yahoo publishes
// the updated calendar (~2-6 hr after release).
//
// Logs but does not propagate per-holding errors — one bad ticker
// shouldn't block the rest.
func (s *Service) RefreshEarningsOnly(ctx context.Context, userID int64) {
	stocks, err := s.Store.ListStockHoldings(ctx, userID)
	if err != nil {
		slog.Error("earnings-only refresh: list stocks", "err", err)
		return
	}
	updated := 0
	for _, h := range stocks {
		if h.Ticker == nil || *h.Ticker == "" {
			continue
		}
		select {
		case <-ctx.Done():
			slog.Warn("earnings-only refresh: context cancelled", "updated", updated)
			return
		default:
		}
		ticker := *h.Ticker
		cal, err := market.FetchYahooCalendarDates(ctx, ticker)
		if err != nil || cal == nil {
			continue
		}
		if cal.EarningsDate == "" && cal.ExDivDate == "" {
			continue
		}
		if err := s.Store.SetCalendarDates(ctx, userID, ticker, cal.EarningsDate, cal.ExDivDate); err == nil {
			updated++
		}
		time.Sleep(250 * time.Millisecond) // Yahoo politeness
	}
	slog.Info("earnings-only refresh complete", "user_id", userID, "updated", updated)
}
