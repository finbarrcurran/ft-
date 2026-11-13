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
	"ft/internal/domain"
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
	// FT-091: where the cap comes from. "yahoo" = the daily stats pass (marketStats), with its as-of; "sample" = the
	// seeded S&P sample figure, approximate (round numbers, not refreshed).
	MarketCapSource string `json:"marketCapSource"`
	MarketCapApprox bool   `json:"marketCapApprox"`
	MarketCapAsOf   *int64 `json:"marketCapAsOf,omitempty"`
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

	// Resolve user's holdings + watchlist ticker sets. Demo mode (SC-22 doctrine: portfolio data is
	// never served in demo mode, enforced server-side) skips both reads: no row is flagged held or
	// on the watchlist, and the held=only/hide filter then sees an empty book.
	held := map[string]bool{}
	onWatch := map[string]bool{}
	if !s.demoModeOn(r.Context()) {
		if holdings, err := s.store.ListStockHoldings(r.Context(), userID); err == nil {
			for _, h := range holdings {
				if h.Ticker != nil && *h.Ticker != "" {
					held[strings.ToUpper(*h.Ticker)] = true
				}
			}
		}
		if wl, err := s.store.ListWatchlist(r.Context(), userID); err == nil {
			for _, e := range wl {
				if e.Kind == "stock" {
					onWatch[strings.ToUpper(e.Ticker)] = true
				}
			}
		}
	}

	// FT-091: real market caps where the daily stats pass has them (watchlist tickers). Skipped in demo mode, where
	// watchlist membership must not show. Everything else keeps the sample figure, labelled approximate.
	var stats map[string]*domain.MarketStatsInfo
	if !s.demoModeOn(r.Context()) {
		stats = s.store.MarketStatsMap(r.Context())
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
		capB, capSrc, capApprox := t.MarketCapB, "sample", true
		var capAsOf *int64
		if st, ok := stats[tickerU]; ok && st != nil && st.MarketCap.Value != nil && *st.MarketCap.Value > 0 {
			capB, capSrc, capApprox, capAsOf = *st.MarketCap.Value/1e9, "yahoo", false, st.MarketCap.AsOf
		}
		if mcapMin > 0 && capB < mcapMin {
			continue
		}
		if mcapMax > 0 && capB > mcapMax {
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
			MarketCapB:  capB,
			MarketCapSource: capSrc,
			MarketCapApprox: capApprox,
			MarketCapAsOf:   capAsOf,
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
