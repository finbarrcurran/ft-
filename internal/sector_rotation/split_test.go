package sector_rotation

import (
	"context"
	"path/filepath"
	"testing"

	"ft/internal/market"
	"ft/internal/store"
)

func TestFindStep(t *testing.T) {
	if _, _, ok := FindStep([]float64{100, 102, 99, 101, 95}); ok {
		t.Fatal("normal moves must not read as a split")
	}
	i, r, ok := FindStep([]float64{450.41, 131.9, 136.53})
	if !ok || i != 1 || r > 0.3 {
		t.Fatalf("SOXX-style 3:1 step not found: %d %.3f %v", i, r, ok)
	}
	if _, _, ok := FindStep([]float64{50, 140}); !ok {
		t.Fatal("a reverse split step must be found")
	}
	if _, _, ok := FindStep([]float64{100}); ok {
		t.Fatal("one point has no step")
	}
}

// FT-081: a step in the stored series is repaired from the adjusted history; a clean series is left alone;
// a dry run writes nothing.
func TestRepairSplits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sectors, err := st.ListSectorUniverse(ctx)
	if err != nil || len(sectors) == 0 {
		t.Fatalf("no seeded sectors: %v", err)
	}
	sec := sectors[0]
	if _, err := st.DB.Exec(`DELETE FROM sector_snapshots`); err != nil {
		t.Fatal(err)
	}
	// 2026-11-01 is a Sunday: stored by carrying the Friday close forward, so Yahoo has no bar for it.
	stored := map[string]float64{"2026-10-30": 471.51, "2026-11-01": 471.51, "2026-11-03": 465.0, "2026-11-04": 450.41, "2026-11-05": 131.9, "2026-11-06": 136.53}
	for d, c := range stored {
		if err := st.InsertSectorSnapshot(ctx, sec.ID, d, c, nil, 735, nil, "yahoo"); err != nil {
			t.Fatal(err)
		}
	}
	adjusted := []market.DailyClose{{Date: "2026-10-30", Close: 157.17}, {Date: "2026-11-03", Close: 155.0}, {Date: "2026-11-04", Close: 150.14},
		{Date: "2026-11-05", Close: 131.9}, {Date: "2026-11-06", Close: 136.53}}
	src := func(_ context.Context, _, _ string) ([]market.DailyClose, error) { return adjusted, nil }

	dry := RepairSplits(ctx, st, src, false)
	if len(dry.Changes) != 4 || dry.Applied {
		t.Fatalf("dry run: %+v", dry)
	}
	snaps, _ := st.ListRecentSnapshots(ctx, sec.ID, 800)
	if snaps[0].ClosePrimary != 471.51 {
		t.Fatalf("dry run wrote: %.2f", snaps[0].ClosePrimary)
	}
	done := RepairSplits(ctx, st, src, true)
	if !done.Applied || len(done.Changes) != 4 {
		t.Fatalf("apply: %+v", done)
	}
	snaps, _ = st.ListRecentSnapshots(ctx, sec.ID, 800)
	closes := make([]float64, len(snaps))
	for i, s := range snaps {
		closes[i] = s.ClosePrimary
	}
	if _, _, ok := FindStep(closes); ok {
		t.Fatalf("step remains after repair: %v", closes)
	}
	if again := RepairSplits(ctx, st, src, true); len(again.TickersWithStep) != 0 || len(again.Changes) != 0 {
		t.Fatalf("second pass must be a no-op: %+v", again)
	}
}
