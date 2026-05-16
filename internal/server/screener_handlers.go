// Spec 9b D9 — Screener endpoint.
//
// Reuses the existing heatmap S&P 500 sample dataset (with live-quote
// overlay) so no new market-data calls are needed.
//
// GET /api/screener?sectors=&beta_min=&beta_max=&mcap_min=&mcap_max=&change_min=&change_max=&held=
//
// All filters optional. `held` ∈ "" | "hide" | "show" | "only":
//   "" or "show" — include everything (default)
//   "hide"       — exclude tickers in user's stock_holdings
//   "only"       — only tickers in user's stock_holdings

package server

import (
	"ft/internal/heatmap"
	"net/http"
	"strconv"
	"strings"
)

type screenerRow struct {
	Ticker     string  `json:"ticker"`
	Name       string  `json:"name"`
	Sector     string  `json:"sector"`
	Price      float64 `json:"price"`
	ChangePct  float64 `json:"changePct"`
	MarketCapB float64 `json:"marketCapB"`
	Beta       float64 `json:"beta,omitempty"` // not in seed; reserved for future
	Held       bool    `json:"held"`
	OnWatchlist bool   `json:"onWatchlist"`
}

func (s *Server) handleScreener(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	q := r.URL.Query()

	sectorsFilter := commaSet(q.Get("sectors"))
	mcapMin := parseFloatDefault(q.Get("mcap_min"), 0)
	mcapMax := parseFloatDefault(q.Get("mcap_max"), 0) // 0 = no upper bound
	changeMin := parseFloatDefault(q.Get("change_min"), -1000)
	changeMax := parseFloatDefault(q.Get("change_max"), 1000)
	heldMode := strings.ToLower(strings.TrimSpace(q.Get("held")))

	// Resolve user's holdings + watchlist ticker sets.
	held := map[string]bool{}
	if holdings, err := s.store.ListStockHoldings(r.Context(), userID); err == nil {
		for _, h := range holdings {
			if h.Ticker != nil && *h.Ticker != "" {
				held[strings.ToUpper(*h.Ticker)] = true
			}
		}
	}
	onWatch := map[string]bool{}
	if wl, err := s.store.ListWatchlist(r.Context(), userID); err == nil {
		for _, e := range wl {
			if e.Kind == "stock" {
				onWatch[strings.ToUpper(e.Ticker)] = true
			}
		}
	}

	tiles := heatmap.AllTilesWithLive()
	out := make([]screenerRow, 0, len(tiles))
	for _, t := range tiles {
		tickerU := strings.ToUpper(t.Ticker)
		isHeld := held[tickerU]
		switch heldMode {
		case "hide":
			if isHeld {
				continue
			}
		case "only":
			if !isHeld {
				continue
			}
		}
		if len(sectorsFilter) > 0 && !sectorsFilter[t.Sector] {
			continue
		}
		if mcapMin > 0 && t.MarketCapB < mcapMin {
			continue
		}
		if mcapMax > 0 && t.MarketCapB > mcapMax {
			continue
		}
		if t.ChangePct < changeMin || t.ChangePct > changeMax {
			continue
		}
		out = append(out, screenerRow{
			Ticker:      t.Ticker,
			Name:        t.Name,
			Sector:      t.Sector,
			Price:       t.Price,
			ChangePct:   t.ChangePct,
			MarketCapB:  t.MarketCapB,
			Held:        isHeld,
			OnWatchlist: onWatch[tickerU],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":    out,
		"total":   len(tiles),
		"matched": len(out),
	})
}

// commaSet builds a set from a comma-separated query string. Empty input → empty set.
func commaSet(raw string) map[string]bool {
	out := map[string]bool{}
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func parseFloatDefault(raw string, def float64) float64 {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	return v
}
