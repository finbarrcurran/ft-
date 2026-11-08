package refresh

// SC-57 — 52-week range, all-time high and market cap for the stock-watchlist rows.
// Refreshed by the daily job and by `ft market-stats-refresh`; the API only reads the stored rows.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ft/internal/market"
	"ft/internal/store"
)

// MarketStatsSource is where the figures come from. Production uses Yahoo; tests use fixtures.
type MarketStatsSource interface {
	Stats(ctx context.Context, symbol string) (*market.MarketStats, error)
}

type yahooStatsSource struct{}

func (yahooStatsSource) Stats(ctx context.Context, symbol string) (*market.MarketStats, error) {
	return market.FetchYahooMarketStats(ctx, symbol)
}

// MarketStatsSummary is what one pass did.
type MarketStatsSummary struct {
	Total, Answered, Failed int
	Errors                  []string
}

// RefreshMarketStats refreshes every stock-watchlist ticker once (provider symbol via ticker_symbol_map).
// A failed half never clears stored values; a name Yahoo does not know is stored as nulls with the error.
func (s *Service) RefreshMarketStats(ctx context.Context) MarketStatsSummary {
	src := s.MarketStats
	if src == nil {
		src = yahooStatsSource{}
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	gap := s.CalendarGap
	if gap == 0 {
		gap = 250 * time.Millisecond
	}
	var sum MarketStatsSummary
	set, err := s.Store.CalendarTickerSet(ctx)
	if err != nil {
		slog.Error("market stats: ticker set", "err", err)
		return sum
	}
	for _, ct := range set {
		if !ct.Watchlist {
			continue
		}
		select {
		case <-ctx.Done():
			slog.Warn("market stats: context ended before the pass finished", "done", sum.Total)
			return sum
		default:
		}
		sum.Total++
		st, err := src.Stats(ctx, ct.ProviderSymbol)
		w := store.MarketStatsWrite{Ticker: ct.Ticker, ProviderSymbol: ct.ProviderSymbol, Now: now().Unix()}
		if st == nil {
			st = &market.MarketStats{ChartErr: err, CapErr: err}
		}
		if st.ChartErr == nil {
			w.ChartOK, w.Currency, w.High52w, w.Low52w, w.ATH = true, st.Currency, st.High52w, st.Low52w, st.ATH
			w.ATHDate, w.HistFrom, w.HistTo, w.ChartAsOf = st.ATHDate, st.HistoryFrom, st.HistoryTo, st.ChartAsOf
		}
		if st.CapErr == nil {
			w.CapOK, w.MarketCap, w.CapAsOf = true, st.MarketCap, st.CapAsOf
		}
		if st.ChartErr != nil {
			w.Err = "chart: " + st.ChartErr.Error()
		}
		if st.CapErr != nil {
			if w.Err != "" {
				w.Err += "; "
			}
			w.Err += "cap: " + st.CapErr.Error()
		}
		if len(w.Err) > 300 {
			w.Err = w.Err[:300]
		}
		if err := s.Store.UpsertMarketStats(ctx, w); err != nil {
			slog.Error("market stats: store", "ticker", ct.Ticker, "err", err)
		}
		if w.ChartOK || w.CapOK {
			sum.Answered++
		}
		if w.Err != "" {
			sum.Failed++
			sum.Errors = append(sum.Errors, fmt.Sprintf("%s: %s", ct.Ticker, w.Err))
			slog.Warn("market stats: fetch", "ticker", ct.Ticker, "err", w.Err)
		}
		if gap > 0 {
			time.Sleep(gap)
		}
	}
	slog.Info("market stats pass", "tickers", sum.Total, "answered", sum.Answered, "failed", sum.Failed)
	return sum
}
