// Package server — bot-facing handlers.
//
// Endpoints under /api/bot/* are designed for the OpenClaw skill that runs on
// the same box and pushes Telegram notifications. They all also accept session
// cookies, so they're curl-friendly for humans during development.
package server

import (
	"fmt"
	"ft/internal/alert"
	"ft/internal/domain"
	"ft/internal/metrics"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// GET /api/bot/alerts
//
// Returns RED + AMBER alerts for the user that have NOT been ACKed today.
// Each alert carries the holding context (ticker, name, price) so the bot's
// message can be self-contained.
//
// Response:
//
//	{
//	  "asOf": "2026-05-14T18:25:53Z",
//	  "alerts": [
//	    {
//	      "holdingKind": "stock", "holdingId": 12,
//	      "ticker": "NVDA", "name": "NVIDIA Corporation",
//	      "kind": "red",
//	      "triggers": ["RSI 78 (≥ 75, overbought)"],
//	      "currentPrice": 235.09,
//	      "distanceToSlPct": 17.06
//	    }
//	  ],
//	  "count": 1
//	}
//
// Query params:
//
//	only_unnotified=0|1   default 1. When 0, returns every active alert
//	                      regardless of notification_log state. The skill's
//	                      natural-language path uses this so the user can ask
//	                      "what alerts do I have?" any time and get a fresh
//	                      answer.
func (s *Server) handleBotAlerts(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	onlyUnnotified := r.URL.Query().Get("only_unnotified") != "0"
	today := time.Now().UTC().Format("2006-01-02")

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	type alertOut struct {
		HoldingKind     string   `json:"holdingKind"`
		HoldingID       int64    `json:"holdingId"`
		Ticker          *string  `json:"ticker,omitempty"`
		Name            string   `json:"name"`
		Kind            string   `json:"kind"`
		Triggers        []string `json:"triggers"`
		CurrentPrice    *float64 `json:"currentPrice,omitempty"`
		DistanceToSLPct *float64 `json:"distanceToSlPct,omitempty"`
	}
	out := []alertOut{}
	margin := s.currentAlertMargin(r.Context()) // Spec 9b D6

	for _, h := range stocks {
		m := metrics.ComputeStock(h)
		ar := alert.ComputeWithMargin(h, m, margin)
		if ar.Status != domain.AlertRed && ar.Status != domain.AlertAmber {
			continue
		}
		if onlyUnnotified {
			acked, err := s.store.HasAlertBeenAckedToday(r.Context(), "stock", h.ID, string(ar.Status), today)
			if err != nil {
				mapStoreError(w, err)
				return
			}
			if acked {
				continue
			}
		}
		out = append(out, alertOut{
			HoldingKind:     "stock",
			HoldingID:       h.ID,
			Ticker:          h.Ticker,
			Name:            h.Name,
			Kind:            string(ar.Status),
			Triggers:        ar.Triggers,
			CurrentPrice:    h.CurrentPrice,
			DistanceToSLPct: m.DistanceToSLPct,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"asOf":   time.Now().UTC().Format(time.RFC3339),
		"alerts": out,
		"count":  len(out),
	})
}

// POST /api/bot/alerts/ack
//
// Body: { "holdingKind": "stock", "holdingId": 12, "kind": "red" }
// Optional: { ..., "day": "2026-05-14" } to override (defaults to today).
//
// Writes a row to notification_log. Idempotent via the UNIQUE constraint;
// re-ACKing the same alert on the same day refreshes acked_at.
func (s *Server) handleBotAlertsAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HoldingKind string `json:"holdingKind"`
		HoldingID   int64  `json:"holdingId"`
		Kind        string `json:"kind"`
		Day         string `json:"day"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.HoldingKind == "" || req.HoldingID == 0 || req.Kind == "" {
		writeError(w, http.StatusBadRequest, "holdingKind, holdingId, kind are required")
		return
	}
	day := req.Day
	if day == "" {
		day = time.Now().UTC().Format("2006-01-02")
	}
	if err := s.store.AckAlert(r.Context(), req.HoldingKind, req.HoldingID, req.Kind, day); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acked": true})
}

// GET /api/bot/holdings/summary
//
// One-shot rollup used by the "what's my portfolio doing?" Telegram path.
// All fields are nullable when their inputs are missing.
func (s *Server) handleBotSummary(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	cryptos, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	// Stock totals + alert tally.
	stockTotals := totalsForStocks(stocks)
	alertTally := map[string]int{"red": 0, "amber": 0, "green": 0, "neutral": 0}
	margin := s.currentAlertMargin(r.Context()) // Spec 9b D6
	for _, h := range stocks {
		m := metrics.ComputeStock(h)
		ar := alert.ComputeWithMargin(h, m, margin)
		alertTally[string(ar.Status)]++
	}

	// Crypto totals.
	cryptoTotals := totalsForCrypto(cryptos)

	// Last refresh time from meta.
	lastRefreshed := ""
	if v, err := s.store.GetMeta(r.Context(), "last_refreshed_at"); err == nil && v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			lastRefreshed = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"asOf":            time.Now().UTC().Format(time.RFC3339),
		"lastRefreshedAt": lastRefreshed,
		"stocks":          stockTotals,
		"crypto":          cryptoTotals,
		"alerts":          alertTally,
	})
}

// GET /api/bot/holdings/movers?limit=5
//
// Top + bottom by today's percent change. Stocks and crypto in separate
// buckets so the message can read them clearly. Default limit 5; max 10.
func (s *Server) handleBotMovers(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	limit := 5
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 10 {
		limit = 10
	}

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	cryptos, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	type moverRow struct {
		Kind           string   `json:"kind"`
		ID             int64    `json:"id"`
		Symbol         string   `json:"symbol"`
		Name           string   `json:"name"`
		ChangePct      float64  `json:"changePct"`
		CurrentPrice   *float64 `json:"currentPrice,omitempty"`
	}

	// Stocks — by daily_change_pct.
	stockRows := []moverRow{}
	for _, h := range stocks {
		if h.DailyChangePct == nil {
			continue
		}
		sym := ""
		if h.Ticker != nil {
			sym = *h.Ticker
		}
		stockRows = append(stockRows, moverRow{
			Kind:         "stock",
			ID:           h.ID,
			Symbol:       sym,
			Name:         h.Name,
			ChangePct:    *h.DailyChangePct,
			CurrentPrice: h.CurrentPrice,
		})
	}
	sort.Slice(stockRows, func(i, j int) bool { return stockRows[i].ChangePct > stockRows[j].ChangePct })

	// Crypto — by daily_change_pct (24h change).
	cryptoRows := []moverRow{}
	for _, h := range cryptos {
		if h.DailyChangePct == nil {
			continue
		}
		cryptoRows = append(cryptoRows, moverRow{
			Kind:         "crypto",
			ID:           h.ID,
			Symbol:       h.Symbol,
			Name:         h.Name,
			ChangePct:    *h.DailyChangePct,
			CurrentPrice: h.CurrentPriceUSD,
		})
	}
	sort.Slice(cryptoRows, func(i, j int) bool { return cryptoRows[i].ChangePct > cryptoRows[j].ChangePct })

	bestStocks, worstStocks := head(stockRows, limit), tail(stockRows, limit)
	bestCrypto, worstCrypto := head(cryptoRows, limit), tail(cryptoRows, limit)

	writeJSON(w, http.StatusOK, map[string]any{
		"asOf": time.Now().UTC().Format(time.RFC3339),
		"stocks": map[string]any{
			"best":  bestStocks,
			"worst": worstStocks,
		},
		"crypto": map[string]any{
			"best":  bestCrypto,
			"worst": worstCrypto,
		},
	})
}

// ----- internal totals helpers --------------------------------------------

type stockTotals struct {
	Count       int      `json:"count"`
	InvestedUSD float64  `json:"investedUsd"`
	ValueUSD    *float64 `json:"valueUsd,omitempty"`
	PNLUSD      *float64 `json:"pnlUsd,omitempty"`
	PNLPct      *float64 `json:"pnlPct,omitempty"`
}

func totalsForStocks(stocks []*domain.StockHolding) stockTotals {
	var t stockTotals
	t.Count = len(stocks)
	var totalValue float64
	var anyValued bool
	for _, h := range stocks {
		t.InvestedUSD += h.InvestedUSD
		m := metrics.ComputeStock(h)
		if m.CurrentValueUSD != nil {
			totalValue += *m.CurrentValueUSD
			anyValued = true
		}
	}
	if anyValued {
		v := totalValue
		t.ValueUSD = &v
		p := totalValue - t.InvestedUSD
		t.PNLUSD = &p
		if t.InvestedUSD > 0 {
			pct := (p / t.InvestedUSD) * 100
			t.PNLPct = &pct
		}
	}
	return t
}

type cryptoTotals struct {
	Count        int      `json:"count"`
	CostBasisUSD float64  `json:"costBasisUsd"`
	ValueUSD     *float64 `json:"valueUsd,omitempty"`
	PNLUSD       *float64 `json:"pnlUsd,omitempty"`
	PNLPct       *float64 `json:"pnlPct,omitempty"`
}

func totalsForCrypto(cs []*domain.CryptoHolding) cryptoTotals {
	var t cryptoTotals
	t.Count = len(cs)
	var totalValue float64
	var anyValued bool
	for _, c := range cs {
		if c.CostBasisUSD != nil {
			t.CostBasisUSD += *c.CostBasisUSD
		}
		m := metrics.ComputeCrypto(c)
		if m.CurrentValueUSD != nil {
			totalValue += *m.CurrentValueUSD
			anyValued = true
		}
	}
	if anyValued {
		v := totalValue
		t.ValueUSD = &v
		p := totalValue - t.CostBasisUSD
		t.PNLUSD = &p
		if t.CostBasisUSD > 0 {
			pct := (p / t.CostBasisUSD) * 100
			t.PNLPct = &pct
		}
	}
	return t
}

func head[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}
func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		// reverse so most-negative first
		out := make([]T, len(xs))
		for i, x := range xs {
			out[len(xs)-1-i] = x
		}
		return out
	}
	// take last n and reverse
	src := xs[len(xs)-n:]
	out := make([]T, n)
	for i, x := range src {
		out[n-1-i] = x
	}
	return out
}

// avoid "imported and not used" if these end up unreferenced during build:
var _ = fmt.Sprintf
