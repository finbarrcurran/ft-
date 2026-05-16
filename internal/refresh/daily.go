// Daily background job: populates price_history (sparklines), refreshes
// calendar dates (earnings + ex-div), and auto-resolves beta for stocks
// missing it. Designed to run at 04:00 UTC, well after US market close.
//
// All steps are best-effort: per-ticker failures are logged but never abort
// the whole job. Partial data is better than no data — sparklines render
// even if a few tickers fall over.

package refresh

import (
	"context"
	"fmt"
	"ft/internal/market"
	"ft/internal/store"
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
	yahooRange := pickYahooRange(days)

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

			// 1) Price history → price_history table.
			if pts, err := market.FetchYahooDailyCloses(ctx, ticker, yahooRange); err != nil {
				stockMu.Lock()
				r.Errors = append(r.Errors, fmt.Sprintf("history %s: %s", ticker, err))
				stockMu.Unlock()
			} else {
				// Trim to trailing `days` points so we don't store more than
				// we render (Yahoo's 1mo can return 22 trading days, 3mo ~63).
				trimmed := tailDailyCloses(pts, days)
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
	cryptoSem := make(chan struct{}, 3) // CoinGecko free tier ~30 req/min
	var cryptoWG sync.WaitGroup
	var cryptoMu sync.Mutex
	for _, h := range cryptos {
		h := h
		cryptoWG.Add(1)
		cryptoSem <- struct{}{}
		go func() {
			defer cryptoWG.Done()
			defer func() { <-cryptoSem }()

			pts, err := market.FetchCryptoDailyCloses(ctx, h.Symbol, days)
			if err != nil {
				cryptoMu.Lock()
				r.Errors = append(r.Errors, fmt.Sprintf("crypto history %s: %s", h.Symbol, err))
				cryptoMu.Unlock()
				return
			}
			trimmed := tailDailyCloses(pts, days)
			toStore := make([]store.PricePoint, 0, len(trimmed))
			for _, p := range trimmed {
				toStore = append(toStore, store.PricePoint{Date: p.Date, Close: p.Close})
			}
			if err := s.Store.InsertPriceHistoryBatch(ctx, h.Symbol, "crypto", toStore); err == nil {
				cryptoMu.Lock()
				r.CryptoHistoryOK++
				cryptoMu.Unlock()
			}
		}()
	}
	cryptoWG.Wait()

	// ---- Prune ----
	if n, err := s.Store.PrunePriceHistory(ctx, days+5); err == nil {
		r.PrunedRows = n
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
