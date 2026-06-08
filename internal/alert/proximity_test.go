// SC-08 — coverage for the progress-based two-tier proximity family that
// replaced the retired flat-margin SL/TP proximity (see proximity.go).
//
//	AMBER fires at progress ≥ 0.75 (≥ 0.85 when the regime is tightened).
//	RED   fires within 2% of the stop (price ≤ SL×1.02) or the target
//	      (price ≥ TP×0.98) and supersedes amber on the same side.
package alert

import (
	"testing"

	"ft/internal/domain"
	"ft/internal/metrics"
)

func tkr(s string) *string { return &s }

// proxHold builds a minimal holding for ProximityEvents (price + optional TP).
func proxHold(price, tp *float64) *domain.StockHolding {
	return &domain.StockHolding{
		Name:         "TEST",
		Ticker:       tkr("TEST"),
		CurrentPrice: price,
		TakeProfit:   tp,
	}
}

func TestProximity_RedSL_Within2Pct(t *testing.T) {
	// price 101 vs SL 100 → 101 ≤ 102 → red SL (supersedes the amber progress).
	h := proxHold(pf(101), nil)
	m := metrics.StockMetrics{EffectiveSLPrice: pf(100), ProgressToSL: pf(0.95)}
	evs := ProximityEvents(h, m, false)
	if len(evs) != 1 || evs[0].Kind != KindProximityRedSL {
		t.Fatalf("price within 2%% of SL → one red-SL event, got %+v", evs)
	}
	if evs[0].Tone != "red" || evs[0].Side != "sl" {
		t.Errorf("expected red/sl tone+side, got %q/%q", evs[0].Tone, evs[0].Side)
	}
}

func TestProximity_AmberSL_AtThreshold(t *testing.T) {
	// 110 vs SL 100 → not within 2%; progress 0.80 ≥ 0.75 default → amber SL.
	h := proxHold(pf(110), nil)
	m := metrics.StockMetrics{EffectiveSLPrice: pf(100), ProgressToSL: pf(0.80)}
	evs := ProximityEvents(h, m, false)
	if len(evs) != 1 || evs[0].Kind != KindProximityAmberSL {
		t.Fatalf("progress 0.80 → one amber-SL event, got %+v", evs)
	}
}

func TestProximity_AmberSL_TightenedSuppresses(t *testing.T) {
	// Same 0.80 progress, but tightened raises the bar to 0.85 → no event.
	h := proxHold(pf(110), nil)
	m := metrics.StockMetrics{EffectiveSLPrice: pf(100), ProgressToSL: pf(0.80)}
	if evs := ProximityEvents(h, m, true); len(evs) != 0 {
		t.Fatalf("tightened (0.85 bar) should suppress a 0.80 amber, got %+v", evs)
	}
}

func TestProximity_RedTP_Within2Pct(t *testing.T) {
	// price 99 vs TP 100 → 99 ≥ 98 → red TP (no SL metrics → SL side skipped).
	h := proxHold(pf(99), pf(100))
	m := metrics.StockMetrics{ProgressToTP: pf(0.95)}
	evs := ProximityEvents(h, m, false)
	if len(evs) != 1 || evs[0].Kind != KindProximityRedTP {
		t.Fatalf("price within 2%% of TP → one red-TP event, got %+v", evs)
	}
}

func TestProximity_AmberTP_AtThreshold(t *testing.T) {
	// 90 vs TP 100 → not within 2%; progress 0.78 ≥ 0.75 → amber TP.
	h := proxHold(pf(90), pf(100))
	m := metrics.StockMetrics{ProgressToTP: pf(0.78)}
	evs := ProximityEvents(h, m, false)
	if len(evs) != 1 || evs[0].Kind != KindProximityAmberTP {
		t.Fatalf("progress 0.78 → one amber-TP event, got %+v", evs)
	}
}

func TestProximity_NoPrice_NoEvents(t *testing.T) {
	h := proxHold(nil, pf(100))
	if evs := ProximityEvents(h, metrics.StockMetrics{ProgressToTP: pf(0.99)}, false); evs != nil {
		t.Fatalf("nil price → no events, got %+v", evs)
	}
}
