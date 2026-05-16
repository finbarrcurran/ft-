package technicals

import "math"

// ATR returns Wilder-smoothed Average True Range over `period` bars.
// Returns 0 when there's not enough data (`< period+1` bars).
//
// True Range for bar i:
//	TR = max( H - L, |H - prevClose|, |L - prevClose| )
//
// Wilder's smoothing is mathematically equivalent to an EMA with
// alpha = 1/period; chosen over simple-moving-average for stability.
//
// ATR is computed over `bars` which the caller has aggregated to the
// desired cadence. For Percoco's vol-adjusted stops we use period=14
// on **weekly** bars.
func ATR(bars []Bar, period int) float64 {
	if period <= 0 || len(bars) < period+1 {
		return 0
	}
	// Seed: simple average of first `period` true ranges.
	var seed float64
	for i := 1; i <= period; i++ {
		seed += trueRange(bars[i-1], bars[i])
	}
	atr := seed / float64(period)
	// Wilder smoothing for the remainder.
	for i := period + 1; i < len(bars); i++ {
		tr := trueRange(bars[i-1], bars[i])
		atr = (atr*(float64(period)-1) + tr) / float64(period)
	}
	return atr
}

// AvgATRPctOverWindow returns the average of (ATR / close) over the
// trailing `windowBars` bars worth of ATR snapshots. Used by the
// Percoco Q5 "volatility band" auto-score: is current ATR/price near
// its 12-month average?
//
// Implementation: for each anchor bar in the last `windowBars`, compute
// ATR over the (period bars prior) and divide by that bar's close.
// Average the resulting percentages.
//
// Returns 0 if windowBars < period or insufficient data.
func AvgATRPctOverWindow(bars []Bar, period, windowBars int) float64 {
	if period <= 0 || windowBars <= 0 {
		return 0
	}
	if len(bars) < period+windowBars {
		// Best-effort: shrink window to fit.
		windowBars = len(bars) - period
		if windowBars <= 0 {
			return 0
		}
	}
	var sum float64
	var n int
	for i := len(bars) - windowBars; i < len(bars); i++ {
		slice := bars[:i+1]
		a := ATR(slice, period)
		c := slice[len(slice)-1].Close
		if a > 0 && c > 0 {
			sum += a / c
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func trueRange(prev, cur Bar) float64 {
	return math.Max(
		cur.High-cur.Low,
		math.Max(math.Abs(cur.High-prev.Close), math.Abs(cur.Low-prev.Close)),
	)
}
