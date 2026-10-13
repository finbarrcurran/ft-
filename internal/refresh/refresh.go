// Package refresh orchestrates a full market-data refresh: FX, stocks, crypto.
// Per-step failures are logged but don't abort the overall refresh — partial
// updates are better than no updates.
package refresh

import (
	"context"
	"fmt"
	"ft/internal/heatmap"
	"ft/internal/market"
	"ft/internal/marketdata"
	"ft/internal/store"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// Result summarises a refresh run.
type Result struct {
	StartedAt        time.Time
	FinishedAt       time.Time
	StocksAttempted  int
	StocksUpdated    int
	CryptoAttempted  int
	CryptoUpdated    int
	HeatmapUpdated   int
	FXUpdated        bool
	Errors           []string
}

// Service holds the dependencies for a refresh run. New one per app instance.
type Service struct {
	Store *store.Store

	hmMu sync.Mutex
	hm   map[string]*hmState // per exchange: heatmap refresh bookkeeping
}

// hmState tracks when an exchange's heatmap tiles were last refreshed, and whether that
// happened while it was open (so one final refresh after the close still captures the
// closing prints).
type hmState struct {
	last      time.Time
	whileOpen bool
}

func New(st *store.Store) *Service {
	return &Service{Store: st, hm: map[string]*hmState{}}
}

// heatmapInterval is the minimum gap between heatmap refreshes of an open exchange
// (work order 2026-10-13: every 30 minutes, not every 15). Slightly under 30 min so the
// 15-minute cycle's jitter never skips a whole extra cycle.
const heatmapInterval = 28 * time.Minute

// heatmapDue says whether an exchange's heatmap tiles should be refreshed now:
//   - open: at most every 30 minutes;
//   - closed: once after the close (to capture closing prints), then never until it opens;
//   - never refreshed yet (startup): yes, so the heatmap is populated.
// Closed exchanges keep their last live values untouched.
func (s *Service) heatmapDue(exchange string, open bool, now time.Time) bool {
	s.hmMu.Lock()
	defer s.hmMu.Unlock()
	st := s.hm[exchange]
	if st == nil || st.last.IsZero() {
		return true
	}
	if open {
		return now.Sub(st.last) >= heatmapInterval
	}
	return st.whileOpen // closed: one final pass after having refreshed while open
}

func (s *Service) heatmapDone(exchange string, open bool, now time.Time) {
	s.hmMu.Lock()
	defer s.hmMu.Unlock()
	st := s.hm[exchange]
	if st == nil {
		st = &hmState{}
		s.hm[exchange] = st
	}
	st.last = now
	st.whileOpen = open
}

// RefreshAll fetches FX → stocks → crypto and writes results back, all owned
// by the given userID. Designed to be idempotent and safe under concurrent
// callers (manual button + background scheduler can both call it; the latest
// values just win).
func (s *Service) RefreshAll(ctx context.Context, userID int64) *Result {
	r := &Result{StartedAt: time.Now().UTC()}

	// 1) FX. If this fails, fall back to whatever's in meta (or seed default 1.08).
	if fx, err := market.FetchEURUSD(ctx); err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("fx: %s", err))
	} else if fx != nil {
		if err := s.Store.SetMeta(ctx, "fx_snapshot_eur_usd", strconv.FormatFloat(fx.EURToUSD, 'f', 6, 64)); err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("fx persist: %s", err))
		} else {
			r.FXUpdated = true
		}
	}

	// 2) Stocks. Read tickers, batch quote, write back.
	stocks, err := s.Store.ListStockHoldings(ctx, userID)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("list stocks: %s", err))
	} else {
		tickers := make([]string, 0, len(stocks))
		for _, h := range stocks {
			if h.Ticker != nil && *h.Ticker != "" {
				tickers = append(tickers, *h.Ticker)
			}
		}
		r.StocksAttempted = len(tickers)
		quotes := market.FetchStockQuotes(ctx, tickers)
		for _, q := range quotes {
			// Yahoo chart enrichment: if the quote provider didn't return
			// RSI/MA (Finnhub free returns price only for US tickers), try to
			// fill them in from Yahoo's /v8/chart endpoint. Best-effort.
			if q.RSI14 == nil || q.MA50 == nil {
				market.EnrichStockHistory(ctx, q)
			}
			var dailyChange *float64
			if q.ChangePct != 0 {
				v := q.ChangePct
				dailyChange = &v
			}
			if err := s.Store.UpdateStockMarketData(ctx, userID, q.Ticker, q.Price, q.RSI14, q.MA50, q.MA200, dailyChange); err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("update %s: %s", q.Ticker, err))
				continue
			}
			// Also seed the heatmap live cache so the global view has fresh
			// numbers for tickers that overlap with the user's portfolio.
			heatmap.SetLiveQuote(q.Ticker, q.Price, q.ChangePct)
			r.StocksUpdated++
		}
	}

	// 3) Crypto.
	cryptos, err := s.Store.ListCryptoHoldings(ctx, userID)
	if err != nil {
		r.Errors = append(r.Errors, fmt.Sprintf("list crypto: %s", err))
	} else {
		symbols := make([]string, 0, len(cryptos))
		for _, h := range cryptos {
			symbols = append(symbols, h.Symbol)
		}
		r.CryptoAttempted = len(symbols)
		quotes := market.FetchCryptoQuotes(ctx, symbols)
		for _, q := range quotes {
			var dailyChange *float64
			if q.Change24hPct != 0 {
				v := q.Change24hPct
				dailyChange = &v
			}
			if err := s.Store.UpdateCryptoMarketData(ctx, userID, q.Symbol, q.PriceUSD, q.PriceEUR, q.Change7dPct, q.Change30dPct, dailyChange); err != nil {
				r.Errors = append(r.Errors, fmt.Sprintf("update %s: %s", q.Symbol, err))
				continue
			}
			r.CryptoUpdated++
		}
	}

	// 4) Heatmap-only tickers (those in the global S&P sample but NOT already
	// fetched as part of the user's portfolio). Updates the in-memory live
	// cache only; no DB writes. Skipped if Finnhub key isn't set (would just
	// fail per-ticker anyway).
	if s.Store != nil {
		// Build the set of tickers we already have quotes for.
		seen := map[string]bool{}
		for _, h := range stocks {
			if h.Ticker != nil {
				seen[*h.Ticker] = true
			}
		}
		// Filter heatmap dataset to those not already done.
		var heatmapOnly []string
		for _, tk := range heatmap.AllTickers() {
			if !seen[tk] {
				heatmapOnly = append(heatmapOnly, tk)
			}
		}
		// Market-hours aware, per exchange (marketdata.ExchangeForTicker / Status): open
		// exchanges refresh every ~30 min, a closed one gets one last pass after the close,
		// and otherwise keeps its previous live values.
		now := time.Now()
		byEx := map[string][]string{}
		for _, tk := range heatmapOnly {
			ex := marketdata.ExchangeForTicker(tk)
			byEx[ex] = append(byEx[ex], tk)
		}
		for ex, tickers := range byEx {
			open := marketdata.Status(ex, now).Open
			if !s.heatmapDue(ex, open, now) {
				continue
			}
			hmQuotes := market.FetchStockQuotesYahoo(ctx, tickers) // SC-48: Yahoo, not Finnhub (60/min)
			for _, q := range hmQuotes {
				heatmap.SetLiveQuote(q.Ticker, q.Price, q.ChangePct)
				r.HeatmapUpdated++
			}
			s.heatmapDone(ex, open, now)
		}
	}

	r.FinishedAt = time.Now().UTC()
	_ = s.Store.SetMeta(ctx, "last_refreshed_at", strconv.FormatInt(r.FinishedAt.Unix(), 10))
	if len(r.Errors) > 0 {
		_ = s.Store.SetMeta(ctx, "last_partial_failure_at", strconv.FormatInt(r.FinishedAt.Unix(), 10))
		slog.Warn("refresh: partial failure",
			"errors", len(r.Errors),
			"stocks", fmt.Sprintf("%d/%d", r.StocksUpdated, r.StocksAttempted),
			"crypto", fmt.Sprintf("%d/%d", r.CryptoUpdated, r.CryptoAttempted),
		)
	} else {
		slog.Info("refresh: ok",
			"stocks", fmt.Sprintf("%d/%d", r.StocksUpdated, r.StocksAttempted),
			"crypto", fmt.Sprintf("%d/%d", r.CryptoUpdated, r.CryptoAttempted),
			"heatmap", r.HeatmapUpdated,
			"fx", r.FXUpdated,
			"took", r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond),
		)
	}
	return r
}
