// Spec 13 — Test coverage for performance metrics + cohort assignment.
//
// Pure-function targets: ComputeMetrics, HistogramOf, AssignCohorts,
// GroupByCohort, scoreBucket, holdingBucket, MonotonicCheck,
// computeMaxDrawdownPct.

package performance

import (
	"math"
	"testing"
)

func almostEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
func pi(v int) *int                      { return &v }

// trade builds a minimal ClosedTrade for tests.
func trade(rMult, pnl float64, holdDays int) ClosedTrade {
	return ClosedTrade{
		Kind:              "stock",
		HoldingPeriodDays: holdDays,
		RealizedRMultiple: rMult,
		RealizedPnLUSD:    pnl,
	}
}

// ----- ComputeMetrics ----------------------------------------------------

func TestComputeMetrics_EmptyInput(t *testing.T) {
	m := ComputeMetrics(nil)
	if m.Count != 0 || m.WinRate != 0 || m.Expectancy != 0 {
		t.Fatalf("empty input → zero metrics, got %+v", m)
	}
}

func TestComputeMetrics_SingleWinner(t *testing.T) {
	trades := []ClosedTrade{trade(2.0, 200, 14)}
	m := ComputeMetrics(trades)
	if m.Count != 1 || m.WinCount != 1 || m.LossCount != 0 {
		t.Errorf("counts wrong: %+v", m)
	}
	if !almostEqual(m.WinRate, 1.0, 0.001) {
		t.Errorf("win rate got %v want 1.0", m.WinRate)
	}
	// Expectancy = 1.0 × 2.0 + 0.0 × 0 = 2.0
	if !almostEqual(m.Expectancy, 2.0, 0.001) {
		t.Errorf("expectancy got %v want 2.0", m.Expectancy)
	}
	if !almostEqual(m.AvgHoldDays, 14.0, 0.001) {
		t.Errorf("avg hold got %v want 14", m.AvgHoldDays)
	}
}

func TestComputeMetrics_MixedWinLossExpectancy(t *testing.T) {
	// 60% win rate, avg winner +2R, avg loser -1R.
	// Expectancy = 0.6 × 2 + 0.4 × -1 = 1.2 - 0.4 = 0.8R
	trades := []ClosedTrade{
		trade(2.0, 200, 10),
		trade(2.0, 200, 10),
		trade(2.0, 200, 10),
		trade(-1.0, -100, 5),
		trade(-1.0, -100, 5),
	}
	// 3 winners, 2 losers but to force "60% win rate" we need... 6 trades.
	// Adjust: 3/5 = 60%.
	m := ComputeMetrics(trades)
	if m.Count != 5 || m.WinCount != 3 || m.LossCount != 2 {
		t.Errorf("counts wrong: count=%d wins=%d losses=%d", m.Count, m.WinCount, m.LossCount)
	}
	if !almostEqual(m.WinRate, 0.6, 0.001) {
		t.Errorf("win rate got %v want 0.6", m.WinRate)
	}
	if !almostEqual(m.AvgWinnerR, 2.0, 0.001) {
		t.Errorf("avg winner R got %v want 2.0", m.AvgWinnerR)
	}
	if !almostEqual(m.AvgLoserR, -1.0, 0.001) {
		t.Errorf("avg loser R got %v want -1.0", m.AvgLoserR)
	}
	if !almostEqual(m.Expectancy, 0.8, 0.001) {
		t.Errorf("expectancy got %v want 0.8", m.Expectancy)
	}
	if !almostEqual(m.TotalPnLUSD, 400.0, 0.001) {
		t.Errorf("total P&L got %v want 400", m.TotalPnLUSD)
	}
}

func TestComputeMetrics_ZeroRCountsAsLoser(t *testing.T) {
	// A flat exit at exactly 0R counts as a loser per the function's
	// docstring (process-correctness lens).
	trades := []ClosedTrade{trade(0.0, 0, 5)}
	m := ComputeMetrics(trades)
	if m.WinCount != 0 || m.LossCount != 1 {
		t.Errorf("0R should be loser, got wins=%d losses=%d", m.WinCount, m.LossCount)
	}
}

// ----- HistogramOf -------------------------------------------------------

