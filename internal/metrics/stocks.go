// Package metrics computes derived stock + crypto metrics from a holding.
// Pure functions, no side effects. Any null input that propagates yields null
// in the output, so the UI can render an em-dash rather than a misleading zero.
//
// Ported verbatim from the Next.js prototype:
//
//	lib/data-store/metrics.ts
//	lib/data-store/crypto-metrics.ts
package metrics

import "ft/internal/domain"

// StockMetrics carries the derived numbers for a stock row. JSON tags
// match the prototype's DerivedMetrics interface.
type StockMetrics struct {
	Quantity         *float64 `json:"quantity"`
	PNLUSD           *float64 `json:"pnlUsd"`
	PNLPct           *float64 `json:"pnlPct"`
	CurrentValueUSD  *float64 `json:"currentValueUsd"`
	PriceVs50MAPct   *float64 `json:"priceVs50maPct"`
	DistanceToSLPct  *float64 `json:"distanceToSlPct"`
	RiskReward       *float64 `json:"riskReward"`
	AnalystUpsidePct *float64 `json:"analystUpsidePct"`

	// SC-08 — explicit stop methodology.
	//   EffectiveSLPrice:  the stop price actually in force (manual override if
	//                      set, otherwise the sl_method computation).
	//   EffectiveSLSource: "manual" | "vol_envelope" | "technical" | "" (none).
	//   DistanceToTPPct:   (TP − price) / price × 100.
	//   ProgressToSL/TP:   fraction 0..1 of the entry→SL (resp. entry→TP)
	//                      journey already travelled; drives two-tier proximity.
	EffectiveSLPrice  *float64 `json:"effectiveSlPrice"`
	EffectiveSLSource string   `json:"effectiveSlSource,omitempty"`
	DistanceToTPPct   *float64 `json:"distanceToTpPct"`
	ProgressToSL      *float64 `json:"progressToSl"`
	ProgressToTP      *float64 `json:"progressToTp"`
}

// ComputeStock returns the eight derived metrics for one holding.
func ComputeStock(h *domain.StockHolding) StockMetrics {
	var m StockMetrics

	// quantity = invested / avgOpenPrice
	if h.AvgOpenPrice != nil && *h.AvgOpenPrice != 0 {
		q := h.InvestedUSD / *h.AvgOpenPrice
		m.Quantity = ptr(q)

		// currentValueUsd = quantity * currentPrice
		if h.CurrentPrice != nil {
			v := q * *h.CurrentPrice
			m.CurrentValueUSD = ptr(v)

			// pnl = currentValueUsd - invested
			p := v - h.InvestedUSD
			m.PNLUSD = ptr(p)

			if h.InvestedUSD != 0 {
				pct := (p / h.InvestedUSD) * 100
				m.PNLPct = ptr(pct)
			}
		}
	}

	// priceVs50maPct = (current - ma50) / ma50 * 100
	if h.CurrentPrice != nil && h.MA50 != nil && *h.MA50 != 0 {
		v := ((*h.CurrentPrice - *h.MA50) / *h.MA50) * 100
		m.PriceVs50MAPct = ptr(v)
	}

	// SC-08 — resolve the effective stop (manual override wins, else method).
	if sl, src, ok := EffectiveSL(h); ok {
		m.EffectiveSLPrice = ptr(sl)
		m.EffectiveSLSource = src
	}

	// distanceToSlPct = (current - effectiveSL) / current * 100
	// Positive means we're above the stop (safe); negative means past it.
	if h.CurrentPrice != nil && m.EffectiveSLPrice != nil && *h.CurrentPrice != 0 {
		v := ((*h.CurrentPrice - *m.EffectiveSLPrice) / *h.CurrentPrice) * 100
		m.DistanceToSLPct = ptr(v)
	}

	// distanceToTpPct = (takeProfit - current) / current * 100
	if h.CurrentPrice != nil && h.TakeProfit != nil && *h.CurrentPrice != 0 {
		v := ((*h.TakeProfit - *h.CurrentPrice) / *h.CurrentPrice) * 100
		m.DistanceToTPPct = ptr(v)
	}

	// riskReward = (takeProfit - current) / (current - effectiveSL)
	// Guard against non-positive denominator (stop above current is meaningless).
	if h.CurrentPrice != nil && m.EffectiveSLPrice != nil && h.TakeProfit != nil &&
		(*h.CurrentPrice-*m.EffectiveSLPrice) > 0 {
		v := (*h.TakeProfit - *h.CurrentPrice) / (*h.CurrentPrice - *m.EffectiveSLPrice)
		m.RiskReward = ptr(v)
	}

	// progressToSL = (entry - price) / (entry - effectiveSL), 0..1 fraction of
	// the way from entry down to the stop. progressToTP mirrors it to the upside.
	if h.AvgOpenPrice != nil && h.CurrentPrice != nil && m.EffectiveSLPrice != nil &&
		(*h.AvgOpenPrice-*m.EffectiveSLPrice) > 0 {
		v := (*h.AvgOpenPrice - *h.CurrentPrice) / (*h.AvgOpenPrice - *m.EffectiveSLPrice)
		m.ProgressToSL = ptr(v)
	}
	if h.AvgOpenPrice != nil && h.CurrentPrice != nil && h.TakeProfit != nil &&
		(*h.TakeProfit-*h.AvgOpenPrice) > 0 {
		v := (*h.CurrentPrice - *h.AvgOpenPrice) / (*h.TakeProfit - *h.AvgOpenPrice)
		m.ProgressToTP = ptr(v)
	}

	// analystUpsidePct = (analystTarget - current) / current * 100
	if h.CurrentPrice != nil && h.AnalystTarget != nil && *h.CurrentPrice != 0 {
		v := ((*h.AnalystTarget - *h.CurrentPrice) / *h.CurrentPrice) * 100
		m.AnalystUpsidePct = ptr(v)
	}

	return m
}

