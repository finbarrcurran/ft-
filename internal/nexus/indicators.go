package nexus

import (
	"ft/internal/store"
	"math"
	"sort"
)

// Shared technical primitives for the SC-36 compute engines. All operate on a
// date-ascending bar slice. Validated against the Visser snapshots (W3): RSI is
// Wilder-smoothed (median err 0.03 vs the sheets); MAs/returns/ATR/Williams %R
// all match to <0.27 median error.

func closesOf(b []store.DailyBarRow) []float64 {
	c := make([]float64, len(b))
	for i := range b {
		c[i] = b[i].Close
	}
	return c
}

// sortBars returns the bars sorted ascending by date (defensive copy).
func sortBars(b []store.DailyBarRow) []store.DailyBarRow {
	out := make([]store.DailyBarRow, len(b))
	copy(out, b)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// barsUpTo returns the bars with date <= asOf (assumes ascending input).
func barsUpTo(b []store.DailyBarRow, asOf string) []store.DailyBarRow {
	var out []store.DailyBarRow
	for _, x := range b {
		if x.Date <= asOf {
			out = append(out, x)
		}
	}
	return out
}

func sma(c []float64, n int) (float64, bool) {
	if len(c) < n {
		return 0, false
	}
	s := 0.0
	for _, v := range c[len(c)-n:] {
		s += v
	}
	return s / float64(n), true
}

// wilderRSI is the standard Wilder-smoothed RSI over n periods.
func wilderRSI(c []float64, n int) (float64, bool) {
	if len(c) < n+1 {
		return 0, false
	}
	var g, l float64
	for i := 1; i <= n; i++ {
		d := c[i] - c[i-1]
		if d > 0 {
			g += d
		} else {
			l -= d
		}
	}
	ag, al := g/float64(n), l/float64(n)
	for i := n + 1; i < len(c); i++ {
		d := c[i] - c[i-1]
		up, dn := 0.0, 0.0
		if d > 0 {
			up = d
		} else {
			dn = -d
		}
		ag = (ag*float64(n-1) + up) / float64(n)
		al = (al*float64(n-1) + dn) / float64(n)
	}
	if al == 0 {
		return 100, true
	}
	rs := ag / al
	return 100 - 100/(1+rs), true
}

// atr14 is the Wilder-smoothed Average True Range over n periods.
func atr(b []store.DailyBarRow, n int) (float64, bool) {
	if len(b) < n+1 {
		return 0, false
	}
	trs := make([]float64, 0, len(b)-1)
	for i := 1; i < len(b); i++ {
		h, l, pc := b[i].High, b[i].Low, b[i-1].Close
		tr := h - l
		if v := math.Abs(h - pc); v > tr {
			tr = v
		}
		if v := math.Abs(l - pc); v > tr {
			tr = v
		}
		trs = append(trs, tr)
	}
	a := 0.0
	for _, v := range trs[:n] {
		a += v
	}
	a /= float64(n)
	for i := n; i < len(trs); i++ {
		a = (a*float64(n-1) + trs[i]) / float64(n)
	}
	return a, true
}

// slopePct = (MA_today − MA_{t−L}) / MA_{t−L} × 100 over lookback L.
func slopePct(c []float64, n, l int) (float64, bool) {
	if len(c) < n+l {
		return 0, false
	}
	today := 0.0
	for _, v := range c[len(c)-n:] {
		today += v
	}
	today /= float64(n)
	prev := 0.0
	for _, v := range c[len(c)-n-l : len(c)-l] {
		prev += v
	}
	prev /= float64(n)
	if prev == 0 {
		return 0, false
	}
	return (today - prev) / prev * 100, true
}

// retPct is the n-trading-day percent return.
func retPct(c []float64, n int) (float64, bool) {
	if len(c) < n+1 || c[len(c)-1-n] == 0 {
		return 0, false
	}
	return (c[len(c)-1]/c[len(c)-1-n] - 1) * 100, true
}

// williamsR over n periods (range −100..0; near 0 = pinned at highs).
func williamsR(b []store.DailyBarRow, n int) (float64, bool) {
	if len(b) < n {
		return 0, false
	}
	w := b[len(b)-n:]
	hi, lo := w[0].High, w[0].Low
	for _, x := range w {
		if x.High > hi {
			hi = x.High
		}
		if x.Low < lo {
			lo = x.Low
		}
	}
	if hi == lo {
		return 0, false
	}
	return -100 * (hi - b[len(b)-1].Close) / (hi - lo), true
}

// rangePos20 = close position within the n-bar high-low range, 0..100.
func rangePos(b []store.DailyBarRow, n int) (float64, bool) {
	if len(b) < n {
		return 0, false
	}
	w := b[len(b)-n:]
	hi, lo := w[0].High, w[0].Low
	for _, x := range w {
		if x.High > hi {
			hi = x.High
		}
		if x.Low < lo {
			lo = x.Low
		}
	}
	if hi == lo {
		return 0, false
	}
	return (b[len(b)-1].Close - lo) / (hi - lo) * 100, true
}

func pstdev(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := 0.0
	for _, v := range xs {
		m += v
	}
	m /= float64(len(xs))
	s := 0.0
	for _, v := range xs {
		s += (v - m) * (v - m)
	}
	return math.Sqrt(s / float64(len(xs)))
}

func dailyReturns(c []float64) []float64 {
	if len(c) < 2 {
		return nil
	}
	out := make([]float64, 0, len(c)-1)
	for i := 1; i < len(c); i++ {
		if c[i-1] != 0 {
			out = append(out, c[i]/c[i-1]-1)
		}
	}
	return out
}

func clamp100(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 100 {
		return 100
	}
	return x
}

func medianOf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
