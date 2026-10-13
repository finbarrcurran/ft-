package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openStatementDB(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func pos(id, date string, year int, typ string, copy bool, usd, eur float64) EtoroPosition {
	return EtoroPosition{PositionID: id, CloseDate: date, Year: year, AssetType: typ, IsCopy: copy, ProfitUSD: usd, ProfitEUR: eur}
}

func period(start, end string, fees, interest float64) EtoroPeriod {
	return EtoroPeriod{RangeStart: start, RangeEnd: end, FeesUSD: fees, FeesEUR: fees * 0.9, InterestUSD: interest, InterestEUR: interest * 0.9,
		FeesByType: map[string][2]float64{}, LinesJSON: "[]"}
}

func yearRow(t *testing.T, st *Store, y int) EtoroYearRow {
	t.Helper()
	rows, err := st.ListEtoroPerformance(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Year == y {
			return r
		}
	}
	t.Fatalf("no live row for %d", y)
	return EtoroYearRow{}
}

func tradeCount(r EtoroYearRow) int {
	n := 0
	for _, a := range r.Assets {
		n += a.TradeCount
	}
	return n
}

// Re-importing the same statement, or an overlapping one, must not double-count.
func TestEtoroStatementIdempotentAndOverlapSafe(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	stmt := EtoroStatementData{
		FileName: "a.xlsx",
		Positions: []EtoroPosition{
			pos("1", "2026-02-03", 2026, "Stocks", false, 100, 90),
			pos("2", "2026-03-04", 2026, "Stocks", true, 50, 45),
		},
		Dividends: []EtoroDividend{{Key: "d1", PayDate: "2026-03-10", Year: 2026, AssetType: "Stocks", USD: 5, EUR: 4.5}},
		Period:    period("2026-01-01", "2026-06-30", -3, 10),
	}
	r1, err := st.ApplyEtoroStatement(ctx, 1, stmt)
	if err != nil || r1.PositionsNew != 2 || r1.DividendsNew != 1 {
		t.Fatalf("first import: %+v %v", r1, err)
	}
	want := yearRow(t, st, 2026)
	if want.RealisedPnLUSD != 150 || tradeCount(want) != 2 || want.DividendsUSD != 5 || want.FeesUSD != -3 || want.InterestUSD != 10 || want.NetUSD != 162 {
		t.Fatalf("derived year wrong: %+v", want)
	}

	// Same file again.
	r2, err := st.ApplyEtoroStatement(ctx, 1, stmt)
	if err != nil || r2.PositionsNew != 0 || r2.PositionsKnown != 2 || r2.DividendsNew != 0 || r2.PeriodAction != "replaced_identical" {
		t.Fatalf("re-import must add nothing: %+v %v", r2, err)
	}
	got := yearRow(t, st, 2026)
	if got.RealisedPnLUSD != want.RealisedPnLUSD || got.NetUSD != want.NetUSD || tradeCount(got) != 2 || got.InterestUSD != 10 {
		t.Fatalf("re-import changed the numbers: %+v vs %+v", got, want)
	}

	// An overlapping statement (Mar-Sep) that repeats position 2 and adds position 3: no double count.
	over := EtoroStatementData{
		FileName: "b.xlsx",
		Positions: []EtoroPosition{
			pos("2", "2026-03-04", 2026, "Stocks", true, 50, 45),
			pos("3", "2026-08-01", 2026, "ETFs", false, 20, 18),
		},
		Dividends: []EtoroDividend{{Key: "d1", PayDate: "2026-03-10", Year: 2026, AssetType: "Stocks", USD: 5, EUR: 4.5}},
		Period:    period("2026-03-01", "2026-09-30", -1, 2),
	}
	r3, err := st.ApplyEtoroStatement(ctx, 1, over)
	if err != nil || r3.PositionsNew != 1 || r3.PositionsKnown != 1 || r3.DividendsNew != 0 {
		t.Fatalf("overlap import: %+v %v", r3, err)
	}
	if len(r3.Warnings) == 0 {
		t.Fatal("a partial overlap of the whole-period lines must be flagged")
	}
	g := yearRow(t, st, 2026)
	if tradeCount(g) != 3 || g.RealisedPnLUSD != 170 || g.DividendsUSD != 5 {
		t.Fatalf("overlap double-counted trades or dividends: %+v", g)
	}
}

// A narrower statement must not erase a wider earlier one (the bug behind the live 2026 row).
func TestEtoroNarrowerStatementDoesNotEraseWiderYear(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	// Legacy-style wide row: the year was imported before position-level storage existed.
	if _, err := st.DB.Exec(`INSERT INTO etoro_performance_year (user_id, year, range_start, range_end, is_ytd, net_usd, source_file, imported_at)
		VALUES (1, 2025, '2025-01-01', '2025-12-31', 0, 999, 'full-2025.xlsx', 1)`); err != nil {
		t.Fatal(err)
	}
	r, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName:  "second-half.xlsx",
		Positions: []EtoroPosition{pos("9", "2025-09-01", 2025, "Stocks", false, 10, 9)},
		Period:    period("2025-07-01", "2025-12-31", 0, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	var out *EtoroYearOutcome
	for i := range r.Years {
		if r.Years[i].Year == 2025 {
			out = &r.Years[i]
		}
	}
	if out == nil || out.Action != "kept_existing" {
		t.Fatalf("a half-year statement must not replace a full-year row: %+v", r.Years)
	}
	if g := yearRow(t, st, 2025); g.NetUSD != 999 || g.SourceFile != "full-2025.xlsx" {
		t.Fatalf("the full-year row was overwritten: %+v", g)
	}
}

// Statements that together cover a year replace the partial result; a statement that
// starts at 1 January covers the current year (YTD).
func TestEtoroCoverageBuildsUpAcrossStatements(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	a, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName:  "h2.xlsx",
		Positions: []EtoroPosition{pos("1", "2024-09-01", 2024, "Stocks", false, 10, 9)},
		Period:    period("2024-07-01", "2024-12-31", 0, 0),
	})
	if err != nil || a.Years[0].Action != "created_partial" {
		t.Fatalf("no row existed: partial row expected: %+v %v", a, err)
	}
	if g := yearRow(t, st, 2024); g.CoverageNote == "" || tradeCount(g) != 1 {
		t.Fatalf("partial row must say so: %+v", g)
	}
	b, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName:  "h1.xlsx",
		Positions: []EtoroPosition{pos("2", "2024-03-01", 2024, "Stocks", false, 5, 4.5)},
		Period:    period("2024-01-01", "2024-06-30", 0, 0), // adjacent to the first: together they cover 2024
	})
	if err != nil || b.Years[0].Action != "replaced" {
		t.Fatalf("both halves together must replace the partial row: %+v %v", b, err)
	}
	if g := yearRow(t, st, 2024); g.CoverageNote != "" || tradeCount(g) != 2 || g.RealisedPnLUSD != 15 {
		t.Fatalf("complete year expected: %+v", g)
	}
}

// A statement fully inside an earlier one adds its positions but its whole-period lines are
// not counted again.
func TestEtoroContainedPeriodNotCountedTwice(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	if _, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName: "wide.xlsx", Period: period("2023-01-01", "2023-12-31", -10, 20),
		Positions: []EtoroPosition{pos("1", "2023-05-05", 2023, "Stocks", false, 1, 1)},
	}); err != nil {
		t.Fatal(err)
	}
	r, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName: "inside.xlsx", Period: period("2023-03-01", "2023-06-30", -4, 8),
		Positions: []EtoroPosition{pos("2", "2023-04-04", 2023, "Stocks", false, 2, 2)},
	})
	if err != nil || r.PeriodAction != "contained_in_existing" {
		t.Fatalf("contained period must be recorded as superseded: %+v %v", r, err)
	}
	g := yearRow(t, st, 2023)
	if g.FeesUSD != -10 || g.InterestUSD != 20 || tradeCount(g) != 2 {
		t.Fatalf("whole-period lines double counted or trades lost: %+v", g)
	}
}
