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

// StockMetrics carries the eight derived numbers for a stock row. JSON tags
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

	// distanceToSlPct = (current - stopLoss) / current * 100
	// Positive means we're above the stop (safe); negative means past it.
	if h.CurrentPrice != nil && h.StopLoss != nil && *h.CurrentPrice != 0 {
		v := ((*h.CurrentPrice - *h.StopLoss) / *h.CurrentPrice) * 100
		m.DistanceToSLPct = ptr(v)
	}

	// riskReward = (takeProfit - current) / (current - stopLoss)
	// Guard against non-positive denominator (stop above current is meaningless).
	if h.CurrentPrice != nil && h.StopLoss != nil && h.TakeProfit != nil &&
		(*h.CurrentPrice-*h.StopLoss) > 0 {
		v := (*h.TakeProfit - *h.CurrentPrice) / (*h.CurrentPrice - *h.StopLoss)
		m.RiskReward = ptr(v)
	}

	// analystUpsidePct = (analystTarget - current) / current * 100
	if h.CurrentPrice != nil && h.AnalystTarget != nil && *h.CurrentPrice != 0 {
		v := ((*h.AnalystTarget - *h.CurrentPrice) / *h.CurrentPrice) * 100
		m.AnalystUpsidePct = ptr(v)
	}

	return m
}

func ptr[T any](v T) *T { return &v }
