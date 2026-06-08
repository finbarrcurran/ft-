package alert

import (
	"strings"
	"testing"

	"ft/internal/domain"
)

func fptr(v float64) *float64 { return &v }

func sh(price, ma50w, ma200d *float64, class string, rsi *float64) *domain.StockHolding {
	return &domain.StockHolding{
		CurrentPrice:  price,
		MA50W:         ma50w,
		MA200D:        ma200d,
		PositionClass: class,
		RSI14:         rsi,
	}
}

func TestTrend_Intact(t *testing.T) {
	h := sh(fptr(100), fptr(90), fptr(80), "hold", nil)
	if got := Trend(h); got != TrendIntact {
		t.Fatalf("price above both MAs → TrendIntact, got %v", got)
	}
}

func TestTrend_Broken(t *testing.T) {
	h := sh(fptr(85), fptr(90), fptr(80), "hold", nil) // below 50W
	if got := Trend(h); got != TrendBroken {
		t.Fatalf("price below 50W → TrendBroken, got %v", got)
	}
}

func TestTrend_UnknownWhenMAMissing(t *testing.T) {
	h := sh(fptr(100), nil, fptr(80), "hold", nil)
	if got := Trend(h); got != TrendUnknown {
		t.Fatalf("missing MA50W → TrendUnknown, got %v", got)
	}
}

// AC #8 — RSI overbought on a trend-intact hold must NOT suggest selling and
// must be flagged as a mute candidate.
func TestDecide_RSIOnTrendIntactHold_NoSell(t *testing.T) {
	h := sh(fptr(100), fptr(90), fptr(80), "hold", fptr(78))
	d := Decide(h, string(domain.AlertRed), []string{"RSI 78 (≥ 75, overbought)"}, TrendIntact)
	if !d.RSIOnlyHold {
		t.Errorf("expected RSIOnlyHold=true (mute candidate)")
	}
	if d.Severity != SeverityInfo {
		t.Errorf("expected info severity, got %q", d.Severity)
	}
	low := strings.ToLower(d.Action)
	for _, banned := range []string{"sell", "trim", "exit", "close"} {
		if strings.Contains(low, banned) {
			t.Errorf("trend-intact hold RSI action must not mention %q: %q", banned, d.Action)
		}
	}
}

// Trend-broken hold: RSI + below-50W is allowed to escalate to a review (amber).
func TestDecide_RSIOnTrendBrokenHold_Review(t *testing.T) {
	h := sh(fptr(85), fptr(90), fptr(80), "hold", fptr(70))
	d := Decide(h, string(domain.AlertAmber), []string{"RSI 70 (65–74, elevated)"}, TrendBroken)
	if d.Severity != SeverityAmber {
		t.Errorf("trend-broken RSI → amber review, got %q (%q)", d.Severity, d.Action)
	}
	if d.RSIOnlyHold {
		t.Errorf("trend-broken RSI should not be a mute candidate")
	}
}

// Trade approaching TP1 gets the scale/raise-SL action (AC #7).
func TestDecide_TP1ProximityTrade_ScaleAction(t *testing.T) {
	h := sh(fptr(100), nil, nil, "trade", nil)
	d := Decide(h, KindTP1Proximity, []string{"within 1.5% of TP1"}, TrendUnknown)
	if d.Severity != SeverityAmber {
		t.Errorf("trade TP1 proximity → amber, got %q", d.Severity)
	}
	if !strings.Contains(strings.ToLower(d.Action), "scale") {
		t.Errorf("trade TP1 action should mention scaling, got %q", d.Action)
	}
}

// Same event on a hold is informational, never a sell.
func TestDecide_TP1ProximityHold_Info(t *testing.T) {
	h := sh(fptr(100), fptr(90), fptr(80), "hold", nil)
	d := Decide(h, KindTP1Proximity, []string{"within 1.5% of TP1"}, TrendIntact)
	if d.Severity != SeverityInfo {
		t.Errorf("hold TP1 proximity → info, got %q (%q)", d.Severity, d.Action)
	}
}
