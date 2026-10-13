package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestClassifyStatementStart(t *testing.T) {
	cov := BuildCoverage("2025-01-01", "2026-10-09", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if !cov.Has || cov.End != "2026-10-09" || cov.NextStart != "2026-10-10" || cov.AgeDays != 4 || cov.Stale {
		t.Fatalf("coverage: %+v", cov)
	}
	gap := ClassifyStatementStart(cov, "2026-10-15")
	if gap.Kind != "gap" || gap.MissingFrom != "2026-10-10" || gap.MissingTo != "2026-10-14" {
		t.Fatalf("gap: %+v", gap)
	}
	if ov := ClassifyStatementStart(cov, "2026-10-05"); ov.Kind != "overlap" {
		t.Fatalf("overlap: %+v", ov)
	}
	if cl := ClassifyStatementStart(cov, "2026-10-10"); cl.Kind != "clean" {
		t.Fatalf("clean: %+v", cl)
	}
	if f := ClassifyStatementStart(EtoroCoverage{}, "2026-01-01"); f.Kind != "first" {
		t.Fatalf("first: %+v", f)
	}
	// Staleness: exactly 35 days is still fine, 36 is stale.
	if BuildCoverage("2026-01-01", "2026-09-01", time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)).Stale {
		t.Fatal("35 days must not be stale")
	}
	if !BuildCoverage("2026-01-01", "2026-09-01", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)).Stale {
		t.Fatal("36 days must be stale")
	}
}

// Coverage comes from the stored (non-superseded) periods, not from the newest close date.
func TestCoverageFromStoredPeriods(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	if c, _ := st.EtoroCoverageFor(ctx, 1, now); c.Has {
		t.Fatal("no coverage before any import")
	}
	_, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "a.xlsx",
		Positions: []EtoroPosition{pos("1", "2026-02-03", 2026, "Stocks", false, 10, 9)},
		Period:    period("2026-01-01", "2026-05-31", 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "b.xlsx",
		Positions: []EtoroPosition{pos("2", "2026-06-03", 2026, "Stocks", false, 5, 4.5)},
		Period:    period("2026-06-01", "2026-10-09", 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.EtoroCoverageFor(ctx, 1, now)
	if err != nil || c.Start != "2026-01-01" || c.End != "2026-10-09" || c.NextStart != "2026-10-10" {
		t.Fatalf("coverage: %+v %v", c, err)
	}
}

// Two tiled statements must leave the year the same as one statement over the same dates.
func TestTiledImportMatchesSingleStatement(t *testing.T) {
	ctx := context.Background()
	positions := func() []EtoroPosition {
		return []EtoroPosition{
			pos("1", "2026-02-03", 2026, "Stocks", false, 100.5, 90),
			pos("2", "2026-03-04", 2026, "Stocks", true, -40.25, -36),
			pos("3", "2026-07-01", 2026, "ETFs", false, 20, 18),
			pos("4", "2026-09-09", 2026, "CFDs", true, -5, -4.5),
		}
	}
	single := openStatementDB(t)
	if _, err := single.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "all.xlsx", Positions: positions(),
		Dividends: []EtoroDividend{{Key: "d1", PayDate: "2026-03-10", Year: 2026, AssetType: "Stocks", USD: 5, EUR: 4.5}, {Key: "d2", PayDate: "2026-08-10", Year: 2026, AssetType: "Stocks", USD: 7, EUR: 6}},
		Period:    period("2026-01-01", "2026-10-09", -6, 12)}); err != nil {
		t.Fatal(err)
	}
	tiled := openStatementDB(t)
	p := positions()
	if _, err := tiled.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "h1.xlsx", Positions: p[:2],
		Dividends: []EtoroDividend{{Key: "d1", PayDate: "2026-03-10", Year: 2026, AssetType: "Stocks", USD: 5, EUR: 4.5}},
		Period:    period("2026-01-01", "2026-06-30", -2, 5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := tiled.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "h2.xlsx", Positions: p[2:],
		Dividends: []EtoroDividend{{Key: "d2", PayDate: "2026-08-10", Year: 2026, AssetType: "Stocks", USD: 7, EUR: 6}},
		Period:    period("2026-07-01", "2026-10-09", -4, 7)}); err != nil {
		t.Fatal(err)
	}
	a, b := yearRow(t, single, 2026), yearRow(t, tiled, 2026)
	for name, pair := range map[string][2]float64{
		"realised": {a.RealisedPnLUSD, b.RealisedPnLUSD}, "dividends": {a.DividendsUSD, b.DividendsUSD},
		"fees": {a.FeesUSD, b.FeesUSD}, "interest": {a.InterestUSD, b.InterestUSD}, "net": {a.NetUSD, b.NetUSD},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatalf("%s: single %v vs tiled %v", name, pair[0], pair[1])
		}
	}
	if tradeCount(a) != tradeCount(b) || a.RangeStart != b.RangeStart || a.RangeEnd != b.RangeEnd {
		t.Fatalf("shape differs: %+v vs %+v", a, b)
	}
	// The tiled statements leave no gap and no overlap warning for the second one.
	c, _ := tiled.EtoroCoverageFor(ctx, 1, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if c.End != "2026-10-09" || c.Start != "2026-01-01" {
		t.Fatalf("coverage: %+v", c)
	}
}
