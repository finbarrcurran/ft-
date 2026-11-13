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
	"fmt"
	"log/slog"
	"ft/internal/domain"
	"ft/internal/metrics"
	"ft/internal/monitor"
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
	s.recordHistoryCash(r.Context(), date, check.CashUSD)
	s.applyBreaker(r.Context(), &check.PortfolioRiskCheck, &monitor.Telegram{})
	writeJSON(w, http.StatusOK, check)
}

// applyBreaker arms or clears the circuit breaker from the check (FT-079). It is ADVISORY (FT-106): it sets the
// flag and sends one Telegram alert on the transition to armed; nothing in the trade flow reads it. Used by the
// nightly job and the manual "Run snapshot now" route.
func (s *Server) applyBreaker(ctx context.Context, check *technicals.PortfolioRiskCheck, notify monitor.Notifier) {
	today := time.Now().UTC()
	// Auto-arm: drawdown at or beyond the cap and not already armed.
	if check.DrawdownPct <= -check.Caps.DrawdownCircuitPct && !check.CircuitBreakerActive {
		until := today.AddDate(0, 0, 14).Format("2006-01-02")
		_ = s.store.SetPreference(ctx, "risk_circuit_breaker_active", "true")
		_ = s.store.SetPreference(ctx, "risk_circuit_breaker_until", until)
		check.CircuitBreakerActive = true
		check.CircuitBreakerUntil = until
		check.Warnings = append(check.Warnings, "circuit breaker armed (drawdown >= cap) - advisory only")
		if notify != nil {
			msg := fmt.Sprintf("FT risk (ADVISORY): drawdown %.1f%% (%s) is beyond the -%.0f%% cap. Circuit breaker flagged until %s. Advisory only: no trade is blocked.",
				check.DrawdownPct, check.DrawdownBasis, check.Caps.DrawdownCircuitPct, until)
			if err := notify.Send(ctx, msg); err != nil && err != monitor.ErrNotConfigured {
				slog.Warn("risk: breaker alert not sent", "err", err)
			} else if err == nil {
				slog.Info("risk: breaker alert sent", "drawdownPct", check.DrawdownPct, "until", until)
			}
		}
	}
	// Auto-clear: drawdown back above -5% and the flag period has passed.
	if check.CircuitBreakerActive && check.DrawdownPct > -5 && check.CircuitBreakerUntil != "" {
		untilT, err := time.Parse("2006-01-02", check.CircuitBreakerUntil)
		if err == nil && today.After(untilT) {
			_ = s.store.SetPreference(ctx, "risk_circuit_breaker_active", "false")
			_ = s.store.SetPreference(ctx, "risk_circuit_breaker_until", "")
			check.CircuitBreakerActive = false
			check.CircuitBreakerUntil = ""
			check.Warnings = append(check.Warnings, "circuit breaker auto-cleared")
		}
	}
}

// RunNightlyRiskCheck is the scheduled arm/clear (FT-079): it records today's cash with the day's value row
// (FT-110), reads the cash-inclusive drawdown and applies the breaker. It runs after the nightly value snapshot.
func (s *Server) RunNightlyRiskCheck(ctx context.Context, now time.Time, notify monitor.Notifier) error {
	// FT-109: in demo mode the book is masked, so a check now would arm the breaker and store cash on synthetic
	// numbers. Skip it; the next night's run covers it.
	if s.demoModeOn(ctx) {
		slog.Info("risk: nightly check skipped (demo mode on)")
		return nil
	}
	var uid int64
	if err := s.store.DB.QueryRowContext(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&uid); err != nil {
		return err
	}
	check, err := s.computeRiskCheck(ctx, uid)
	if err != nil {
		return err
	}
	s.recordHistoryCash(ctx, now.UTC().Format("2006-01-02"), check.CashUSD)
	s.applyBreaker(ctx, &check.PortfolioRiskCheck, notify)
	// FT-144: one line a night with what the check saw, so the run can be read back from the journal.
	slog.Info("risk: nightly check", "portfolioValueUSD", check.PortfolioValue, "valueAsOf", check.ValueAsOf, "valueSource", check.ValueSource, "drawdownPct", check.DrawdownPct, "basis", check.DrawdownBasis, "cashUSD", check.CashUSD,
		"cashSource", s.effectiveCash(ctx, now).Source, "breakerActive", check.CircuitBreakerActive, "advisory", check.Advisory)
	return nil
}

// riskCashUSD is the cash balance used on the cash-inclusive basis. Demo mode excludes it (the real balance
// must not reach a demo view).
func (s *Server) riskCashUSD(ctx context.Context) float64 {
	if s.demoModeOn(ctx) {
		return 0
	}
	return s.effectiveCash(ctx, time.Now()).USD // FT-142: eToro cash while fresh, else the manual entry
}

// dashboardCheck wraps PortfolioRiskCheck with private snapshot fields the
// handler needs but the JSON-out type doesn't need to expose.
type dashboardCheck struct {
	technicals.PortfolioRiskCheck
	stocksUSD float64
	cryptoUSD float64
}

func (s *Server) computeRiskCheck(ctx context.Context, userID int64) (*dashboardCheck, error) {
	// SC-22 — masked synthetic holdings when demo mode is on, so the position
	// sizing / concentration readouts (SC-08) never expose real sizes.
	stocks, cryptos, err := s.loadHoldings(ctx, userID)
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
	// FT-079: the drawdown is measured on the value INCLUDING cash (as the Summary donut is). Days recorded before
	// the cash column existed use today's cash balance (cash history was not kept).
	cash := s.riskCashUSD(ctx)
	values := make([]float64, 0, len(history)+1)
	for _, p := range history {
		c := cash
		if p.Cash != nil {
			c = *p.Cash
		}
		values = append(values, p.Total+c)
	}
	values = append(values, portfolioValue+cash) // include current
	drawdownPct, _ := technicals.ComputeDrawdownPct(values)

	// Circuit-breaker state from prefs.
	cbActive, _ := s.store.GetPreference(ctx, "risk_circuit_breaker_active")
	cbUntil, _ := s.store.GetPreference(ctx, "risk_circuit_breaker_until")

	check := technicals.Compute(positions, portfolioValue, drawdownPct, caps, cbActive == "true", cbUntil)
	check.CashUSD = cash
	// FT-144: "today" in the drawdown is the LIVE holdings value (current stored prices, refreshed every 15 minutes),
	// never the last nightly snapshot; the snapshots are only the history it is compared with.
	check.ValueSource = "live holdings at stored prices (not the nightly snapshot)"
	if v, err := s.store.GetMeta(ctx, "last_refreshed_at"); err == nil && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			check.ValueAsOf = time.Unix(n, 0).UTC().Format(time.RFC3339)
		}
	}
	check.Advisory = true
	check.DrawdownBasis = "incl. cash; days before cash was recorded use today's balance"
	if cash == 0 {
		check.DrawdownBasis = "excl. cash"
	}
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
