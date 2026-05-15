package heatmap

import "sync"

// liveQuote is a per-ticker runtime override on top of the seeded Tiles data.
// Set by the refresh service; read by Render. Mutex-protected for safe
// concurrent access between the background refresh goroutine and HTTP handlers.
type liveQuote struct {
	Price     float64
	ChangePct float64
}

var (
	liveMu sync.RWMutex
	live   = map[string]liveQuote{}
)

// SetLiveQuote records the latest price + daily change % for a ticker. Called
// by refresh when a quote comes back from any provider — both for tickers that
// are in the user's portfolio AND for the heatmap-only set.
func SetLiveQuote(ticker string, price, changePct float64) {
	if ticker == "" || price <= 0 {
		return
	}
	liveMu.Lock()
	live[ticker] = liveQuote{Price: price, ChangePct: changePct}
	liveMu.Unlock()
}

// applyLive returns a fresh copy of the seed Tiles with any matching live
// quotes overlaid. Safe to call from any goroutine.
func applyLive() []MarketTile {
	liveMu.RLock()
	defer liveMu.RUnlock()
	out := make([]MarketTile, len(Tiles))
	for i, t := range Tiles {
		if q, ok := live[t.Ticker]; ok {
			t.Price = q.Price
			t.ChangePct = q.ChangePct
		}
		out[i] = t
	}
	return out
}

// LiveCount returns how many tickers currently have an override. Useful for
// diagnostics / dashboard chips.
func LiveCount() int {
	liveMu.RLock()
	defer liveMu.RUnlock()
	return len(live)
}

// AllTickers returns the list of all heatmap-dataset tickers, suitable for
// the refresh service to batch-fetch quotes for.
func AllTickers() []string {
	out := make([]string, 0, len(Tiles))
	for _, t := range Tiles {
		out = append(out, t.Ticker)
	}
	return out
}
