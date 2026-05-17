package performance

import "math"

// ComputeMetrics rolls up a slice of closed trades into one TradeMetrics
// snapshot. Spec 9d D3. Pure function — no IO, no globals.
//
// A trade is a "winner" if realized_r_multiple > 0; "loser" if ≤ 0.
// (A flat exit at zero R counts as a loser for the win-rate calc; that
// matches Percoco's terminology where "process correctness" cares about
// trades that *captured* positive R.)
func ComputeMetrics(trades []ClosedTrade) TradeMetrics {
	if len(trades) == 0 {
		return TradeMetrics{}
	}
	var (
		winners, losers       []float64 // R-multiples
		totalPnL              float64
		totalHoldDays         int
	)
	for _, t := range trades {
		if t.RealizedRMultiple > 0 {
			winners = append(winners, t.RealizedRMultiple)
		} else {
			losers = append(losers, t.RealizedRMultiple)
		}
		totalPnL += t.RealizedPnLUSD
		totalHoldDays += t.HoldingPeriodDays
	}
	winRate := float64(len(winners)) / float64(len(trades))
	avgWinR := mean(winners)
	avgLossR := mean(losers)
	expectancy := winRate*avgWinR + (1.0-winRate)*avgLossR

	return TradeMetrics{
		Count:          len(trades),
		WinCount:       len(winners),
		LossCount:      len(losers),
		WinRate:        winRate,
		AvgWinnerR:     avgWinR,
		AvgLoserR:      avgLossR,
		Expectancy:     expectancy,
		TotalPnLUSD:    totalPnL,
		AvgHoldDays:    float64(totalHoldDays) / float64(len(trades)),
		MaxDrawdownPct: computeMaxDrawdownPct(trades),
	}
}

// computeMaxDrawdownPct walks the trades in chronological order, summing
// realized P&L cumulatively, tracking peak-to-trough. Returns the largest
// drawdown as a negative percentage (e.g. -4.2 = 4.2% below peak at the
// trough). Returns 0 for empty input.
//
// Trades are assumed to be sorted by closed_at ASC by the caller. If not,
// the result is still a valid drawdown but may not match the equity
// curve's reported peak/trough.
func computeMaxDrawdownPct(trades []ClosedTrade) float64 {
	if len(trades) == 0 {
		return 0
	}
	var cum, peak, maxDD float64
	for _, t := range trades {
		cum += t.RealizedPnLUSD
		if cum > peak {
			peak = cum
		}
		if peak > 0 {
			dd := (cum - peak) / peak * 100
			if dd < maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// RMultipleHistogram bins trades by R-multiple for the histogram chart.
// Default bins: <-3R, -3..-2R, -2..-1R, -1..0R, 0..+1R, +1..+2R, +2..+3R, ≥+3R.
//
// Min/Max are NOT serialised because the lowest/highest bins use
// math.Inf, which encoding/json rejects (silently truncates the body
// when the encoder errors mid-stream). UI only needs Label + Count.
type Bin struct {
	Label string  `json:"label"`
	Min   float64 `json:"-"`
	Max   float64 `json:"-"`
	Count int     `json:"count"`
}

// DefaultRBins returns the 8-bucket scheme used by Spec 9d D5.
func DefaultRBins() []Bin {
	return []Bin{
		{Label: "≤-3R", Min: math.Inf(-1), Max: -3},
		{Label: "-3..-2R", Min: -3, Max: -2},
		{Label: "-2..-1R", Min: -2, Max: -1},
		{Label: "-1..0R", Min: -1, Max: 0},
		{Label: "0..+1R", Min: 0, Max: 1},
		{Label: "+1..+2R", Min: 1, Max: 2},
		{Label: "+2..+3R", Min: 2, Max: 3},
		{Label: "≥+3R", Min: 3, Max: math.Inf(1)},
	}
}

// HistogramOf populates DefaultRBins with counts from `trades`.
func HistogramOf(trades []ClosedTrade) []Bin {
	bins := DefaultRBins()
	for _, t := range trades {
		r := t.RealizedRMultiple
		for i := range bins {
			if r >= bins[i].Min && r < bins[i].Max {
				bins[i].Count++
				break
			}
			// Last bin's upper bound is +Inf, so handle inclusive max.
			if i == len(bins)-1 && r >= bins[i].Min {
				bins[i].Count++
				break
			}
		}
	}
	return bins
}
