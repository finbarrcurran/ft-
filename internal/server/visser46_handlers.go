package server

import (
	"context"
	"ft/internal/store"
	"ft/internal/visser46"
	"net/http"
	"time"
)

// GET /api/crypto-index/visser46/breadth — Jordi-46 Part B: breadth per vertical and total, computed from
// stored closes, with the latest /coins/markets snapshot per member and the Part B switch state. Makes no
// CoinGecko call. Reference/market data only (no holdings, watchlist or broker data), so demo mode has nothing to mask.
func (s *Server) handleCryptoIndexBreadth(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("key") != visser46.IndexKey {
		writeError(w, http.StatusNotFound, "unknown index")
		return
	}
	svc := visser46.New(s.store)
	b, err := svc.Breadth(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, _ := s.store.ListCryptoIndexMembers(r.Context(), visser46.IndexKey)
	snaps, _ := s.store.ListIndexSnapshots(r.Context())
	type row struct {
		Symbol   string               `json:"symbol"`
		Vertical string               `json:"vertical"`
		Snapshot *store.IndexSnapshot `json:"snapshot,omitempty"`
	}
	rows := make([]row, 0, len(members))
	for _, m := range members {
		rw := row{Symbol: m.Symbol, Vertical: m.Vertical}
		if m.CoinGeckoID != nil {
			if sn, ok := snaps[*m.CoinGeckoID]; ok {
				sn := sn
				rw.Snapshot = &sn
			}
		}
		rows = append(rows, rw)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"breadth": b, "members": rows,
		"partB":           map[string]any{"enabled": s.store.PartBEnabled(r.Context())},
		"publishedFigure": "Not stored. Jordi's published breadth figure is a check only: compare by eye; FT never uses it.",
	})
}

// coinGeckoUsage is the Diagnostics block for CoinGecko (Jordi-46 Part B): the monthly keyed counter against
// the soft stop and the 10,000 limit, calls per endpoint and feature this month and today, the Part B switch,
// and the projected month-end keyed total at the pace so far.
func (s *Server) coinGeckoUsage(ctx context.Context) map[string]any {
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	monthKeyed, _ := s.store.CoinGeckoMonthKeyed(ctx, now)
	perMonth, _ := s.store.CoinGeckoEndpointUsage(ctx, first.Format("2006-01-02"), next.Format("2006-01-02"))
	today := now.Format("2006-01-02")
	perToday, _ := s.store.CoinGeckoEndpointUsage(ctx, today, now.AddDate(0, 0, 1).Format("2006-01-02"))
	projected := 0.0 // too early in the month to project during its first 24 hours
	if since := now.Sub(first); since >= 24*time.Hour {
		projected = float64(monthKeyed) * float64(next.Sub(first)) / float64(since)
	}
	return map[string]any{
		"month":               first.Format("2006-01"),
		"partBEnabled":        s.store.PartBEnabled(ctx),
		"monthKeyed":          monthKeyed,
		"softStop":            s.store.PartBSoftStop(ctx),
		"monthlyLimit":        10000,
		"projectedMonthKeyed": int(projected + 0.5),
		"projectionNote":      "month-to-date keyed calls scaled to the whole month; not shown during the first 24 hours of a month. Part B adds about 1,440 a month (48 a day) plus at most 42 one-off backfill calls.",
		"month_endpoints":     perMonth,
		"today_endpoints":     perToday,
	}
}
