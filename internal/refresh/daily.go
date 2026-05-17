// Daily background job: populates price_history (sparklines), refreshes
// calendar dates (earnings + ex-div), and auto-resolves beta for stocks
// missing it. Designed to run at 04:00 UTC, well after US market close.
//
// Spec 9c extension: also fetches 2y daily OHLC bars per stock, stores in
// daily_bars, and runs technicals.Refresh (ATR, vol_tier_auto, S/R
// candidates).
//
// All steps are best-effort: per-ticker failures are logged but never abort
// the whole job. Partial data is better than no data — sparklines render
// even if a few tickers fall over.

package refresh

import (
	"context"
	"fmt"
	"ft/internal/domain"
	"ft/internal/market"
	"ft/internal/store"
	"ft/internal/technicals"
	"log/slog"
	"sync"
	"time"
)

// DailyResult summarises a daily-job run.
type DailyResult struct {
	StartedAt        time.Time
	FinishedAt       time.Time
	StocksProcessed  int
	StocksHistoryOK  int
	CryptoProcessed  int
	CryptoHistoryOK  int
	CalendarOK       int
	BetaOK           int
	PrunedRows       int64
	Errors           []string
}

// RunDailyJob populates price_history for every active holding the user owns,
// refreshes earnings/ex-div + beta for stocks, and prunes old rows. Safe to
// re-run (price_history upserts are idempotent).
//
// `days` controls history depth (30 is the spec default).
func (s *Service) RunDailyJob(ctx context.Context, userID int64, days int) *DailyResult {
	if days <= 0 {
		days = 30
	}
	r := &DailyResult{StartedAt: time.Now().UTC()}

	stocks, err := s.Store.ListStockHoldings(ctx, userID)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("list stocks: %s", err))
	}
	cryptos, err := s.Store.ListCryptoHoldings(ctx, userID)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("list crypto: %s", err))
	}

	r.StocksProcessed = len(stocks)
	r.CryptoProcessed = len(cryptos)

	// ---- Stocks: history + calendar + beta in parallel per ticker ----------
	// Cap concurrency to avoid hammering Yahoo (per-ticker call burst will
	// trip rate limits otherwise). 4 in-flight is gentle and still finishes
	// 23 tickers in ~6-8s.
	stockSem := make(chan struct{}, 4)
	var stockWG sync.WaitGroup
	var stockMu sync.Mutex
	// Spec 9c: fetch 2y window once per ticker; we trim closes for the
	// sparkline AND store full OHLC for ATR/S-R. pickYahooRange retained
	// in case we want to revert to per-`days`-sized fetches.
	_ = pickYahooRange

	for _, h := range stocks {
		if h.Ticker == nil || *h.Ticker == "" {
			continue
		}
		h := h
		ticker := *h.Ticker
		stockWG.Add(1)
		stockSem <- struct{}{}
		go func() {
			defer stockWG.Done()
			defer func() { <-stockSem }()

			// 1) Fetch 2y OHLC bars (Spec 9c). Used for BOTH the existing
			// sparkline (trim to trailing `days` closes) AND the daily_bars
			// store for ATR/vol-tier/S-R computation.
			bars, err := market.FetchYahooDailyBars(ctx, ticker, "2y")
			if err != nil {
				stockMu.Lock()
				r.Errors = append(r.Errors, fmt.Sprintf("history %s: %s", ticker, err))
				stockMu.Unlock()
			} else {
				// Sparkline: trim to trailing `days` and store in price_history.
				closes := make([]market.DailyClose, 0, len(bars))
				for _, b := range bars {
					closes = append(closes, market.DailyClose{Date: b.Date, Close: b.Close})
				}
				trimmed := tailDailyCloses(closes, days)
				toStore := make([]store.PricePoint, 0, len(trimmed))
				for _, p := range trimmed {
					toStore = append(toStore, store.PricePoint{Date: p.Date, Close: p.Close})
				}
				if err := s.Store.InsertPriceHistoryBatch(ctx, ticker, "stock", toStore); err != nil {
					stockMu.Lock()
					r.Errors = append(r.Errors, fmt.Sprintf("write history %s: %s", ticker, err))
					stockMu.Unlock()
				} else {
					stockMu.Lock()
					r.StocksHistoryOK++
					stockMu.Unlock()
				}
				// daily_bars: full 2y OHLC.
				ohlcRows := make([]store.DailyBarRow, 0, len(bars))
				for _, b := range bars {
					ohlcRows = append(ohlcRows, store.DailyBarRow{
						Date: b.Date, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
					})
				}
				if err := s.Store.BulkInsertDailyBars(ctx, ticker, "stock", ohlcRows); err == nil {
					// Run the technicals pipeline (ATR, vol-tier, S/R).
					var price float64
					if h.CurrentPrice != nil {
						price = *h.CurrentPrice
					} else if len(bars) > 0 {
						price = bars[len(bars)-1].Close
					}
					if atrW, volT, err := technicals.Refresh(ctx, s.Store, ticker, "stock", price); err == nil && atrW > 0 {
						_ = s.Store.SetStockTechnicals(ctx, h.ID, atrW, volT)
					}
				}
			}

			// 2) Calendar dates (earnings, ex-div). Patchy outside US; ignore
			// errors per spec (frontend renders "—").
			if cal, err := market.FetchYahooCalendarDates(ctx, ticker); err == nil && cal != nil {
				if cal.EarningsDate != "" || cal.ExDivDate != "" {
					if err := s.Store.SetCalendarDates(ctx, userID, ticker, cal.EarningsDate, cal.ExDivDate); err == nil {
						stockMu.Lock()
						r.CalendarOK++
						stockMu.Unlock()
					}
				}
			}

			// 3) Beta auto-resolve if missing — drives suggested SL/TP.
			if h.Beta == nil {
				if beta, err := market.FetchYahooBeta(ctx, ticker); err == nil && beta != nil {
					if err := s.Store.SetStockBeta(ctx, userID, ticker, *beta); err == nil {
						stockMu.Lock()
						r.BetaOK++
						stockMu.Unlock()
					}
				}
			}
		}()
	}
	stockWG.Wait()

	// ---- Crypto: CoinGecko market_chart per symbol -------------------------
	// CoinGecko free tier penalises bursts hard (sticky 429 for ~minutes).
	// Sequential with a 2.5s gap fits well under the ~30 req/min limit and
	// completes 13 symbols in ~30s.
	for _, h := range cryptos {
		pts, err := market.FetchCryptoDailyCloses(ctx, h.Symbol, days)
		if err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("crypto history %s: %s", h.Symbol, err))
			// Still gap before next call so we don't compound the rate-limit.
			select {
			case <-ctx.Done():
				break
			case <-time.After(2500 * time.Millisecond):
			}
			continue
		}
		trimmed := tailDailyCloses(pts, days)
		toStore := make([]store.PricePoint, 0, len(trimmed))
		for _, p := range trimmed {
			toStore = append(toStore, store.PricePoint{Date: p.Date, Close: p.Close})
		}
		if err := s.Store.InsertPriceHistoryBatch(ctx, h.Symbol, "crypto", toStore); err == nil {
			r.CryptoHistoryOK++
		}
		select {
		case <-ctx.Done():
		case <-time.After(2500 * time.Millisecond):
		}
	}

	// ---- Prune ----
	if n, err := s.Store.PrunePriceHistory(ctx, days+5); err == nil {
		r.PrunedRows = n
	}

	// Spec 9c D13 — daily portfolio value snapshot for drawdown tracking.
	// Computed AFTER history + ATR work above so live prices are current.
	if err := s.snapshotPortfolioValue(ctx, userID, stocks, cryptos); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("portfolio snapshot: %s", err))
	}

	r.FinishedAt = time.Now().UTC()
	slog.Info("daily job done",
		"stocks_history", fmt.Sprintf("%d/%d", r.StocksHistoryOK, r.StocksProcessed),
		"crypto_history", fmt.Sprintf("%d/%d", r.CryptoHistoryOK, r.CryptoProcessed),
		"calendar", r.CalendarOK,
		"beta", r.BetaOK,
		"pruned", r.PrunedRows,
		"errs", len(r.Errors),
		"took", r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond),
	)
	return r
}

