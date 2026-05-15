package server

import (
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
//
// The user's held tickers are derived from their stock_holdings rows and
// flagged on matching heatmap tiles (drawn with an accent stripe).
//
// Response is a complete <svg>...</svg> document. We deliberately don't return
// JSON — the SVG is the payload. The frontend just innerHTML's it.
func (s *Server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	width := parsePxParam(r.URL.Query().Get("w"), 1100, 320, 3000)
	height := parsePxParam(r.URL.Query().Get("h"), 600, 300, 1800)

	// Derive held tickers from the user's stock holdings.
	held := map[string]bool{}
	if holdings, err := s.store.ListStockHoldings(r.Context(), userID); err == nil {
		for _, h := range holdings {
			if h.Ticker != nil && *h.Ticker != "" {
				held[*h.Ticker] = true
			}
		}
	}

	sector := r.URL.Query().Get("sector")
	svg := heatmap.Render(heatmap.RenderOptions{
		Width:  width,
		Height: height,
		Held:   held,
		Sector: sector,
	})

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	// Small TTL — held flags change with holdings, fine to refetch.
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write([]byte(svg))
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
