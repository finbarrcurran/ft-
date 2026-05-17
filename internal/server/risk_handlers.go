// Spec 9c D12/D13/D16 — Portfolio Risk Dashboard endpoints.
//
// GET /api/risk/dashboard      consolidated snapshot (cookie OR token)
// POST /api/risk/snapshot      run nightly snapshot (cron, also exposed for
//                              manual recompute via Settings button)
//
// Risk-cap settings live in user_preferences (Spec 6) under keys
// `risk_concentration_cap_pct`, `risk_theme_concentration_cap_pct`,
// `risk_total_active_cap_pct`, `risk_drawdown_circuit_breaker_pct`,
// `risk_per_trade_default_pct`, `risk_per_trade_max_pct`. Plus the
// `risk_circuit_breaker_active` / `risk_circuit_breaker_until` toggle.

package server

import (
	"context"
	"ft/internal/domain"
	"ft/internal/metrics"
	"ft/internal/technicals"
	"net/http"
	"strconv"
	"time"
)

// GET /api/risk/dashboard
func (s *Server) handleRiskDashboard(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	check, err := s.computeRiskCheck(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, check)
}

// POST /api/risk/snapshot — writes one portfolio_value_history row for today
// + checks the circuit-breaker trigger. Idempotent on date.
func (s *Server) handleRiskSnapshot(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	check, err := s.computeRiskCheck(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	date := time.Now().UTC().Format("2006-01-02")
	if err := s.store.UpsertPortfolioValue(r.Context(), date,
		check.PortfolioValue,
		check.stocksUSD,
		check.cryptoUSD,
	); err != nil {
		mapStoreError(w, err)
		return
	}
	// Circuit-breaker auto-arm: drawdown <= -10% AND breaker not yet active.
	if check.DrawdownPct <= -check.Caps.DrawdownCircuitPct && !check.CircuitBreakerActive {
		_ = s.store.SetPreference(r.Context(), "risk_circuit_breaker_active", "true")
		until := time.Now().UTC().AddDate(0, 0, 14).Format("2006-01-02")
		_ = s.store.SetPreference(r.Context(), "risk_circuit_breaker_until", until)
		check.CircuitBreakerActive = true
		check.CircuitBreakerUntil = until
		check.Warnings = append(check.Warnings, "circuit breaker armed (drawdown >= cap)")
	}
	// Circuit-breaker auto-clear: drawdown < 5% AND until has passed.
	if check.CircuitBreakerActive && check.DrawdownPct > -5 && check.CircuitBreakerUntil != "" {
		untilT, err := time.Parse("2006-01-02", check.CircuitBreakerUntil)
		if err == nil && time.Now().UTC().After(untilT) {
			_ = s.store.SetPreference(r.Context(), "risk_circuit_breaker_active", "false")
			_ = s.store.SetPreference(r.Context(), "risk_circuit_breaker_until", "")
			check.CircuitBreakerActive = false
			check.CircuitBreakerUntil = ""
			check.Warnings = append(check.Warnings, "circuit breaker auto-cleared")
		}
	}
	writeJSON(w, http.StatusOK, check)
}

// dashboardCheck wraps PortfolioRiskCheck with private snapshot fields the
// handler needs but the JSON-out type doesn't need to expose.
type dashboardCheck struct {
	technicals.PortfolioRiskCheck
	stocksUSD float64
	cryptoUSD float64
}

func (s *Server) computeRiskCheck(ctx context.Context, userID int64) (*dashboardCheck, error) {
	stocks, err := s.store.ListStockHoldings(ctx, userID)
	if err != nil {
		return nil, err
	}
	cryptos, err := s.store.ListCryptoHoldings(ctx, userID)
	if err != nil {
		return nil, err
	}

	caps := s.loadRiskCaps(ctx)

	// Build OpenPosition list. Position size = current_value; risk = (entry-stop)×qty.
	var stockUSD, cryptoUSD float64
	positions := make([]technicals.OpenPosition, 0, len(stocks)+len(cryptos))
	for _, h := range stocks {
		m := metrics.ComputeStock(h)
		if m.CurrentValueUSD != nil {
			stockUSD += *m.CurrentValueUSD
		}
		positions = append(positions, openPositionFromStock(h, m))
	}
	for _, h := range cryptos {
		m := metrics.ComputeCrypto(h)
		if m.CurrentValueUSD != nil {
			cryptoUSD += *m.CurrentValueUSD
		}
		positions = append(positions, openPositionFromCrypto(h, m))
	}
	portfolioValue := stockUSD + cryptoUSD

	// Drawdown — pull last 365 daily snapshots.
	history, _ := s.store.GetPortfolioValueHistory(ctx, 365)
	values := make([]float64, 0, len(history)+1)
	for _, p := range history {
		values = append(values, p.Total)
	}
	values = append(values, portfolioValue) // include current
	drawdownPct, _ := technicals.ComputeDrawdownPct(values)

	// Circuit-breaker state from prefs.
	cbActive, _ := s.store.GetPreference(ctx, "risk_circuit_breaker_active")
	cbUntil, _ := s.store.GetPreference(ctx, "risk_circuit_breaker_until")

	check := technicals.Compute(positions, portfolioValue, drawdownPct, caps, cbActive == "true", cbUntil)
	return &dashboardCheck{
		PortfolioRiskCheck: check,
		stocksUSD:          stockUSD,
		cryptoUSD:          cryptoUSD,
	}, nil
}

func (s *Server) loadRiskCaps(ctx context.Context) technicals.RiskCaps {
	get := func(k string, def float64) float64 {
		v, err := s.store.GetPreference(ctx, k)
		if err != nil || v == "" {
			return def
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return def
		}
		return f
	}
	return technicals.RiskCaps{
		ConcentrationPct:      get("risk_concentration_cap_pct", 15),
		ThemeConcentrationPct: get("risk_theme_concentration_cap_pct", 30),
		TotalActivePct:        get("risk_total_active_cap_pct", 8),
		DrawdownCircuitPct:    get("risk_drawdown_circuit_breaker_pct", 10),
		PerTradeDefaultPct:    get("risk_per_trade_default_pct", 1),
		PerTradeMaxPct:        get("risk_per_trade_max_pct", 2),
	}
}

// cachedPortfolioValueUSD returns the most recent portfolio_value_history
// snapshot (Spec 9c D13). Returns 0 if no snapshot exists. Used by
// trade_snapshot_json on open so the journal records portfolio context.
func (s *Server) cachedPortfolioValueUSD(ctx context.Context) float64 {
	rows, err := s.store.GetPortfolioValueHistory(ctx, 1)
	if err != nil || len(rows) == 0 {
		return 0
	}
	return rows[len(rows)-1].Total
}

func openPositionFromStock(h *domain.StockHolding, m metrics.StockMetrics) technicals.OpenPosition {
	op := technicals.OpenPosition{
		Sector: "Other",
	}
	if h.Ticker != nil {
		op.Ticker = *h.Ticker
	}
	if h.Sector != nil && *h.Sector != "" {
		op.Sector = *h.Sector
	}
	if m.CurrentValueUSD != nil {
		op.PositionUSD = *m.CurrentValueUSD
	} else {
		op.PositionUSD = h.InvestedUSD
	}
	// Per-position risk: qty × (entry − stop). qty = invested/avg_open.
	if h.StopLoss != nil && h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 {
		qty := h.InvestedUSD / *h.AvgOpenPrice
		entry := *h.AvgOpenPrice
		stop := *h.StopLoss
		if entry > stop {
			op.RiskUSD = qty * (entry - stop)
		}
	}
	return op
}

func openPositionFromCrypto(h *domain.CryptoHolding, m metrics.CryptoMetrics) technicals.OpenPosition {
	op := technicals.OpenPosition{
		Ticker: h.Symbol,
		Sector: "Crypto",
	}
	if h.IsCore {
		op.Sector = "Crypto Core"
	}
	if m.CurrentValueUSD != nil {
		op.PositionUSD = *m.CurrentValueUSD
	} else if h.CostBasisUSD != nil {
		op.PositionUSD = *h.CostBasisUSD
	}
	// Crypto risk in v1 — no SL math (most crypto holdings don't have SLs set).
	return op
}
