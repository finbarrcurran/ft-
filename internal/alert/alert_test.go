// Spec 13 — Test coverage for alert classification.
//
// Tests the priority order RED > AMBER > GREEN > NEUTRAL and the
// distance-to-SL / RSI thresholds. The old flat-margin SL/TP proximity (and
// the Spec 9b D6 margin scaling that tuned it) was retired in SC-08 — the
// progress-based two-tier proximity family now lives in proximity.go and is
// covered by proximity_test.go.

package alert

import (
	"ft/internal/domain"
	"ft/internal/metrics"
	"strings"
	"testing"
)

// helpers ----------------------------------------------------------------

func pf(v float64) *float64 { return &v }
func pb(v bool) *bool       { return &v }

// hold builds a minimal StockHolding for tests. Pass nil for any field
// you don't care about.
func hold(rsi, daily, sl, tp, currentPrice *float64, golden *bool) *domain.StockHolding {
	return &domain.StockHolding{
		Name:           "TEST",
		RSI14:          rsi,
		DailyChangePct: daily,
		StopLoss:       sl,
		TakeProfit:     tp,
		CurrentPrice:   currentPrice,
		GoldenCross:    golden,
	}
}

func met(distSL, rr *float64) metrics.StockMetrics {
	return metrics.StockMetrics{
		DistanceToSLPct: distSL,
		RiskReward:      rr,
	}
}

// RED ----------------------------------------------------------------------

func TestRed_DistanceToSLAt3Percent(t *testing.T) {
	h := hold(pf(50), nil, pf(100), nil, pf(103), nil)
	r := Compute(h, met(pf(3.0), nil))
	if r.Status != domain.AlertRed {
		t.Fatalf("dist 3.0 → expected RED, got %s", r.Status)
	}
	if len(r.Triggers) == 0 || !strings.Contains(r.Triggers[0], "stop loss") {
		t.Errorf("expected stop-loss trigger, got %v", r.Triggers)
	}
}

func TestRed_DistanceToSLBelow3Percent(t *testing.T) {
	h := hold(pf(50), nil, pf(100), nil, pf(102), nil)
	r := Compute(h, met(pf(2.0), nil))
	if r.Status != domain.AlertRed {
		t.Fatalf("dist 2.0 → expected RED, got %s", r.Status)
	}
}

func TestRed_RSIAt75(t *testing.T) {
	h := hold(pf(75), nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertRed {
		t.Fatalf("RSI 75 → expected RED, got %s", r.Status)
	}
}

func TestRed_RSIAbove75(t *testing.T) {
	h := hold(pf(82), nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertRed {
		t.Fatalf("RSI 82 → expected RED, got %s", r.Status)
	}
}

// AMBER --------------------------------------------------------------------

func TestAmber_DistanceToSLBetween3And6(t *testing.T) {
	h := hold(pf(50), nil, pf(100), nil, pf(105), nil)
	r := Compute(h, met(pf(5.0), nil))
	if r.Status != domain.AlertAmber {
		t.Fatalf("dist 5.0 → expected AMBER, got %s", r.Status)
	}
}

func TestAmber_DistanceToSLAt6Percent(t *testing.T) {
	h := hold(pf(50), nil, pf(100), nil, pf(106), nil)
	r := Compute(h, met(pf(6.0), nil))
	if r.Status != domain.AlertAmber {
		t.Fatalf("dist 6.0 → expected AMBER (≤ 6), got %s", r.Status)
	}
}

func TestAmber_RSI65(t *testing.T) {
	h := hold(pf(65), nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertAmber {
		t.Fatalf("RSI 65 → expected AMBER, got %s", r.Status)
	}
}

func TestAmber_RSI74(t *testing.T) {
	h := hold(pf(74), nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertAmber {
		t.Fatalf("RSI 74 → expected AMBER, got %s", r.Status)
	}
}

// GREEN --------------------------------------------------------------------

func TestGreen_ClassicTriad(t *testing.T) {
	h := hold(pf(35), nil, nil, nil, nil, pb(true))
	r := Compute(h, met(nil, pf(2.5)))
	if r.Status != domain.AlertGreen {
		t.Fatalf("classic triad → expected GREEN, got %s", r.Status)
	}
	if len(r.Triggers) != 3 {
		t.Errorf("expected 3 triggers, got %d (%v)", len(r.Triggers), r.Triggers)
	}
}

func TestGreen_NoGoldenCross_FallsThrough(t *testing.T) {
	h := hold(pf(35), nil, nil, nil, nil, pb(false))
	r := Compute(h, met(nil, pf(2.5)))
	if r.Status != domain.AlertNeutral {
		t.Fatalf("RSI low + R/R good but no golden cross → expected NEUTRAL, got %s", r.Status)
	}
}

// NEUTRAL / fallthrough ---------------------------------------------------

func TestNeutral_AllZero(t *testing.T) {
	h := hold(nil, nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertNeutral {
		t.Fatalf("empty inputs → expected NEUTRAL, got %s", r.Status)
	}
}

func TestNeutral_RSIBelowGreenThreshold_NoGolden(t *testing.T) {
	// RSI 30 alone doesn't trigger green without golden + R/R.
	h := hold(pf(30), nil, nil, nil, nil, nil)
	r := Compute(h, met(nil, nil))
	if r.Status != domain.AlertNeutral {
		t.Fatalf("RSI 30 only → expected NEUTRAL, got %s", r.Status)
	}
}

// Priority ----------------------------------------------------------------

func TestPriority_RedBeatsAmber(t *testing.T) {
	// RSI 75 (RED) + dist-to-SL 5 (AMBER) → RED wins.
	h := hold(pf(75), nil, pf(100), nil, pf(105), nil)
	r := Compute(h, met(pf(5.0), nil))
	if r.Status != domain.AlertRed {
		t.Fatalf("RSI 75 + dist 5 → expected RED, got %s", r.Status)
	}
}

func TestPriority_AmberBeatsGreen(t *testing.T) {
	// RSI 65 (AMBER) + R/R 2.5 + golden + low RSI signal: ambiguous
	// because RSI can't be both 65 and < 40. Use SL-distance AMBER
	// alongside a fully-green-eligible row instead.
	h := hold(pf(35), nil, pf(100), nil, pf(105), pb(true))
	r := Compute(h, met(pf(5.0), pf(2.5)))
	if r.Status != domain.AlertAmber {
		t.Fatalf("AMBER + green-eligible → expected AMBER, got %s", r.Status)
	}
}

func TestProximityMarginConstant(t *testing.T) {
	// Sanity: the published constant is 5%.
	if ProximityMargin != 0.05 {
		t.Fatalf("ProximityMargin should be 0.05, got %v", ProximityMargin)
	}
}