// snapshotPortfolioValue writes one row to portfolio_value_history for
// today (UTC). Idempotent: re-runs upsert on the same date.
func (s *Service) snapshotPortfolioValue(ctx context.Context, userID int64, stocks []*domain.StockHolding, cryptos []*domain.CryptoHolding) error {
	_ = userID
	stockUSD := 0.0
	cryptoUSD := 0.0
	for _, h := range stocks {
		if h.CurrentPrice != nil && h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 {
			qty := h.InvestedUSD / *h.AvgOpenPrice
			stockUSD += qty * *h.CurrentPrice
		} else {
			stockUSD += h.InvestedUSD
		}
	}
	for _, h := range cryptos {
		if h.CurrentValueUSD != nil {
			cryptoUSD += *h.CurrentValueUSD
		} else if h.CostBasisUSD != nil {
			cryptoUSD += *h.CostBasisUSD
		}
	}
	date := time.Now().UTC().Format("2006-01-02")
	return s.Store.UpsertPortfolioValue(ctx, date, stockUSD+cryptoUSD, stockUSD, cryptoUSD)
}

// pickYahooRange maps requested history depth to a Yahoo range string.
// Yahoo ranges are quantised; "1mo" returns ~22 trading days, "3mo" ~63.
func pickYahooRange(days int) string {
	switch {
	case days <= 7:
		return "5d"
	case days <= 22:
		return "1mo"
	case days <= 60:
		return "3mo"
	case days <= 180:
		return "6mo"
	default:
		return "1y"
	}
}

// tailDailyCloses returns the last `n` entries of `pts`.
func tailDailyCloses(pts []market.DailyClose, n int) []market.DailyClose {
	if len(pts) <= n {
		return pts
	}
	return pts[len(pts)-n:]
}

// ScheduleDailyJob fires `fn` at the next 04:00 UTC and every 24h thereafter.
// Returns when ctx is canceled. Block-call from a goroutine.
func ScheduleDailyJob(ctx context.Context, fn func()) {
	for {
		next := nextRunAt(time.Now().UTC(), 4, 0)
		wait := time.Until(next)
		slog.Info("daily job scheduled", "next", next.Format(time.RFC3339), "in", wait.Round(time.Second))

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		// Each run gets its own timeout via the caller's `fn`. Yahoo +
		// CoinGecko on 36 holdings finishes in ~8s normally, but rate-limit
		// retries can stretch it.
		fn()
	}
}

// nextRunAt returns the next moment after `now` at HH:MM UTC.
func nextRunAt(now time.Time, hour, minute int) time.Time {
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate
}
