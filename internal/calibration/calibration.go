// Package calibration — SC-38 score-vs-outcome, read-time + anti-overfit.
// PURE stats only: no write path to any band/weight/pillar, no fitted
// parameters, no optimised thresholds. The n-floors are structural gates
// (Fin ruling D), not tuned numbers. Whole-sample only (no per-band cells).
package calibration

import (
	"math"
	"sort"
)

// Point is one locked thesis with its forward price outcome (post-lock only).
type Point struct {
	Ticker      string  `json:"ticker"`
	Score       int     `json:"score"`
	MaxScore    int     `json:"maxScore"`
	LockedDate  string  `json:"lockedDate"`
	HoldingDays int     `json:"holdingPeriodTradingDays"`
	AbsReturn   float64 `json:"absoluteReturn"` // fraction, e.g. 0.083 = +8.3%
	SpyReturn   float64 `json:"spyReturn"`
	Excess      float64 `json:"excessReturn"` // ticker - SPY, the headline (ruling B)
	TooFresh    bool    `json:"tooFresh"`     // < 20 trading days (ruling F)
}

// NoOutcome is a locked thesis with no usable bars (listed, never zero-filled).
type NoOutcome struct {
	Ticker string `json:"ticker"`
	Reason string `json:"reason"`
}

// Summary is the whole-sample stats block. Correlation is present only at
// n>=30; the verdict is eligible only at n>=50 (ruling D). n here is the
// SETTLED sample (holding >= 20 td) the correlation is computed on.
type Summary struct {
	N                   int      `json:"n"` // locked theses with outcome data
	NoOutcomeCount      int      `json:"noOutcomeCount"`
	TooFreshCount       int      `json:"tooFreshCount"`
	NSettled            int      `json:"nSettled"`    // holding >= 20 td (stat sample)
	SpearmanRho         *float64 `json:"spearmanRho"` // nil unless nSettled >= 30
	SpearmanN           int      `json:"spearmanN"`
	MedianExcess        float64  `json:"medianExcess"`
	MeanExcess          float64  `json:"meanExcess"`
	MedianHoldingDays   int      `json:"medianHoldingDays"`
	CorrelationEligible bool     `json:"correlationEligible"` // nSettled >= 30
	VerdictEligible     bool     `json:"verdictEligible"`     // nSettled >= 50
}

// Summarize computes the whole-sample block. The Spearman correlation runs on
// the settled sub-sample (score vs excess), excluding too-fresh points whose
// returns are noise; both n's are reported. No fitted line, no cutoff.
func Summarize(points []Point, noOutcome int) Summary {
	s := Summary{N: len(points), NoOutcomeCount: noOutcome}
	var settledScores, settledExcess, allExcess, holdings []float64
	for _, p := range points {
		allExcess = append(allExcess, p.Excess)
		holdings = append(holdings, float64(p.HoldingDays))
		if p.TooFresh {
			s.TooFreshCount++
			continue
		}
		settledScores = append(settledScores, float64(p.Score))
		settledExcess = append(settledExcess, p.Excess)
	}
	s.NSettled = len(settledScores)
	s.CorrelationEligible = s.NSettled >= 30
	s.VerdictEligible = s.NSettled >= 50
	if s.CorrelationEligible {
		if rho, ok := SpearmanRho(settledScores, settledExcess); ok {
			s.SpearmanRho = &rho
			s.SpearmanN = s.NSettled
		}
	}
	s.MedianExcess = median(allExcess)
	s.MeanExcess = mean(allExcess)
	s.MedianHoldingDays = int(median(holdings))
	return s
}

// SpearmanRho returns the Spearman rank correlation of paired xs,ys; ok=false
// with < 3 points or zero variance.
func SpearmanRho(xs, ys []float64) (float64, bool) {
	n := len(xs)
	if n != len(ys) || n < 3 {
		return 0, false
	}
	rx, ry := ranks(xs), ranks(ys)
	var mx, my float64
	for i := 0; i < n; i++ {
		mx += rx[i]
		my += ry[i]
	}
	mx /= float64(n)
	my /= float64(n)
	var num, dx, dy float64
	for i := 0; i < n; i++ {
		a, b := rx[i]-mx, ry[i]-my
		num += a * b
		dx += a * a
		dy += b * b
	}
	if dx == 0 || dy == 0 {
		return 0, false
	}
	return num / math.Sqrt(dx*dy), true
}

// ranks returns tie-corrected average ranks (1-based).
func ranks(v []float64) []float64 {
	n := len(v)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	r := make([]float64, n)
	for i := 0; i < n; {
		j := i
		for j+1 < n && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			r[idx[k]] = avg
		}
		i = j + 1
	}
	return r
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	m := len(c) / 2
	if len(c)%2 == 1 {
		return c[m]
	}
	return (c[m-1] + c[m]) / 2
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
