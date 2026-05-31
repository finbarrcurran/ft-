package server

import (
	"ft/internal/alert"
	"ft/internal/domain"
	"ft/internal/marketdata"
	"ft/internal/metrics"
	"ft/internal/sparkline"
	"ft/internal/store"
	"net/http"
	"strings"
	"time"
)

// scoreSummary is the shape attached to each holding/watchlist row so the
// frontend can render a Score column without a second round-trip. (Spec 4 D7.)
//
// v1.19B added Source + thesis-related fields. When Source == "thesis"
// the score has been overlaid from theses_index (Spec 15) — the locked
// thesis is the authoritative source, framework_scores fallback for
// tickers without a lock.
type scoreSummary struct {
	TotalScore  int    `json:"totalScore"`
	MaxScore    int    `json:"maxScore"`
	Passes      bool   `json:"passes"`
	FrameworkID string `json:"frameworkId"`
	ScoredAt    string `json:"scoredAt"` // ISO; client computes "120d ago" etc.
	StaleDays   int    `json:"staleDays"`
	Source      string `json:"source,omitempty"`     // "thesis" | "manual" — v1.19B
	Adapter     string `json:"adapter,omitempty"`    // thesis adapter slug — v1.19B
	SubType     string `json:"subType,omitempty"`    // thesis sub-type — v1.19B
	GitHubURL   string `json:"githubUrl,omitempty"`  // canonical thesis URL — v1.19B
	LockedDate  string `json:"lockedDate,omitempty"` // ISO YYYY-MM-DD — v1.19B
	// SC-10 — Band is set for crypto thesis scores (crypto_theses.band) so
	// the Crypto tab can render "13/18 — Accumulate". Empty for stocks.
	Band string `json:"band,omitempty"`
	// SC-02 — earnings_urgency carried through for the Summary freshness
	// nudge ('revision_needed' = earnings posted since the lock). Stocks only.
	EarningsUrgency string `json:"earningsUrgency,omitempty"`
}

// stockResp is the API shape returned by /api/holdings/stocks. The holding
// itself is embedded so the JSON has every StockHolding field at the top
// level, plus a `metrics` sub-object and an `alert` classification.
type stockResp struct {
	*domain.StockHolding
	Metrics         metrics.StockMetrics     `json:"metrics"`
	Alert           domain.AlertResult       `json:"alert"`
	SparklineSVG    string                   `json:"sparklineSvg"`     // raw inline SVG, "<span class=sparkline-empty>—</span>" when no data
	SparklineDir    string                   `json:"sparklineDir"`     // "up" | "down" | "flat" — drives row colour cues
	Sparkline30dPct float64                  `json:"sparkline30dPct"`  // for hover popover label
	SuggestedSLPct  float64                  `json:"suggestedSlPct"`   // negative; from risk_rules.go
	SuggestedTPPct  float64                  `json:"suggestedTpPct"`   // positive; from risk_rules.go
	Score           *scoreSummary            `json:"score,omitempty"`  // Spec 4 D7
	Market          *marketdata.MarketStatus `json:"market,omitempty"` // Spec 5 D3
}

