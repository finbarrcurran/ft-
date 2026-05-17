// Spec 9c D12 — portfolio risk engine.
//
// Pre-trade + continuous check. Inputs:
//   - all open positions + their entry/SL math
//   - portfolio_value_history for drawdown
//   - user_preferences for cap thresholds
// Outputs:
//   - PortfolioRiskCheck struct telling the UI whether the proposed
//     trade is allowed, and which caps are tight
//
// Pure functions; DB IO is delegated to caller (server handler assembles
// the inputs from store).

package technicals

import "strings"

// OpenPosition is a thin view of a holding for risk calculation.
type OpenPosition struct {
	Ticker         string
	Sector         string  // theme key for theme-concentration cap
	BottleneckTag  string  // optional — overrides Sector for theme grouping
	PositionUSD    float64 // current value
	RiskUSD        float64 // (entry − stop) × units; 0 if no SL set
}

// RiskCaps is the threshold set from user_preferences.
type RiskCaps struct {
	ConcentrationPct      float64 // default 15
	ThemeConcentrationPct float64 // default 30
	TotalActivePct        float64 // default 8
	DrawdownCircuitPct    float64 // default 10
	PerTradeDefaultPct    float64 // default 1
	PerTradeMaxPct        float64 // default 2
}

// PortfolioRiskCheck is what the handler returns + the dashboard reads.
type PortfolioRiskCheck struct {
	PortfolioValue        float64            `json:"portfolioValue"`
	Concentration         map[string]float64 `json:"concentration"`    // ticker → %
	ThemeConcentration    map[string]float64 `json:"themeConcentration"` // theme → %
	TotalActiveRiskPct    float64            `json:"totalActiveRiskPct"`
	DrawdownPct           float64            `json:"drawdownPct"` // negative when underwater
	CircuitBreakerActive  bool               `json:"circuitBreakerActive"`
	CircuitBreakerUntil   string             `json:"circuitBreakerUntil,omitempty"` // ISO date
	Caps                  RiskCaps           `json:"caps"`
	Warnings              []string           `json:"warnings"`
}

// Compute synthesises the portfolio-risk snapshot from inputs. `drawdownPct`
// is negative when underwater (e.g. -4.2 means 4.2% below peak).
func Compute(positions []OpenPosition, portfolioValue, drawdownPct float64, caps RiskCaps, circuitBreakerActive bool, circuitBreakerUntil string) PortfolioRiskCheck {
	out := PortfolioRiskCheck{
		PortfolioValue:       portfolioValue,
		DrawdownPct:          drawdownPct,
		Caps:                 caps,
		CircuitBreakerActive: circuitBreakerActive,
		CircuitBreakerUntil:  circuitBreakerUntil,
		Concentration:        map[string]float64{},
		ThemeConcentration:   map[string]float64{},
	}
	if portfolioValue <= 0 {
		return out
	}

	var totalRiskUSD float64
	for _, p := range positions {
		pct := (p.PositionUSD / portfolioValue) * 100
		out.Concentration[p.Ticker] = pct
		if pct > caps.ConcentrationPct {
			out.Warnings = append(out.Warnings,
				p.Ticker+" exceeds concentration cap")
		}
		theme := p.BottleneckTag
		if theme == "" {
			theme = p.Sector
		}
		if theme == "" {
			theme = "Unknown"
		}
		out.ThemeConcentration[theme] += pct
		totalRiskUSD += p.RiskUSD
	}
	out.TotalActiveRiskPct = (totalRiskUSD / portfolioValue) * 100

	for theme, pct := range out.ThemeConcentration {
		if pct > caps.ThemeConcentrationPct {
			out.Warnings = append(out.Warnings, "theme "+theme+" exceeds cap")
		}
	}
	if out.TotalActiveRiskPct > caps.TotalActivePct {
		out.Warnings = append(out.Warnings, "total active risk exceeds cap")
	}
	if drawdownPct <= -caps.DrawdownCircuitPct && !circuitBreakerActive {
		out.Warnings = append(out.Warnings,
			"drawdown >= circuit-breaker threshold — review")
	}
	return out
}

// AllowsNewTrade reports whether the system permits opening a new trade
// right now. Spec 9c D12 / D13: blocked when:
//   - circuit breaker active (drawdown >10% AND breaker not yet auto-cleared)
//   - proposed trade would push concentration/theme/total caps over
//
// The caller supplies a `proposed` position to evaluate; pass an empty
// OpenPosition{} to ask "are we accepting any new trades?".
func (c PortfolioRiskCheck) AllowsNewTrade(proposed OpenPosition) (bool, []string) {
	var reasons []string
	if c.CircuitBreakerActive {
		reasons = append(reasons, "circuit breaker active")
	}
	if c.PortfolioValue > 0 && proposed.PositionUSD > 0 {
		// Predict concentration if this trade lands.
		predictedTickerPct := ((c.Concentration[proposed.Ticker] / 100 * c.PortfolioValue) + proposed.PositionUSD) /
			c.PortfolioValue * 100
		if predictedTickerPct > c.Caps.ConcentrationPct {
			reasons = append(reasons,
				"would exceed concentration cap on "+proposed.Ticker)
		}
		theme := strings.TrimSpace(proposed.BottleneckTag)
		if theme == "" {
			theme = proposed.Sector
		}
		if theme != "" {
			predictedThemePct := ((c.ThemeConcentration[theme] / 100 * c.PortfolioValue) + proposed.PositionUSD) /
				c.PortfolioValue * 100
			if predictedThemePct > c.Caps.ThemeConcentrationPct {
				reasons = append(reasons, "would exceed theme cap on "+theme)
			}
		}
		if proposed.RiskUSD > 0 {
			predictedTotalRiskPct := c.TotalActiveRiskPct + (proposed.RiskUSD/c.PortfolioValue)*100
			if predictedTotalRiskPct > c.Caps.TotalActivePct {
				reasons = append(reasons, "would exceed total active risk cap")
			}
		}
	}
	return len(reasons) == 0, reasons
}

// ComputeDrawdownPct walks portfolio_value_history and returns
// (currentDrawdownPct, peakUSD). Negative drawdown when below peak.
// Empty input → (0, 0).
func ComputeDrawdownPct(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	peak := 0.0
	for _, v := range values {
		if v > peak {
			peak = v
		}
	}
	if peak <= 0 {
		return 0, 0
	}
	cur := values[len(values)-1]
	return ((cur - peak) / peak) * 100, peak
}