func TestHistogramOf_BucketBoundaries(t *testing.T) {
	// One trade per bucket — confirm each lands in the right bin.
	trades := []ClosedTrade{
		trade(-5.0, 0, 0),   // ≤-3R
		trade(-2.5, 0, 0),   // -3..-2R
		trade(-1.5, 0, 0),   // -2..-1R
		trade(-0.5, 0, 0),   // -1..0R
		trade(0.5, 0, 0),    // 0..+1R
		trade(1.5, 0, 0),    // +1..+2R
		trade(2.5, 0, 0),    // +2..+3R
		trade(5.0, 0, 0),    // ≥+3R
	}
	bins := HistogramOf(trades)
	if len(bins) != 8 {
		t.Fatalf("expected 8 bins, got %d", len(bins))
	}
	for i, b := range bins {
		if b.Count != 1 {
			t.Errorf("bin %d (%s): count %d, want 1", i, b.Label, b.Count)
		}
	}
}

func TestHistogramOf_EdgeValueLowerInclusive(t *testing.T) {
	// 0.0 is at the boundary between -1..0R and 0..+1R.
	// Code uses [min, max) — 0 belongs to 0..+1R (since -1..0R upper is exclusive).
	trades := []ClosedTrade{trade(0.0, 0, 0)}
	bins := HistogramOf(trades)
	// 0 >= 0 AND 0 < 1 → 0..+1R bucket. Verify.
	for _, b := range bins {
		if b.Label == "0..+1R" && b.Count != 1 {
			t.Errorf("0.0 should land in 0..+1R, got %d", b.Count)
		}
	}
}

func TestHistogramOf_NegativeInfinityBin(t *testing.T) {
	// Extreme loser → ≤-3R bucket.
	trades := []ClosedTrade{trade(-12.5, -5000, 30)}
	bins := HistogramOf(trades)
	for _, b := range bins {
		if b.Label == "≤-3R" && b.Count != 1 {
			t.Errorf("-12.5R should land in ≤-3R, got %d", b.Count)
		}
	}
}

// ----- AssignCohorts + scoreBucket ---------------------------------------

func TestScoreBucket(t *testing.T) {
	cases := []struct {
		name   string
		score  *int
		expect string
	}{
		{"nil score", nil, ""},
		{"0", pi(0), "le-8"},
		{"8", pi(8), "le-8"},
		{"9", pi(9), "9-12"},
		{"12", pi(12), "9-12"},
		{"13", pi(13), "13-16"},
		{"16", pi(16), "13-16"},
	}
	for _, tc := range cases {
		if got := scoreBucket(tc.score); got != tc.expect {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.expect)
		}
	}
}

func TestHoldingBucket(t *testing.T) {
	cases := []struct {
		days   int
		expect string
	}{
		{0, "short"}, {7, "short"},
		{8, "medium"}, {60, "medium"},
		{61, "long"}, {365, "long"},
	}
	for _, tc := range cases {
		if got := holdingBucket(tc.days); got != tc.expect {
			t.Errorf("days=%d got %q want %q", tc.days, got, tc.expect)
		}
	}
}

func TestAssignCohorts_FullSet(t *testing.T) {
	tr := ClosedTrade{
		Kind:              "stock",
		SetupType:         "A_breakout_retest",
		RegimeEffective:   "stable",
		JordiScore:        pi(14),
		CowenScore:        pi(11),
		PercocoScore:      pi(8),
		HoldingPeriodDays: 30,
		ExitReason:        "tp1_hit",
	}
	cohorts := AssignCohorts(tr)
	expectations := []string{
		"all",
		"kind:stock",
		"setup:A_breakout_retest",
		"regime:stable",
		"exit:tp1_hit",
		"jordi:13-16",
		"cowen:9-12",
		"percoco:le-8",
		"hold:medium",
	}
	for _, want := range expectations {
		if !contains(cohorts, want) {
			t.Errorf("expected cohort %q in %v", want, cohorts)
		}
	}
}

