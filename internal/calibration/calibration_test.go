package calibration

import (
	"math"
	"testing"
)

func TestSpearmanMonotonic(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	ys := []float64{2, 4, 6, 8, 10} // perfectly monotonic increasing
	rho, ok := SpearmanRho(xs, ys)
	if !ok || math.Abs(rho-1.0) > 1e-9 {
		t.Fatalf("monotonic rho=%v ok=%v want 1", rho, ok)
	}
	ys2 := []float64{10, 8, 6, 4, 2}
	rho2, _ := SpearmanRho(xs, ys2)
	if math.Abs(rho2+1.0) > 1e-9 {
		t.Fatalf("decreasing rho=%v want -1", rho2)
	}
}

func TestSpearmanTiesAndGuards(t *testing.T) {
	if _, ok := SpearmanRho([]float64{1, 2}, []float64{1, 2}); ok {
		t.Error("n<3 should be not-ok")
	}
	if _, ok := SpearmanRho([]float64{1, 1, 1}, []float64{1, 2, 3}); ok {
		t.Error("zero variance in x should be not-ok")
	}
	rho, ok := SpearmanRho([]float64{1, 2, 2, 3}, []float64{1, 2, 3, 4})
	if !ok || rho <= 0 {
		t.Errorf("tie case rho=%v ok=%v want positive", rho, ok)
	}
}

func mkPts(n int, tooFresh int) []Point {
	pts := make([]Point, 0, n)
	for i := 0; i < n; i++ {
		pts = append(pts, Point{Ticker: "T", Score: i % 15, MaxScore: 16, Excess: float64(i) * 0.001, TooFresh: i < tooFresh, HoldingDays: 30})
	}
	return pts
}

func TestSummarizeGating(t *testing.T) {
	// nSettled = 25 (< 30) -> no correlation, no verdict.
	s := Summarize(mkPts(25, 0), 1)
	if s.CorrelationEligible || s.SpearmanRho != nil || s.VerdictEligible {
		t.Errorf("n=25 should suppress correlation+verdict: %+v", s)
	}
	if s.NoOutcomeCount != 1 {
		t.Errorf("noOutcome=%d want 1", s.NoOutcomeCount)
	}
	// nSettled = 35 -> correlation eligible, verdict NOT.
	s2 := Summarize(mkPts(35, 0), 0)
	if !s2.CorrelationEligible || s2.SpearmanRho == nil || s2.VerdictEligible {
		t.Errorf("n=35 should show correlation but not verdict: %+v", s2)
	}
	// nSettled = 55 -> verdict eligible.
	s3 := Summarize(mkPts(55, 0), 0)
	if !s3.VerdictEligible || s3.SpearmanRho == nil {
		t.Errorf("n=55 should be verdict-eligible: %+v", s3)
	}
	// too-fresh excluded from settled: 40 points, 15 fresh -> settled 25 -> no correlation.
	s4 := Summarize(mkPts(40, 15), 0)
	if s4.NSettled != 25 || s4.CorrelationEligible {
		t.Errorf("too-fresh exclusion wrong: nSettled=%d elig=%v", s4.NSettled, s4.CorrelationEligible)
	}
	if s4.N != 40 || s4.TooFreshCount != 15 {
		t.Errorf("counts wrong: N=%d fresh=%d", s4.N, s4.TooFreshCount)
	}
}