type cryptoResp struct {
	*domain.CryptoHolding
	Metrics         metrics.CryptoMetrics `json:"metrics"`
	SparklineSVG    string                `json:"sparklineSvg"`
	SparklineDir    string                `json:"sparklineDir"`
	Sparkline30dPct float64               `json:"sparkline30dPct"`
	SuggestedSLPct  float64               `json:"suggestedSlPct"`
	SuggestedTPPct  float64               `json:"suggestedTpPct"`
	Score           *scoreSummary         `json:"score,omitempty"` // Spec 4 D7
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
	// v1.19B — overlay locked-thesis scores from theses_index. Lookup by
	// ticker (case-insensitive). Thesis wins over manual framework_scores
	// when both exist.
	thesisByTicker, _ := s.store.ThesisScoresByTicker(r.Context(), tickers)
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
		// v1.19A — Dist to SL now falls back to the suggested-derived SL
		// when no manual stop_loss is set. The "Proposed SL" cell on the
		// stocks tab shows this same effective price; this keeps the
		// Dist-to-SL column consistent with what the user sees.
		if m.DistanceToSLPct == nil && h.CurrentPrice != nil && h.AvgOpenPrice != nil &&
			*h.CurrentPrice > 0 && *h.AvgOpenPrice > 0 {
			effectiveSL := *h.AvgOpenPrice * (1 + slPct/100.0)
			if effectiveSL > 0 {
				v := ((*h.CurrentPrice - effectiveSL) / *h.CurrentPrice) * 100.0
				m.DistanceToSLPct = &v
			}
		}
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
		// v1.19B — pick thesis score over manual when available.
		var tkr string
		if h.Ticker != nil {
			tkr = *h.Ticker
		}
		score := pickScore(scoreByID[h.ID], thesisByTicker, tkr)
		out = append(out, stockResp{
			StockHolding:    h,
			Metrics:         m,
			Alert:           alert.ComputeWithMargin(h, m, margin),
			SparklineSVG:    sparkline.RenderDefault(series),
			SparklineDir:    sparkline.Direction(series),
			Sparkline30dPct: sparkline.ChangePct(series),
			SuggestedSLPct:  slPct,
			SuggestedTPPct:  tpPct,
			Score:           score,
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
	// SC-10 — crypto SCORE comes from crypto_theses (Spec 9k), keyed by
	// coin symbol. Previously this called ThesisScoresByTicker, which only
	// reads theses_index (stocks) and therefore always missed crypto
	// symbols → blank column. Now the locked crypto thesis wins over the
	// (empty) manual framework_scores, scale-aware via max_score.
	cryptoThesisBySymbol, _ := s.store.CryptoThesisScoresBySymbol(r.Context(), symbols)

	out := make([]cryptoResp, 0, len(holdings))
	for _, h := range holdings {
		series := closes[h.Symbol]
		slPct, tpPct := domain.SuggestCryptoRisk(h.VolTier)
		score := pickCryptoScore(scoreByID[h.ID], cryptoThesisBySymbol, h.Symbol)
		out = append(out, cryptoResp{
			CryptoHolding:   h,
			Metrics:         metrics.ComputeCrypto(h),
			SparklineSVG:    sparkline.RenderDefault(series),
			SparklineDir:    sparkline.Direction(series),
			Sparkline30dPct: sparkline.ChangePct(series),
			SuggestedSLPct:  slPct,
			SuggestedTPPct:  tpPct,
			Score:           score,
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
		Source:      "manual",
	}
}

// thesisScoreToSummary maps a store.ThesisScoreRow → scoreSummary with
// Source="thesis" so the frontend can distinguish lock-derived scores
// from manual framework_scores. v1.19B.
func thesisScoreToSummary(tr *store.ThesisScoreRow) *scoreSummary {
	if tr == nil {
		return nil
	}
	// Pass criterion mirrors the thesis library convention: score must
	// clear half the max (and is positive). PAAS 12/16 passes; WBA 4/16
	// does not. Tunable per-framework if needed later.
	passes := tr.Score > 0 && tr.MaxScore > 0 && (tr.Score*2 >= tr.MaxScore)
	// staleDays uses lockedDate when present; otherwise leave at 0 so
	// the UI doesn't render a stale-warning marker for un-dated locks.
	stale := 0
	if tr.LockedDate != "" {
		if t, err := time.Parse("2006-01-02", tr.LockedDate); err == nil {
			stale = int(time.Since(t).Hours() / 24)
		}
	}
	return &scoreSummary{
		TotalScore:  tr.Score,
		MaxScore:    tr.MaxScore,
		Passes:      passes,
		FrameworkID: tr.Adapter,
		ScoredAt:    tr.LockedDate, // already ISO YYYY-MM-DD
		StaleDays:   stale,
		Source:          "thesis",
		Adapter:         tr.Adapter,
		SubType:         tr.SubType,
		GitHubURL:       tr.GitHubURL,
		LockedDate:      tr.LockedDate,
		EarningsUrgency: tr.EarningsUrgency,
	}
}

// cryptoThesisScoreToSummary maps a store.CryptoThesisScoreRow → scoreSummary
// with Source="thesis" and Band populated so the Crypto tab renders
// "13/18 — Accumulate". Scale is carried via MaxScore (12 BTC / 18 alt). SC-10.
func cryptoThesisScoreToSummary(cr *store.CryptoThesisScoreRow) *scoreSummary {
	if cr == nil {
		return nil
	}
	// "Passes" drives the green/amber badge styling. For a crypto thesis we
	// treat Strong/Accumulate as a pass; Hold/Trim/Exit as not.
	passes := cr.Band == "strong" || cr.Band == "accumulate"
	stale := 0
	if cr.LockedDate != "" {
		if t, err := time.Parse("2006-01-02", cr.LockedDate); err == nil {
			stale = int(time.Since(t).Hours() / 24)
		}
	}
	return &scoreSummary{
		TotalScore:  cr.Score,
		MaxScore:    cr.MaxScore,
		Passes:      passes,
		FrameworkID: cr.ScorecardType,
		ScoredAt:    cr.LockedDate,
		StaleDays:   stale,
		Source:      "thesis",
		Adapter:     cr.ScorecardType,
		GitHubURL:   cr.GitHubURL,
		LockedDate:  cr.LockedDate,
		Band:        cr.Band,
	}
}

// pickScore — choose between thesis (preferred) and manual framework score.
// v1.19B. Returns nil if neither is present.
func pickScore(manual *domain.FrameworkScore, thesisByTicker map[string]*store.ThesisScoreRow, ticker string) *scoreSummary {
	if ticker != "" {
		if t, ok := thesisByTicker[strings.ToUpper(ticker)]; ok && t != nil {
			return thesisScoreToSummary(t)
		}
	}
	return toScoreSummary(manual)
}

// pickCryptoScore — crypto analogue of pickScore: locked crypto thesis
// (preferred) over manual framework score. SC-10.
func pickCryptoScore(manual *domain.FrameworkScore, cryptoThesisBySymbol map[string]*store.CryptoThesisScoreRow, symbol string) *scoreSummary {
	if symbol != "" {
		if t, ok := cryptoThesisBySymbol[strings.ToUpper(symbol)]; ok && t != nil {
			return cryptoThesisScoreToSummary(t)
		}
	}
	return toScoreSummary(manual)
}
