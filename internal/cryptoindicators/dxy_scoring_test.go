package cryptoindicators

import "testing"

// FT-114: DXY gap rows and the reorder that makes the -1.0 row reachable.
func TestDXYScoring_GapRowsAndReorder(t *testing.T) {
	def := DefsByID["pal_dxy"]
	f := func(v float64) *float64 { return &v }
	for _, c := range []struct {
		name         string
		level, trend float64
		want         float64
	}{
		{"97 rising (gap row)", 97, 2, 0.0},
		{"107 falling (gap row)", 107, -2, -0.25},
		{"107 flat (gap row)", 107, 0, -0.5},
		{"107 rising reaches -1.0", 107, 2, -1.0},
		{"105 rising", 105, 2, -1.0},
		{"102 rising (existing)", 102, 2, -0.5},
		{"100 rising (existing)", 100, 2, -0.5},
		{"99 falling (existing)", 99, -2, 1.0},
		{"103 falling (existing)", 103, -2, 0.5},
		{"103 flat (existing, today)", 103.17, 0, 0.0},
	} {
		got, ok := def.Score(ScoringInputs{Value: f(c.level), Trend4w: f(c.trend)})
		if !ok || got != c.want {
			t.Errorf("%s: got %v ok=%v, want %v", c.name, got, ok, c.want)
		}
	}
}
