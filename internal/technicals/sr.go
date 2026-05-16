package technicals

import (
	"math"
	"sort"
	"time"
)

// SRCandidate is one proposed support or resistance level for a
// ticker, produced by clustering weekly pivot highs/lows.
type SRCandidate struct {
	LevelType   string  // "support" | "resistance"
	Price       float64 // cluster centroid
	Touches     int     // pivots in this cluster
	LastTouchAt time.Time
	Score       float64 // ranking — higher = stronger candidate
}

// DetectSR walks weekly bars and returns the top-N support + top-N
// resistance candidates relative to currentPrice. Deterministic — no
// randomness, no ML.
//
// Algorithm:
//  1. Find weekly pivot highs (local maxima with `pivotN` bars either side).
//     Likewise pivot lows.
//  2. Cluster pivot prices: any two pivots within 1×ATR collapse into the
//     same cluster (simple agglomerative).
//  3. Score each cluster:  touches × recency × tightness
//      - touches:    number of pivots in cluster
//      - recency:    1.0 if newest touch within last 26 weeks (~6mo), else 0.6
//      - tightness:  1 / (1 + stddev(prices)/atr)
//  4. Add structural levels: 52w high (resistance), 52w low (support).
//  5. Filter: support candidates must be ≤ currentPrice; resistance ≥.
//  6. Sort by score desc, take top `topN` per side.
//
// Returns ([]SRCandidate, []SRCandidate) = (supports, resistances) each
// up to topN long. Empty slices if input is too short.
func DetectSR(bars []Bar, atrWeekly, currentPrice float64, topN, pivotN int) ([]SRCandidate, []SRCandidate) {
	if len(bars) < 2*pivotN+1 || atrWeekly <= 0 {
		return nil, nil
	}
	if pivotN <= 0 {
		pivotN = 3
	}
	if topN <= 0 {
		topN = 3
	}

	highs := pivots(bars, pivotN, true)
	lows := pivots(bars, pivotN, false)

	resClusters := clusterPivots(highs, atrWeekly)
	supClusters := clusterPivots(lows, atrWeekly)

	// Score every cluster.
	now := bars[len(bars)-1].Date
	scored := func(clusters []pivotCluster, levelType string) []SRCandidate {
		out := make([]SRCandidate, 0, len(clusters))
		for _, c := range clusters {
			rec := 0.6
			weeksAgo := now.Sub(c.LastDate).Hours() / 24 / 7
			if weeksAgo <= 26 {
				rec = 1.0
			}
			tightness := 1.0 / (1.0 + (c.StdDev / atrWeekly))
			score := float64(c.Touches) * rec * tightness
			out = append(out, SRCandidate{
				LevelType:   levelType,
				Price:       c.Centroid,
				Touches:     c.Touches,
				LastTouchAt: c.LastDate,
				Score:       score,
			})
		}
		return out
	}
	supports := scored(supClusters, "support")
	resistances := scored(resClusters, "resistance")

	// Structural levels: 52w high (resistance), 52w low (support).
	if len(bars) >= 52 {
		window := bars[len(bars)-52:]
		hi52, hi52Date := windowMax(window)
		lo52, lo52Date := windowMin(window)
		if hi52 > 0 {
			resistances = append(resistances, SRCandidate{
				LevelType: "resistance", Price: hi52, Touches: 1,
				LastTouchAt: hi52Date, Score: 0.7, // mid-tier baseline
			})
		}
		if lo52 > 0 {
			supports = append(supports, SRCandidate{
				LevelType: "support", Price: lo52, Touches: 1,
				LastTouchAt: lo52Date, Score: 0.7,
			})
		}
	}

	// Position filter + sort + trim.
	supports = filterSide(supports, currentPrice, true)
	resistances = filterSide(resistances, currentPrice, false)

	sort.Slice(supports, func(i, j int) bool { return supports[i].Score > supports[j].Score })
	sort.Slice(resistances, func(i, j int) bool { return resistances[i].Score > resistances[j].Score })

	if len(supports) > topN {
		supports = supports[:topN]
	}
	if len(resistances) > topN {
		resistances = resistances[:topN]
	}
	return supports, resistances
}

