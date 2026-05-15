// Package alert classifies a stock holding into RED / AMBER / GREEN / NEUTRAL
// per the rules described in handoff doc §11.3 and ported verbatim from the
// Next.js prototype's lib/data-store/alert.ts.
//
//	RED:    distance-to-SL ≤ 3%   OR  RSI ≥ 75
//	AMBER:  distance-to-SL ≤ 6%   OR  (RSI between 65 and 74 inclusive)
//	GREEN:  RSI < 40  AND  goldenCross true  AND  R/R > 2
//	NEUTRAL: anything else
//
// Priority on early return: RED > AMBER > GREEN > NEUTRAL.
//
// When inputs are missing we fall through to neutral — never claim a status
// we can't justify. The triggers slice carries human-readable reason fragments
// for tooltips.
package alert

import (
	"fmt"
	"ft/internal/domain"
	"ft/internal/metrics"
)

// Compute returns the alert classification for a stock holding plus its
// already-computed metrics. (We require metrics rather than recomputing here
// so the handler only does the math once per row.)
func Compute(h *domain.StockHolding, m metrics.StockMetrics) domain.AlertResult {
	// ---- RED checks ----
	red := []string{}
	if m.DistanceToSLPct != nil && *m.DistanceToSLPct <= 3 {
		red = append(red, fmt.Sprintf("Distance to stop loss %.1f%% (≤ 3%%)", *m.DistanceToSLPct))
	}
	if h.RSI14 != nil && *h.RSI14 >= 75 {
		red = append(red, fmt.Sprintf("RSI %.0f (≥ 75, overbought)", *h.RSI14))
	}
	if len(red) > 0 {
		return domain.AlertResult{Status: domain.AlertRed, Triggers: red}
	}

	// ---- AMBER checks ----
	amber := []string{}
	if m.DistanceToSLPct != nil && *m.DistanceToSLPct <= 6 {
		amber = append(amber, fmt.Sprintf("Distance to stop loss %.1f%% (≤ 6%%)", *m.DistanceToSLPct))
	}
	if h.RSI14 != nil && *h.RSI14 >= 65 && *h.RSI14 <= 74 {
		amber = append(amber, fmt.Sprintf("RSI %.0f (65–74, elevated)", *h.RSI14))
	}
	if len(amber) > 0 {
		return domain.AlertResult{Status: domain.AlertAmber, Triggers: amber}
	}

	// ---- GREEN checks (all three required) ----
	rsiLow := h.RSI14 != nil && *h.RSI14 < 40
	golden := h.GoldenCross != nil && *h.GoldenCross
	rrOK := m.RiskReward != nil && *m.RiskReward > 2
	if rsiLow && golden && rrOK {
		return domain.AlertResult{
			Status: domain.AlertGreen,
			Triggers: []string{
				fmt.Sprintf("RSI %.0f (< 40, oversold)", *h.RSI14),
				"Golden Cross active (MA50 > MA200)",
				fmt.Sprintf("R/R %.2f (> 2, favourable)", *m.RiskReward),
			},
		}
	}

	return domain.AlertResult{Status: domain.AlertNeutral, Triggers: []string{}}
}
