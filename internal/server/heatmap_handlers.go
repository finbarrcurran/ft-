package server

import (
	"ft/internal/domain"
	"ft/internal/heatmap"
	"net/http"
	"strconv"
)

// GET /api/heatmap.svg
//
// Query params:
//
//	w     width in px   (default 1100, min 320, max 3000)
//	h     height in px  (default 600,  min 300, max 1800)
//	mode  "market_cap" (default) | "my_holdings"  (Spec 6)
//	sector  optional GICS sector filter
//
// Mode behaviour:
//   * market_cap  — S&P 500 sample with live-quote overlay. Held tickers
//                   show the accent stripe.
//   * my_holdings — universe is the user's stock_holdings rows. Tile size
//                   is position value USD (qty × current price; falls back
//                   to invested$). Tile color is daily_change_pct. Sector
//                   grouping preserved.
//
// Response is a complete <svg>...</svg> document.
func (s *Server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	width := parsePxParam(r.URL.Query().Get("w"), 1100, 320, 3000)
	height := parsePxParam(r.URL.Query().Get("h"), 600, 300, 1800)
	sector := r.URL.Query().Get("sector")
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		// Fall back to the stored preference, else market_cap.
		if v, err := s.store.GetPreference(r.Context(), "heatmap_mode"); err == nil && v != "" {
			mode = v
		}
	}
	if mode != "my_holdings" {
		mode = "market_cap"
	}

	holdings, _ := s.store.ListStockHoldings(r.Context(), userID)

	// Derive held tickers from holdings — used for both modes.
	held := map[string]bool{}
	for _, h := range holdings {
		if h.Ticker != nil && *h.Ticker != "" {
			held[*h.Ticker] = true
		}
	}

	opts := heatmap.RenderOptions{
		Width:  width,
		Height: height,
		Held:   held,
		Sector: sector,
	}

	if mode == "my_holdings" {
		opts.Source = tilesFromHoldings(holdings)
	}

	svg := heatmap.Render(opts)

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// Small TTL — prices change with refresh, fine to refetch.
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write([]byte(svg))
}

// tilesFromHoldings builds one heatmap.MarketTile per stock holding for the
// my-holdings heatmap mode (Spec 6 D3). Size = position value USD; falls back
// to invested USD when current price isn't known.
func tilesFromHoldings(holdings []*domain.StockHolding) []heatmap.MarketTile {
	out := make([]heatmap.MarketTile, 0, len(holdings))
	for _, h := range holdings {
		if h.Ticker == nil || *h.Ticker == "" {
			continue
		}
		// Position value: prefer (qty × current price); fall back to invested$.
		// qty derived from invested/avgOpen so we don't store quantity directly.
		var value float64 = h.InvestedUSD
		if h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 && h.CurrentPrice != nil && *h.CurrentPrice > 0 {
			qty := h.InvestedUSD / *h.AvgOpenPrice
			v := qty * *h.CurrentPrice
			if v > 0 {
				value = v
			}
		}
		if value <= 0 {
			continue
		}
		var price float64
		if h.CurrentPrice != nil {
			price = *h.CurrentPrice
		}
		var change float64
		if h.DailyChangePct != nil {
			change = *h.DailyChangePct
		}
		sector := "Other"
		if h.Sector != nil && *h.Sector != "" {
			sector = *h.Sector
		}
		out = append(out, heatmap.MarketTile{
			Ticker:     *h.Ticker,
			Name:       h.Name,
			Sector:     sector,
			MarketCapB: value, // sizing weight; units don't matter (relative)
			Price:      price,
			ChangePct:  change,
			VolumeM:    0,
			Held:       true,
		})
	}
	return out
}

func parsePxParam(raw string, def, min, max float64) float64 {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
