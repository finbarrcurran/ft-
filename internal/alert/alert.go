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

// ProximityMargin is the spec D12 default of 5% on either side of a manual
// SL/TP. When future regime overlays (Spec 9b) move to SHIFTING/DEFENSIVE,
// tighten to 3% — leave as a const for now.
const ProximityMargin = 0.05

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
	// AMBER_SL_PROXIMITY (Spec 3 D12): manual SL set, current price is within
	// ProximityMargin of SL on the way down, and today's change is negative.
	if reason, ok := slProximity(h, ProximityMargin); ok {
		amber = append(amber, reason)
	}
	if len(amber) > 0 {
		return domain.AlertResult{Status: domain.AlertAmber, Triggers: amber}
	}

	// ---- GREEN checks ----
	// Classic green: all three of RSI < 40, golden cross, R/R > 2.
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
	// GREEN_TP_PROXIMITY (Spec 3 D12): manual TP set, current price is within
	// ProximityMargin of TP on the way up, today's change is positive.
	if reason, ok := tpProximity(h, ProximityMargin); ok {
		return domain.AlertResult{Status: domain.AlertGreen, Triggers: []string{reason}}
	}

	return domain.AlertResult{Status: domain.AlertNeutral, Triggers: []string{}}
}

// slProximity implements AMBER_SL_PROXIMITY per Spec 3 D12.
//
//	fires when current price <= manual_sl_price * (1 + margin)
//	  AND current price > manual_sl_price
//	  AND today's change is negative
func slProximity(h *domain.StockHolding, margin float64) (string, bool) {
	if h.StopLoss == nil || h.CurrentPrice == nil || h.DailyChangePct == nil {
		return "", false
	}
	sl := *h.StopLoss
	px := *h.CurrentPrice
	if sl <= 0 || px <= 0 {
		return "", false
	}
	if px > sl && px <= sl*(1+margin) && *h.DailyChangePct < 0 {
		gap := (px - sl) / sl * 100
		return fmt.Sprintf("Price within %.1f%% of stop loss (today %.2f%%)", gap, *h.DailyChangePct), true
	}
	return "", false
}

// tpProximity implements GREEN_TP_PROXIMITY per Spec 3 D12.
//
//	fires when current price >= manual_tp_price * (1 - margin)
//	  AND current price < manual_tp_price
//	  AND today's change is positive
func tpProximity(h *domain.StockHolding, margin float64) (string, bool) {
	if h.TakeProfit == nil || h.CurrentPrice == nil || h.DailyChangePct == nil {
		return "", false
	}
	tp := *h.TakeProfit
	px := *h.CurrentPrice
	if tp <= 0 || px <= 0 {
		return "", false
	}
	if px < tp && px >= tp*(1-margin) && *h.DailyChangePct > 0 {
		gap := (tp - px) / tp * 100
		return fmt.Sprintf("Price within %.1f%% of take profit (today +%.2f%%)", gap, *h.DailyChangePct), true
	}
	return "", false
}