func TestAssignCohorts_NilScoresOmitted(t *testing.T) {
	// Trades without framework scores → no jordi/cowen/percoco cohorts.
	tr := ClosedTrade{Kind: "crypto", HoldingPeriodDays: 5}
	cohorts := AssignCohorts(tr)
	for _, c := range cohorts {
		if c == "jordi:" || c == "cowen:" || c == "percoco:" {
			t.Errorf("nil score should not produce empty-bucket cohort, got %v", cohorts)
		}
	}
	// Must contain "all", "kind:crypto", "hold:short".
	for _, want := range []string{"all", "kind:crypto", "hold:short"} {
		if !contains(cohorts, want) {
			t.Errorf("expected %q in %v", want, cohorts)
		}
	}
}

func TestGroupByCohort(t *testing.T) {
	trades := []ClosedTrade{
		{Kind: "stock", HoldingPeriodDays: 10, RealizedRMultiple: 2},
		{Kind: "stock", HoldingPeriodDays: 90, RealizedRMultiple: -1},
		{Kind: "crypto", HoldingPeriodDays: 30, RealizedRMultiple: 1.5},
	}
	groups := GroupByCohort(trades)
	if len(groups["all"]) != 3 {
		t.Errorf("'all' bucket should have 3, got %d", len(groups["all"]))
	}
	if len(groups["kind:stock"]) != 2 {
		t.Errorf("'kind:stock' should have 2, got %d", len(groups["kind:stock"]))
	}
	if len(groups["kind:crypto"]) != 1 {
		t.Errorf("'kind:crypto' should have 1, got %d", len(groups["kind:crypto"]))
	}
}

// ----- MonotonicCheck -----------------------------------------------------

func TestMonotonicCheck_Healthy(t *testing.T) {
	// ≤8: 0.1R, 9-12: 0.5R, 13-16: 1.0R — strictly monotonic up.
	ok, reason := MonotonicCheck(0.1, 0.5, 1.0)
	if !ok || reason != "" {
		t.Errorf("monotonic should be true, got ok=%v reason=%q", ok, reason)
	}
}

func TestMonotonicCheck_LowBeatsMiddle(t *testing.T) {
	ok, reason := MonotonicCheck(0.8, 0.3, 1.0)
	if ok || reason == "" {
		t.Errorf("expected non-monotonic with reason, got ok=%v reason=%q", ok, reason)
	}
}

func TestMonotonicCheck_MiddleBeatsTop(t *testing.T) {
	ok, reason := MonotonicCheck(0.1, 0.9, 0.5)
	if ok || reason == "" {
		t.Errorf("expected non-monotonic, got ok=%v reason=%q", ok, reason)
	}
}

func TestMonotonicCheck_EqualValues(t *testing.T) {
	// Equal cohorts (no spread) — treated as monotonic per the > check.
	ok, _ := MonotonicCheck(0.5, 0.5, 0.5)
	if !ok {
		t.Errorf("equal cohorts should pass (no strict inversion)")
	}
}

// ----- computeMaxDrawdownPct ---------------------------------------------

func TestComputeMaxDrawdownPct_NeverDown(t *testing.T) {
	// Cumulative goes 100 → 300 → 600 — strictly up.
	trades := []ClosedTrade{
		{RealizedPnLUSD: 100},
		{RealizedPnLUSD: 200},
		{RealizedPnLUSD: 300},
	}
	if dd := computeMaxDrawdownPct(trades); dd != 0 {
		t.Errorf("monotonic up → 0 DD, got %v", dd)
	}
}

func TestComputeMaxDrawdownPct_WithDrawdown(t *testing.T) {
	// Cum: 100, 300, 200 → peak 300, trough 200 → DD = -33.33%
	trades := []ClosedTrade{
		{RealizedPnLUSD: 100},
		{RealizedPnLUSD: 200}, // cum=300, peak=300
		{RealizedPnLUSD: -100}, // cum=200, DD = (200-300)/300 = -33.33%
	}
	dd := computeMaxDrawdownPct(trades)
	if !almostEqual(dd, -33.333, 0.01) {
		t.Errorf("max DD got %v want -33.33", dd)
	}
}

func TestComputeMaxDrawdownPct_EmptyInput(t *testing.T) {
	if dd := computeMaxDrawdownPct(nil); dd != 0 {
		t.Errorf("empty input should give 0, got %v", dd)
	}
}

// ----- helpers ----------------------------------------------------------

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
