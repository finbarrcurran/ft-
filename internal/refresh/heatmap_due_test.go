package refresh

import (
	"testing"
	"time"
)

func TestHeatmapDueRules(t *testing.T) {
	s := &Service{hm: map[string]*hmState{}}
	t0 := time.Date(2026, 10, 13, 14, 30, 0, 0, time.UTC)

	if !s.heatmapDue("US", true, t0) {
		t.Fatal("first run (startup) must populate the heatmap")
	}
	s.heatmapDone("US", true, t0)
	if s.heatmapDue("US", true, t0.Add(15*time.Minute)) {
		t.Fatal("open exchange: next 15-minute cycle must skip (every 30 min)")
	}
	if !s.heatmapDue("US", true, t0.Add(30*time.Minute)) {
		t.Fatal("open exchange: due again after 30 minutes")
	}
	s.heatmapDone("US", true, t0.Add(30*time.Minute))
	// market closes: exactly one final pass, then nothing until it reopens
	if !s.heatmapDue("US", false, t0.Add(45*time.Minute)) {
		t.Fatal("after the close one last refresh must capture the closing prints")
	}
	s.heatmapDone("US", false, t0.Add(45*time.Minute))
	for i := 1; i <= 20; i++ {
		if s.heatmapDue("US", false, t0.Add(45*time.Minute).Add(time.Duration(i)*15*time.Minute)) {
			t.Fatal("closed exchange must keep its last values (no refresh)")
		}
	}
	if !s.heatmapDue("US", true, t0.Add(24*time.Hour)) && s.hm["US"].whileOpen {
		t.Fatal("unexpected state")
	}
	// next morning it reopens: due again
	if !s.heatmapDue("US", true, t0.Add(24*time.Hour)) {
		t.Fatal("reopened exchange must refresh")
	}
	// other exchanges are independent
	if !s.heatmapDue("LSE", false, t0) {
		t.Fatal("first run per exchange populates")
	}
}
