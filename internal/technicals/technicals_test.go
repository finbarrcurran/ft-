// Spec 13 — Test coverage for the Percoco execution-layer math.
//
// Covers: ATR Wilder smoothing, vol-tier thresholds + multipliers,
// suggested SL/TP formulas, R-multiple, PositionSize, and ComputeDrawdownPct.

package technicals

import (
	"math"
	"testing"
	"time"
)

func almostEqual(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// ----- ATR ---------------------------------------------------------------

func TestATR_InsufficientBars(t *testing.T) {
	bars := make([]Bar, 10) // < period+1 = 15
	if got := ATR(bars, 14); got != 0 {
		t.Fatalf("ATR with 10 bars period=14 should be 0, got %v", got)
	}
}

func TestATR_FlatBars_IsZero(t *testing.T) {
	// 20 identical bars (no range, no gap) → ATR = 0.
	bars := make([]Bar, 20)
	for i := range bars {
		bars[i] = Bar{Open: 100, High: 100, Low: 100, Close: 100}
	}
	if got := ATR(bars, 14); got != 0 {
		t.Fatalf("flat bars should yield ATR=0, got %v", got)
	}
}

func TestATR_ConstantTrueRange(t *testing.T) {
	// Bars where every TR is exactly 2.0 → ATR should converge to 2.0.
	bars := make([]Bar, 30)
	for i := range bars {
		// Alternate close so prev-close-based TR stays at 2.
		c := 100.0
		bars[i] = Bar{Open: c, High: c + 1, Low: c - 1, Close: c}
	}
	got := ATR(bars, 14)
	if !almostEqual(got, 2.0, 0.001) {
		t.Fatalf("ATR over constant-TR bars should be 2.0, got %v", got)
	}
}

func TestATR_PeriodZero(t *testing.T) {
	bars := make([]Bar, 30)
	if got := ATR(bars, 0); got != 0 {
		t.Fatalf("period=0 should yield 0, got %v", got)
	}
}

// ----- VolTier -----------------------------------------------------------

func TestVolTier_Thresholds(t *testing.T) {
	cases := []struct {
		name           string
		atr, price     float64
		expectTier     string
		expectMultMin  float64
		expectMultMax  float64
	}{
		{"low: 1.5%", 1.5, 100, "low", 1.5, 1.5},
		{"low: just under 2%", 1.99, 100, "low", 1.5, 1.5},
		{"medium: exactly 2%", 2.0, 100, "medium", 2.0, 2.0},
		{"medium: 3.5%", 3.5, 100, "medium", 2.0, 2.0},
		{"high: exactly 4%", 4.0, 100, "high", 2.5, 2.5},
		{"high: 6%", 6.0, 100, "high", 2.5, 2.5},
		{"extreme: exactly 7%", 7.0, 100, "extreme", 3.0, 3.0},
		{"extreme: 12%", 12.0, 100, "extreme", 3.0, 3.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := VolTier(tc.atr, tc.price)
			if got != tc.expectTier {
				t.Fatalf("%s: tier got %s want %s", tc.name, got, tc.expectTier)
			}
			mult := VolTierMultiplier(got)
			if mult < tc.expectMultMin || mult > tc.expectMultMax {
				t.Fatalf("%s: multiplier got %v want %v", tc.name, mult, tc.expectMultMin)
			}
		})
	}
}

func TestVolTier_InvalidInputs(t *testing.T) {
	if VolTier(0, 100) != "" {
		t.Error("ATR 0 should return empty string")
	}
	if VolTier(2.0, 0) != "" {
		t.Error("price 0 should return empty string")
	}
	// Unknown tier defaults to medium (2.0).
	if VolTierMultiplier("garbage") != 2.0 {
		t.Error("unknown tier should default to 2.0")
	}
}

// ----- SuggestedSL/TP ----------------------------------------------------

