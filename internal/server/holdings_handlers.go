package server

import (
	"ft/internal/alert"
	"ft/internal/domain"
	"ft/internal/metrics"
	"ft/internal/sparkline"
	"net/http"
)

// stockResp is the API shape returned by /api/holdings/stocks. The holding
// itself is embedded so the JSON has every StockHolding field at the top
// level, plus a `metrics` sub-object and an `alert` classification.
type stockResp struct {
	*domain.StockHolding
	Metrics       metrics.StockMetrics `json:"metrics"`
	Alert         domain.AlertResult   `json:"alert"`
	SparklineSVG  string               `json:"sparklineSvg"`  // raw inline SVG, "<span class=sparkline-empty>—</span>" when no data
	SparklineDir  string               `json:"sparklineDir"`  // "up" | "down" | "flat" — drives row colour cues
	Sparkline30dPct float64            `json:"sparkline30dPct"` // for hover popover label
	SuggestedSLPct float64             `json:"suggestedSlPct"` // negative; from risk_rules.go
	SuggestedTPPct float64             `json:"suggestedTpPct"` // positive; from risk_rules.go
}

type cryptoResp struct {
	*domain.CryptoHolding
	Metrics       metrics.CryptoMetrics `json:"metrics"`
	SparklineSVG  string                `json:"sparklineSvg"`
	SparklineDir  string                `json:"sparklineDir"`
	Sparkline30dPct float64             `json:"sparkline30dPct"`
	SuggestedSLPct float64              `json:"suggestedSlPct"`
	SuggestedTPPct float64              `json:"suggestedTpPct"`
}

// GET /api/holdings/stocks
func (s *Server) handleListStocks(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	holdings, err := s.store.ListStockHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}

	// Batch-fetch sparkline closes for all tickers in one query.
	tickers := make([]string, 0, len(holdings))
	for _, h := range holdings {
		if h.Ticker != nil && *h.Ticker != "" {
			tickers = append(tickers, *h.Ticker)
		}
	}
	closes, _ := s.store.GetAllSparklineCloses(r.Context(), "stock", tickers, 30)

	out := make([]stockResp, 0, len(holdings))
	for _, h := range holdings {
		m := metrics.ComputeStock(h)
		var series []float64
		if h.Ticker != nil {
			series = closes[*h.Ticker]
		}
		slPct, tpPct := domain.SuggestStockRisk(h.Beta)
		out = append(out, stockResp{
			StockHolding:    h,
			Metrics:         m,
			Alert:           alert.Compute(h, m),
			SparklineSVG:    sparkline.RenderDefault(series),
			SparklineDir:    sparkline.Direction(series),
			Sparkline30dPct: sparkline.ChangePct(series),
			SuggestedSLPct:  slPct,
			SuggestedTPPct:  tpPct,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out})
}

// GET /api/holdings/crypto
func (s *Server) handleListCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	holdings, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}

	symbols := make([]string, 0, len(holdings))
	for _, h := range holdings {
		symbols = append(symbols, h.Symbol)
	}
	closes, _ := s.store.GetAllSparklineCloses(r.Context(), "crypto", symbols, 30)

	out := make([]cryptoResp, 0, len(holdings))
	for _, h := range holdings {
		series := closes[h.Symbol]
		slPct, tpPct := domain.SuggestCryptoRisk(h.VolTier)
		out = append(out, cryptoResp{
			CryptoHolding:   h,
			Metrics:         metrics.ComputeCrypto(h),
			SparklineSVG:    sparkline.RenderDefault(series),
			SparklineDir:    sparkline.Direction(series),
			Sparkline30dPct: sparkline.ChangePct(series),
			SuggestedSLPct:  slPct,
			SuggestedTPPct:  tpPct,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out})
}
