// Spec 13 — Test coverage for sector rotation metrics (Spec 9f D3).
//
// Pure-function targets: classifyTag, weeklySparkline, computeReturn,
// computeSPYReturn, computeYTD, applyOrdering.

package sector_rotation

import (
	"math"
	"testing"
)

func almostEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// ----- classifyTag --------------------------------------------------------

func TestClassifyTag_RotatingInThreshold(t *testing.T) {
	// Lock per Spec 9f #1: rs ≥ 1.05 → rotating_in.
	if classifyTag(1.05) != "rotating_in" {
		t.Errorf("1.05 should be rotating_in")
	}
	if classifyTag(1.5) != "rotating_in" {
		t.Errorf("1.5 should be rotating_in")
	}
}

func TestClassifyTag_RotatingOutThreshold(t *testing.T) {
	if classifyTag(0.95) != "rotating_out" {
		t.Errorf("0.95 should be rotating_out")
	}
	if classifyTag(0.5) != "rotating_out" {
		t.Errorf("0.5 should be rotating_out")
	}
}

func TestClassifyTag_NeutralBand(t *testing.T) {
	if classifyTag(1.0) != "neutral" {
		t.Errorf("1.0 should be neutral")
	}
	if classifyTag(1.049) != "neutral" {
		t.Errorf("1.049 should be neutral")
	}
	if classifyTag(0.951) != "neutral" {
		t.Errorf("0.951 should be neutral")
	}
}

// ----- weeklySparkline ---------------------------------------------------

func TestWeeklySparkline_ShortInput(t *testing.T) {
	// Less than wantPoints × 5 trading days → returns full series as-is.
	closes := []float64{100, 101, 102, 103}
	out := weeklySparkline(closes, 26)
	if len(out) != 4 {
		t.Fatalf("short input → return full, got len %d", len(out))
	}
}

func TestWeeklySparkline_ChronologicalOrder(t *testing.T) {
	// 130 closes → 26 weekly samples expected, chronological.
	closes := make([]float64, 130)
	for i := range closes {
		closes[i] = float64(100 + i) // 100, 101, ..., 229
	}
	out := weeklySparkline(closes, 26)
	if len(out) != 26 {
		t.Fatalf("expected 26 sparkline points, got %d", len(out))
	}
	// First sample must be ≤ last (chronological — old → new).
	if out[0] > out[len(out)-1] {
		t.Errorf("sparkline should be chronological asc, got first=%v last=%v", out[0], out[len(out)-1])
	}
}

func TestWeeklySparkline_EmptyInput(t *testing.T) {
	if got := weeklySparkline(nil, 26); got != nil {
		t.Errorf("nil input → nil, got %v", got)
	}
}

// ----- computeReturn -----------------------------------------------------

func TestComputeReturn_StraightForward(t *testing.T) {
	byDate := map[string]float64{
		"2026-01-01": 100,
		"2026-02-01": 110,
	}
	dates := []string{"2026-01-01", "2026-02-01"}
	// 30 days ago from 2026-02-01 ≈ 2026-01-02, but only 2026-01-01 exists.
	// computeReturn walks dates ASC, picks last date ≤ target.
	ret := computeReturn(byDate, dates, 110, 30)
	if ret == nil {
		t.Fatal("expected non-nil return")
	}
	if !almostEqual(*ret, 0.10, 0.001) {
		t.Errorf("expected 10%% return, got %v", *ret)
	}
}

func TestComputeReturn_NoEarlierDate(t *testing.T) {
	// Only one date in history; target lookback (30 days back) is BEFORE
	// that date → no valid reference → nil.
	byDate := map[string]float64{"2026-02-01": 110}
	dates := []string{"2026-02-01"}
	if got := computeReturn(byDate, dates, 110, 30); got != nil {
		t.Errorf("single-date series should return nil, got %v", *got)
	}
}

func TestComputeReturn_ZeroDaysAgo(t *testing.T) {
	byDate := map[string]float64{"2026-02-01": 110}
	dates := []string{"2026-02-01"}
	if got := computeReturn(byDate, dates, 110, 0); got != nil {
		t.Errorf("zero daysAgo → nil, got %v", got)
	}
}

func TestComputeReturn_EmptySeries(t *testing.T) {
	if got := computeReturn(map[string]float64{}, nil, 100, 30); got != nil {
		t.Errorf("empty → nil, got %v", got)
	}
}

// ----- applyOrdering -----------------------------------------------------

func TestApplyOrdering_DefaultsTo3MDescending(t *testing.T) {
	r1 := 0.10
	r2 := 0.25
	r3 := -0.05
	rows := []SectorMetrics{
		{Code: "low", Return3M: &r1, DisplayOrderAuto: 1},
		{Code: "high", Return3M: &r2, DisplayOrderAuto: 2},
		{Code: "neg", Return3M: &r3, DisplayOrderAuto: 3},
	}
	applyOrdering(rows)
	// Expect: high (0.25), low (0.10), neg (-0.05).
	if rows[0].Code != "high" || rows[1].Code != "low" || rows[2].Code != "neg" {
		t.Fatalf("expected high,low,neg order; got %s,%s,%s",
			rows[0].Code, rows[1].Code, rows[2].Code)
	}
}

func TestApplyOrdering_UserOrderingWinsWhenSet(t *testing.T) {
	r1, r2 := 0.10, 0.25
	one, two := 1, 2
	rows := []SectorMetrics{
		{Code: "A", Return3M: &r1, DisplayOrderAuto: 99, DisplayOrderUser: &two},
		{Code: "B", Return3M: &r2, DisplayOrderAuto: 1, DisplayOrderUser: &one},
	}
	applyOrdering(rows)
	if rows[0].Code != "B" || rows[1].Code != "A" {
		t.Errorf("user order should win, got %s,%s", rows[0].Code, rows[1].Code)
	}
}

func TestApplyOrdering_NilReturnsSortLast(t *testing.T) {
	r := 0.10
	rows := []SectorMetrics{
		{Code: "noData", Return3M: nil, DisplayOrderAuto: 1},
		{Code: "hasData", Return3M: &r, DisplayOrderAuto: 2},
	}
	applyOrdering(rows)
	if rows[0].Code != "hasData" || rows[1].Code != "noData" {
		t.Errorf("nil 3M should sort last, got %s,%s", rows[0].Code, rows[1].Code)
	}
}
