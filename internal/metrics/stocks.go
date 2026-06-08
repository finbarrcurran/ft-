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

	// SC-35 Phase 2 — Percoco technical trade levels.
	//   TP1Price/TP2Price: resistance_1 / resistance_2. Informational "next
	//                      resistance" for holds; the staged exit targets for
	//                      trades (Decision E — the entry×multiplier is retired).
	//   RToTP1:            (TP1 − entry) / (entry − SL). Trades are gated on this
	//                      (red/VETO < 1.5, Decision F); holds show it for info.
	//   NeedsLevels:       true when sl_method='technical' is selected but
	//                      support_1 / atr_weekly are missing, so NO stop computes.
	//                      Surfaces the gap rather than silently using vol-envelope.
	TP1Price    *float64 `json:"tp1Price,omitempty"`
	TP2Price    *float64 `json:"tp2Price,omitempty"`
	RToTP1      *float64 `json:"rToTp1,omitempty"`
	NeedsLevels bool     `json:"needsLevels,omitempty"`
}

// TechnicalSLBuffer is Percoco's k in the technical stop `support_1 − k×ATR_weekly`
// (SC-35 D-k, locked at 0.5). Tighter than the vol-envelope catastrophe stop —
// it is a trade-management stop, not a conviction-hold stop.
const TechnicalSLBuffer = 0.5

// RMultipleVetoThreshold — a trade whose reward-to-risk to TP1 is below this is
// flagged red / VETO (Decision F). Holds are never gated on R:R.
const RMultipleVetoThreshold = 1.5

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

	// SC-35 Phase 2 — Percoco technical trade levels. resistance_1/2 surface as
	// TP1/TP2 (informational "next resistance" for holds per 2.4; the staged
	// exit targets for trades per 2.2).
	if h.Resistance1 != nil && *h.Resistance1 > 0 {
		m.TP1Price = ptr(*h.Resistance1)
	}
	if h.Resistance2 != nil && *h.Resistance2 > 0 {
		m.TP2Price = ptr(*h.Resistance2)
	}

	// R-to-TP1 = (TP1 − entry) / (entry − SL). Entry = avg open (fallback
	// proposed entry, then current). Needs both an effective stop and a TP1.
	if entry := entryPrice(h); entry != nil && m.TP1Price != nil &&
		m.EffectiveSLPrice != nil && (*entry-*m.EffectiveSLPrice) > 0 {
		r := (*m.TP1Price - *entry) / (*entry - *m.EffectiveSLPrice)
		m.RToTP1 = ptr(r)
	}

	// NeedsLevels — sl_method='technical' selected but no stop could be computed
	// and there's no manual override, so the row has no stop at all. Surface the
	// gap (Phase 2.1) instead of silently using vol-envelope.
	if h.SLMethod != nil && *h.SLMethod == "technical" &&
		!(h.StopLoss != nil && *h.StopLoss > 0) && m.EffectiveSLPrice == nil {
		m.NeedsLevels = true
	}

	return m
}

// entryPrice returns the holding's entry reference for R:R math: the actual
// average open, else a proposed entry, else the current price. nil when none
// are positive.
func entryPrice(h *domain.StockHolding) *float64 {
	if h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 {
		return h.AvgOpenPrice
	}
	if h.ProposedEntry != nil && *h.ProposedEntry > 0 {
		return h.ProposedEntry
	}
	if h.CurrentPrice != nil && *h.CurrentPrice > 0 {
		return h.CurrentPrice
	}
	return nil
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
		// SC-35 Phase 2.1 — SL = support_1 − 0.5×ATR_weekly (k = TechnicalSLBuffer).
		// support_1 specifically (not the legacy `support` column); if it or the
		// weekly ATR is missing we return ok=false so the caller surfaces a
		// "needs levels" state rather than silently dropping to vol-envelope.
		if h.Support1 != nil && *h.Support1 > 0 && h.ATRWeekly != nil && *h.ATRWeekly > 0 {
			sl := *h.Support1 - TechnicalSLBuffer*(*h.ATRWeekly)
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

func ptr[T any](v T) *T { return &v }
