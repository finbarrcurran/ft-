package technicals

// SC-35 W1 — trend moving averages computed from BARS.
//
// These are NOT Yahoo's `fiftyDayAverage` / `twoHundredDayAverage` quote fields
// (those are 50-DAY / 200-DAY SMAs and live on stock_holdings.ma50/ma200). The
// Percoco trend gate needs a *weekly* trend line, so we compute:
//
//   - MA50W   = 50-week SMA from weekly close, the gate's spine
//               (trend_intact ⇔ weekly close > MA50W).
//   - MA200D  = 200-day SMA from daily close, secondary confirmation.
//
// Both are simple (not Wilder/EMA) averages: Percoco reads the slope and the
// price-vs-line relationship, neither of which benefits from smoothing choice.
// Pure functions; the caller supplies already-aggregated bars.

// SMA returns the simple moving average of the trailing `period` closes and
// ok=true. Returns (0, false) when there are fewer than `period` bars — the
// caller writes NULL rather than asserting a line it can't justify.
func SMA(bars []Bar, period int) (float64, bool) {
	if period <= 0 || len(bars) < period {
		return 0, false
	}
	var sum float64
	for _, b := range bars[len(bars)-period:] {
		sum += b.Close
	}
	return sum / float64(period), true
}

// MA50Weekly returns the 50-week simple moving average from weekly bars.
// Needs ≥50 weekly bars (~1 year of data) or it returns (nil, false).
func MA50Weekly(weekly []Bar) (*float64, bool) {
	v, ok := SMA(weekly, 50)
	if !ok {
		return nil, false
	}
	return &v, true
}

// MA200Daily returns the 200-day simple moving average from daily bars.
// Needs ≥200 daily bars or it returns (nil, false).
func MA200Daily(daily []Bar) (*float64, bool) {
	v, ok := SMA(daily, 200)
	if !ok {
		return nil, false
	}
	return &v, true
}

// SMASlopeRising reports whether the `period`-bar SMA is higher now than it was
// `lookback` bars ago — i.e. the trend line is sloping up. Used as the optional
// stricter-gate input (patch §4: require MA50W AND MA200D both rising). Returns
// false when there isn't enough history to compare the two anchor points.
func SMASlopeRising(bars []Bar, period, lookback int) bool {
	if lookback <= 0 || len(bars) < period+lookback {
		return false
	}
	nowMA, ok1 := SMA(bars, period)
	prevMA, ok2 := SMA(bars[:len(bars)-lookback], period)
	if !ok1 || !ok2 {
		return false
	}
	return nowMA > prevMA
}
