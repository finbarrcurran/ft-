package server

import (
	"ft/internal/alert"
	"ft/internal/domain"
	"ft/internal/marketdata"
	"ft/internal/metrics"
	"ft/internal/sparkline"
	"net/http"
	"time"
)

// scoreSummary is the shape attached to each holding/watchlist row so the
// frontend can render a Score column without a second round-trip. (Spec 4 D7.)
type scoreSummary struct {
	TotalScore  int    `json:"totalScore"`
	MaxScore    int    `json:"maxScore"`
	Passes      bool   `json:"passes"`
	FrameworkID string `json:"frameworkId"`
	ScoredAt    string `json:"scoredAt"` // ISO; client computes "120d ago" etc.
	StaleDays   int    `json:"staleDays"`
}

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
	Score         *scoreSummary        `json:"score,omitempty"` // Spec 4 D7
	Market        *marketdata.MarketStatus `json:"market,omitempty"` // Spec 5 D3
}

type cryptoResp struct {
	*domain.CryptoHolding
	Metrics       metrics.CryptoMetrics `json:"metrics"`
	SparklineSVG  string                `json:"sparklineSvg"`
	SparklineDir  string                `json:"sparklineDir"`
	Sparkline30dPct float64             `json:"sparkline30dPct"`
	SuggestedSLPct float64              `json:"suggestedSlPct"`
	SuggestedTPPct float64              `json:"suggestedTpPct"`
	Score         *scoreSummary         `json:"score,omitempty"` // Spec 4 D7
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
	ids := make([]int64, 0, len(holdings))
	for _, h := range holdings {
		ids = append(ids, h.ID)
		if h.Ticker != nil && *h.Ticker != "" {
			tickers = append(tickers, *h.Ticker)
		}
	}
	closes, _ := s.store.GetAllSparklineCloses(r.Context(), "stock", tickers, 30)
	scoreByID, _ := s.store.LatestFrameworkScoresMany(r.Context(), userID, "holding", ids)
	now := time.Now().UTC()
	margin := s.currentAlertMargin(r.Context()) // Spec 9b D6 — regime-scaled

	out := make([]stockResp, 0, len(holdings))
	for _, h := range holdings {
		m := metrics.ComputeStock(h)
		var series []float64
		if h.Ticker != nil {
			series = closes[*h.Ticker]
		}
		slPct, tpPct := domain.SuggestStockRisk(h.Beta)
		// Resolve exchange: explicit override wins, else suffix rule.
		var exch string
		if h.ExchangeOverride != nil && *h.ExchangeOverride != "" {
			exch = *h.ExchangeOverride
		} else if h.Ticker != nil {
			exch = marketdata.ExchangeForTicker(*h.Ticker)
		}
		var marketPtr *marketdata.MarketStatus
		if exch != "" {
			st := marketdata.Status(exch, now)
			if st.Exchange != "" {
				marketPtr = &st
			}
		}
		out = append(out, stockResp{
			StockHolding:    h,
			Metrics:         m,
			Alert:           alert.ComputeWithMargin(h, m, margin),
			SparklineSVG:    sparkline.RenderDefault(series),
			SparklineDir:    sparkline.Direction(series),
			Sparkline30dPct: sparkline.ChangePct(series),
			SuggestedSLPct:  slPct,
			SuggestedTPPct:  tpPct,
			Score:           toScoreSummary(scoreByID[h.ID]),
			Market:          marketPtr,
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
	ids := make([]int64, 0, len(holdings))
	for _, h := range holdings {
		symbols = append(symbols, h.Symbol)
		ids = append(ids, h.ID)
	}
	closes, _ := s.store.GetAllSparklineCloses(r.Context(), "crypto", symbols, 30)
	scoreByID, _ := s.store.LatestFrameworkScoresMany(r.Context(), userID, "holding", ids)

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
			Score:           toScoreSummary(scoreByID[h.ID]),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out})
}

// toScoreSummary maps a domain.FrameworkScore → the compact response shape.
// nil-in nil-out so the JSON cleanly omits "score" when never scored.
func toScoreSummary(fs *domain.FrameworkScore) *scoreSummary {
	if fs == nil {
		return nil
	}
	stale := int(time.Since(fs.ScoredAt).Hours() / 24)
	return &scoreSummary{
		TotalScore:  fs.TotalScore,
		MaxScore:    fs.MaxScore,
		Passes:      fs.Passes,
		FrameworkID: fs.FrameworkID,
		ScoredAt:    fs.ScoredAt.Format(time.RFC3339),
		StaleDays:   stale,
	}
}