// ----- internals -------------------------------------------------------

type pivot struct {
	Price float64
	Date  time.Time
}

// pivots returns local extrema of `bars` (high or low depending on
// `wantHigh`) where the bar's price is strictly more extreme than the
// `n` bars on either side.
func pivots(bars []Bar, n int, wantHigh bool) []pivot {
	out := make([]pivot, 0)
	for i := n; i < len(bars)-n; i++ {
		ok := true
		for j := 1; j <= n && ok; j++ {
			if wantHigh {
				if bars[i].High <= bars[i-j].High || bars[i].High <= bars[i+j].High {
					ok = false
				}
			} else {
				if bars[i].Low >= bars[i-j].Low || bars[i].Low >= bars[i+j].Low {
					ok = false
				}
			}
		}
		if ok {
			if wantHigh {
				out = append(out, pivot{Price: bars[i].High, Date: bars[i].Date})
			} else {
				out = append(out, pivot{Price: bars[i].Low, Date: bars[i].Date})
			}
		}
	}
	return out
}

type pivotCluster struct {
	Centroid float64
	Touches  int
	LastDate time.Time
	StdDev   float64
}

// clusterPivots collapses pivots whose prices are within 1×ATR into
// single clusters via simple agglomerative passes.
func clusterPivots(pivots []pivot, bandwidth float64) []pivotCluster {
	if len(pivots) == 0 || bandwidth <= 0 {
		return nil
	}
	// Sort by price ascending for sweep.
	sort.Slice(pivots, func(i, j int) bool { return pivots[i].Price < pivots[j].Price })

	var clusters []pivotCluster
	curStart := 0
	for i := 1; i < len(pivots); i++ {
		if pivots[i].Price-pivots[i-1].Price > bandwidth {
			clusters = append(clusters, makeCluster(pivots[curStart:i]))
			curStart = i
		}
	}
	clusters = append(clusters, makeCluster(pivots[curStart:]))
	return clusters
}

func makeCluster(ps []pivot) pivotCluster {
	if len(ps) == 0 {
		return pivotCluster{}
	}
	var sum float64
	last := ps[0].Date
	for _, p := range ps {
		sum += p.Price
		if p.Date.After(last) {
			last = p.Date
		}
	}
	mean := sum / float64(len(ps))
	var sq float64
	for _, p := range ps {
		d := p.Price - mean
		sq += d * d
	}
	std := 0.0
	if len(ps) > 1 {
		std = math.Sqrt(sq / float64(len(ps)))
	}
	return pivotCluster{
		Centroid: mean,
		Touches:  len(ps),
		LastDate: last,
		StdDev:   std,
	}
}

func filterSide(cands []SRCandidate, currentPrice float64, isSupport bool) []SRCandidate {
	out := make([]SRCandidate, 0, len(cands))
	for _, c := range cands {
		if isSupport && c.Price <= currentPrice {
			out = append(out, c)
		}
		if !isSupport && c.Price >= currentPrice {
			out = append(out, c)
		}
	}
	return out
}

func windowMax(bars []Bar) (float64, time.Time) {
	max := 0.0
	var when time.Time
	for _, b := range bars {
		if b.High > max {
			max = b.High
			when = b.Date
		}
	}
	return max, when
}

func windowMin(bars []Bar) (float64, time.Time) {
	min := math.MaxFloat64
	var when time.Time
	for _, b := range bars {
		if b.Low > 0 && b.Low < min {
			min = b.Low
			when = b.Date
		}
	}
	if min == math.MaxFloat64 {
		return 0, time.Time{}
	}
	return min, when
}
