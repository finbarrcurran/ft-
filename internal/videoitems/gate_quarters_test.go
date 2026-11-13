package videoitems

import "testing"

// SC-71 (FT-138): quarter and half-year labels are not tickers, so "Q4" in an item is never looked up as a symbol.
func TestGateQuarterLabelsAreNotTickers(t *testing.T) {
	g := NewGate(nil, words(t, "[0s] Bitcoin tends to put in a low in the fourth quarter and again in the summer."))
	for _, label := range []string{"Q1", "Q2", "Q3", "Q4", "H1", "H2"} {
		v := g.Check(item("seasonality", &Ref{0, 10}, map[string]any{"asset": "Bitcoin", "pattern": "lows in " + label}))
		if v.Class != ClassGrounded {
			t.Fatalf("%s must not be read as a ticker: %+v", label, v)
		}
	}
	// a real symbol that is not spoken is still flagged
	v := g.Check(item("predictions", &Ref{0, 10}, map[string]any{"claim": "NVDA will rally", "outcome": nil}))
	if v.Class == ClassGrounded {
		t.Fatalf("an unspoken real ticker must still be flagged: %+v", v)
	}
}