func TestSuggestedSL(t *testing.T) {
	// SL = support - N × ATR. medium → N = 2.0.
	got := SuggestedSL(95.0, 3.0, "medium")
	want := 95.0 - 2.0*3.0
	if !almostEqual(got, want, 0.001) {
		t.Fatalf("SuggestedSL got %v want %v", got, want)
	}
}

func TestSuggestedSL_HighVolTier(t *testing.T) {
	// high → N = 2.5
	got := SuggestedSL(100, 4.0, "high")
	want := 100.0 - 2.5*4.0
	if !almostEqual(got, want, 0.001) {
		t.Fatalf("SuggestedSL high got %v want %v", got, want)
	}
}

func TestSuggestedSL_InvalidInputs(t *testing.T) {
	if SuggestedSL(0, 3.0, "medium") != 0 {
		t.Error("support 0 → 0")
	}
	if SuggestedSL(100, 0, "medium") != 0 {
		t.Error("ATR 0 → 0")
	}
}

func TestSuggestedTP1(t *testing.T) {
	// TP1 = resistance1 - 0.25 × ATR
	got := SuggestedTP1(120.0, 4.0)
	want := 120.0 - 0.25*4.0
	if !almostEqual(got, want, 0.001) {
		t.Fatalf("SuggestedTP1 got %v want %v", got, want)
	}
}

func TestSuggestedTP2_ZeroWhenNoResistance(t *testing.T) {
	if SuggestedTP2(0, 4.0) != 0 {
		t.Error("no resistance → 0")
	}
}

// ----- R-multiple --------------------------------------------------------

func TestRMultiple_TextbookExample(t *testing.T) {
	// Entry 100, stop 95, target 115 → risk 5, reward 15, R = 3.
	if got := RMultiple(100, 95, 115); !almostEqual(got, 3.0, 0.001) {
		t.Fatalf("RMultiple got %v want 3.0", got)
	}
}

func TestRMultiple_ZeroWhenRiskIsZero(t *testing.T) {
	// entry == stop → no risk → return 0 (broken trade signal).
	if got := RMultiple(100, 100, 110); got != 0 {
		t.Fatalf("entry==stop should give 0, got %v", got)
	}
}

func TestRMultiple_NegativeRisk(t *testing.T) {
	// entry < stop → broken trade math → return 0.
	if got := RMultiple(100, 105, 110); got != 0 {
		t.Fatalf("entry<stop should give 0, got %v", got)
	}
}

// ----- PositionSize ------------------------------------------------------

func TestPositionSize_TextbookExample(t *testing.T) {
	// $100,000 portfolio, 1% per-trade, entry 100, stop 95 → risk $1,000;
	// units = $1,000 / $5 = 200; sizeUSD = 200 × 100 = $20,000.
	units, sizeUSD, riskUSD := PositionSize(100_000, 1.0, 100, 95)
	if !almostEqual(units, 200, 0.01) {
		t.Errorf("units got %v want 200", units)
	}
	if !almostEqual(sizeUSD, 20_000, 0.01) {
		t.Errorf("sizeUSD got %v want 20000", sizeUSD)
	}
	if !almostEqual(riskUSD, 1_000, 0.01) {
		t.Errorf("riskUSD got %v want 1000", riskUSD)
	}
}

func TestPositionSize_InvalidInputs(t *testing.T) {
	// Each of these should return zeros.
	for _, tc := range []struct {
		name                                    string
		pv, pct, entry, stop                    float64
	}{
		{"zero portfolio", 0, 1, 100, 95},
		{"zero pct", 100000, 0, 100, 95},
		{"zero entry", 100000, 1, 0, 95},
		{"zero stop", 100000, 1, 100, 0},
		{"entry <= stop", 100000, 1, 95, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, s, r := PositionSize(tc.pv, tc.pct, tc.entry, tc.stop)
			if u != 0 || s != 0 || r != 0 {
				t.Errorf("expected zeros, got (%v, %v, %v)", u, s, r)
			}
		})
	}
}