// EffectiveSL resolves the stop price actually in force for a holding, per
// SC-08's "manual override wins" rule:
//
//   - if a manual StopLoss is set (> 0) → that price, source "manual";
//   - else sl_method computes it:
//   - "technical":     support1 (else support) − volTier×ATR_weekly;
//   - "vol_envelope":  entry × (1 − vol12m/100 − safety)  [default method].
//
// Returns ok=false when the inputs for the chosen method are missing, so the
// caller renders an em-dash rather than a misleading zero.
func EffectiveSL(h *domain.StockHolding) (price float64, source string, ok bool) {
	// Manual override wins.
	if h.StopLoss != nil && *h.StopLoss > 0 {
		return *h.StopLoss, "manual", true
	}

	method := "vol_envelope"
	if h.SLMethod != nil && *h.SLMethod != "" {
		method = *h.SLMethod
	}

	switch method {
	case "technical":
		base := h.Support1
		if base == nil {
			base = h.Support
		}
		if base != nil && *base > 0 && h.ATRWeekly != nil && *h.ATRWeekly > 0 {
			sl := *base - volTierMultiple(h.VolTierAuto)*(*h.ATRWeekly)
			if sl > 0 {
				return sl, "technical", true
			}
		}
	default: // vol_envelope
		if h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 && h.Volatility12mPct != nil {
			safety := 0.02
			if h.SLSafetyPct != nil {
				safety = *h.SLSafetyPct
			}
			sl := *h.AvgOpenPrice * (1 - (*h.Volatility12mPct / 100.0) - safety)
			if sl > 0 {
				return sl, "vol_envelope", true
			}
		}
	}
	return 0, "", false
}

// volTierMultiple maps the auto-derived volatility tier to the ATR multiple
// used by the technical stop. Mirrors the client-side recompute in app.js.
func volTierMultiple(tier *string) float64 {
	t := ""
	if tier != nil {
		t = *tier
	}
	switch t {
	case "low":
		return 1.5
	case "high":
		return 2.5
	case "extreme":
		return 3.0
	default: // "medium" or unset
		return 2.0
	}
}

func ptr[T any](v T) *T { return &v }
