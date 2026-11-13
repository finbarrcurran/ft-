package cryptoindicators

import "testing"

// FT-114: the US 2Y table now scores every level/trend pair except an unlisted one; today's 4.73% with a falling
// trend, which matched no row before, scores +0.25.
func TestUS2YScoring_ApprovedRows(t *testing.T) {
	def := DefsByID["pal_us2y"]
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		name         string
		level, trend float64
		want         float64
	}{
		{"4.73 falling (today)", 4.73, -1.25, 0.25},
		{"4.0 falling stays on the existing row", 4.0, -2, 0.5},
		{"5.0 falling", 5.0, -2, 0.25},
		{"5.2 falling", 5.2, -2, -0.5},
		{"5.2 flat", 5.2, 0, -0.5},
		{"2.5 flat", 2.5, 0, 0.5},
		{"3.5 flat (already 0.0)", 3.5, 0, 0.0},
		{"5.0 flat stays on the existing row", 5.0, 0, 0.0},
		{"3.5 rising", 3.5, 2, -0.25},
		{"4.5 rising (existing)", 4.5, 2, -0.5},
		{"2.5 falling (existing)", 2.5, -2, 1.0},
		{"3.5 falling (existing)", 3.5, -2, 0.5},
		{"5.2 rising reaches the -1.0 row (FT-114 reorder)", 5.2, 2, -1.0},
		{"5.0 rising", 5.0, 2, -1.0},
	} {
		got, ok := def.Score(ScoringInputs{Value: f(c.level), Trend4w: f(c.trend)})
		if !ok || got != c.want {
			t.Errorf("%s: got %v ok=%v, want %v", c.name, got, ok, c.want)
		}
	}
}