// ----- ComputeDrawdownPct ------------------------------------------------
//
// Returns (currentDrawdownPct_signed, peakUSD).
// Negative DD when current < peak; 0 when at the peak.

func TestComputeDrawdownPct_MonotonicallyUp(t *testing.T) {
	// Strictly rising series → current is the peak → DD = 0; peak = last.
	vals := []float64{100, 105, 110, 115, 120}
	dd, peak := ComputeDrawdownPct(vals)
	if !almostEqual(dd, 0, 0.001) {
		t.Errorf("rising series → DD expected 0, got %v", dd)
	}
	if !almostEqual(peak, 120, 0.001) {
		t.Errorf("rising series → peak expected 120, got %v", peak)
	}
}

func TestComputeDrawdownPct_AtTrough(t *testing.T) {
	// Peak 120, trough 90 → DD = (90-120)/120 = -25%; peak = 120.
	vals := []float64{100, 120, 90}
	dd, peak := ComputeDrawdownPct(vals)
	if !almostEqual(dd, -25.0, 0.01) {
		t.Errorf("trough DD got %v want -25.0", dd)
	}
	if !almostEqual(peak, 120, 0.001) {
		t.Errorf("peak got %v want 120", peak)
	}
}

func TestComputeDrawdownPct_RecoveryAfterTrough(t *testing.T) {
	// Peak 120, trough 90, recover to 110 → DD = (110-120)/120 ≈ -8.33%; peak = 120.
	vals := []float64{100, 120, 90, 110}
	dd, peak := ComputeDrawdownPct(vals)
	if !almostEqual(dd, -8.333, 0.01) {
		t.Errorf("recovery DD got %v want -8.33", dd)
	}
	if !almostEqual(peak, 120, 0.001) {
		t.Errorf("peak got %v want 120", peak)
	}
}

func TestComputeDrawdownPct_EmptyInput(t *testing.T) {
	dd, peak := ComputeDrawdownPct(nil)
	if dd != 0 || peak != 0 {
		t.Errorf("nil input → (0,0), got (%v,%v)", dd, peak)
	}
}

func TestComputeDrawdownPct_AllZeros(t *testing.T) {
	// Peak resolves to 0 → guard clause returns (0,0).
	dd, peak := ComputeDrawdownPct([]float64{0, 0, 0})
	if dd != 0 || peak != 0 {
		t.Errorf("all zeros → (0,0), got (%v,%v)", dd, peak)
	}
}

// ----- AggregateToWeekly -------------------------------------------------

func TestAggregateToWeekly_SingleWeek(t *testing.T) {
	// Five daily bars Mon-Fri → one weekly bar.
	monday := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	daily := []Bar{
		{Date: monday,                  Open: 100, High: 102, Low: 99, Close: 101},
		{Date: monday.AddDate(0, 0, 1), Open: 101, High: 105, Low: 100, Close: 104},
		{Date: monday.AddDate(0, 0, 2), Open: 104, High: 107, Low: 103, Close: 106},
		{Date: monday.AddDate(0, 0, 3), Open: 106, High: 108, Low: 102, Close: 103},
		{Date: monday.AddDate(0, 0, 4), Open: 103, High: 110, Low: 102, Close: 109},
	}
	weekly := AggregateToWeekly(daily)
	if len(weekly) != 1 {
		t.Fatalf("expected 1 weekly bar, got %d", len(weekly))
	}
	w := weekly[0]
	if w.Open != 100 {
		t.Errorf("Monday open should be 100, got %v", w.Open)
	}
	if w.High != 110 {
		t.Errorf("week high should be 110, got %v", w.High)
	}
	if w.Low != 99 {
		t.Errorf("week low should be 99, got %v", w.Low)
	}
	if w.Close != 109 {
		t.Errorf("Friday close should be 109, got %v", w.Close)
	}
}

func TestAggregateToWeekly_Empty(t *testing.T) {
	if got := AggregateToWeekly(nil); len(got) != 0 {
		t.Errorf("nil input → empty, got %v", got)
	}
}
