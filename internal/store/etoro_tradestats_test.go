package store

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: nil, want %v", name, want)
	}
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s: got %v, want %v", name, *got, want)
	}
}

// Hand-checked set: 4 winners (100,50,30,10 = 190), 3 losers (-40,-20,-5 = -65), 1 break-even.
func synthetic() []Trade {
	return []Trade{
		{AssetType: "Stocks", Year: 2026, ProfitUSD: 100, OpenDate: "2026-01-01", CloseDate: "2026-01-11", AmountUSD: 1000}, // 10 d
		{AssetType: "Stocks", Year: 2026, ProfitUSD: 50, OpenDate: "2026-01-05", CloseDate: "2026-01-06", AmountUSD: 500},   // 1 d
		{AssetType: "Stocks", Year: 2026, ProfitUSD: 30, OpenDate: "2026-02-01", CloseDate: "2026-02-21", AmountUSD: 300},   // 20 d
		{AssetType: "Stocks", Year: 2026, ProfitUSD: -40, OpenDate: "2026-03-01", CloseDate: "2026-03-04", AmountUSD: 400},  // 3 d
		{AssetType: "ETFs", Year: 2026, ProfitUSD: -20, OpenDate: "2026-03-10", CloseDate: "2026-03-20", AmountUSD: 200},    // 10 d
		{AssetType: "ETFs", Year: 2026, ProfitUSD: 0, OpenDate: "2026-04-01", CloseDate: "2026-04-01", AmountUSD: 100},      // 0 d, break-even
		{AssetType: "Crypto", Year: 2026, ProfitUSD: 10, OpenDate: "2026-05-01", CloseDate: "2026-05-06", AmountUSD: 3},     // 5 d, tiny
		{AssetType: "CFDs", Year: 2026, ProfitUSD: -5, OpenDate: "2026-06-01", CloseDate: "2026-06-02", AmountUSD: 50},      // 1 d
	}
}

func TestComputeTradeStatsHandChecked(t *testing.T) {
	s := ComputeTradeStats(synthetic())
	if s.N != 8 || s.Wins != 4 || s.Losses != 3 || s.BreakEven != 1 {
		t.Fatalf("counts: %+v", s)
	}
	near(t, "total", &s.TotalUSD, 125)
	near(t, "winRate", s.WinRate, 0.5) // break-even counts in n
	near(t, "avgWinner", s.AvgWinner, 47.5)
	near(t, "avgLoser", s.AvgLoser, -65.0/3)
	near(t, "payoff", s.Payoff, 47.5/(65.0/3))
	near(t, "profitFactor", s.ProfitFact, 190.0/65.0)
	near(t, "expectancy", s.Expectancy, 125.0/8)
	near(t, "medianHold", s.MedianHold, 4) // 0,1,1,3,5,10,10,20 -> (3+5)/2
	if s.HoldKnown != 8 {
		t.Fatalf("holdKnown %d", s.HoldKnown)
	}
	near(t, "largestWinner", s.BestTrade, 100)
	near(t, "largestLoser", s.WorstTrade, -40)
	near(t, "top5Share", s.Top5Share, 190.0/125) // only 4 winners; losses make it exceed 100%
	if !s.Small {
		t.Fatal("n=8 must be flagged small")
	}
}

func TestComputeTradeStatsEdgeCases(t *testing.T) {
	// Empty scope: no trades, no NaN (JSON would fail on NaN).
	e := ComputeTradeStats(nil)
	if e.N != 0 || e.WinRate != nil || e.Expectancy != nil || e.MedianHold != nil {
		t.Fatalf("empty: %+v", e)
	}
	if _, err := json.Marshal(e); err != nil {
		t.Fatal(err)
	}
	// Only winners: no avg loser, payoff or profit factor.
	w := ComputeTradeStats([]Trade{{ProfitUSD: 10}, {ProfitUSD: 20}})
	if w.AvgLoser != nil || w.Payoff != nil || w.ProfitFact != nil {
		t.Fatalf("winners only: %+v", w)
	}
	near(t, "winRate", w.WinRate, 1)
	// Net loser: top-5 share is undefined.
	l := ComputeTradeStats([]Trade{{ProfitUSD: 10}, {ProfitUSD: -50}})
	if l.Top5Share != nil {
		t.Fatal("top5 share must be nil when total <= 0")
	}
	// 10 trades is not small.
	ten := make([]Trade, 10)
	if ComputeTradeStats(ten).Small {
		t.Fatal("n=10 is not a small sample")
	}
	// Unknown/invalid dates do not feed the hold time.
	h := ComputeTradeStats([]Trade{{ProfitUSD: 1, OpenDate: "", CloseDate: "2026-01-01"}, {ProfitUSD: 1, OpenDate: "2026-02-01", CloseDate: "2026-01-01"}})
	if h.MedianHold != nil || h.HoldKnown != 0 {
		t.Fatalf("hold: %+v", h)
	}
}

func TestBuildTradeStatsScopesYearsAndFilter(t *testing.T) {
	all := append(synthetic(),
		Trade{AssetType: "Stocks", Year: 2026, IsCopy: true, ProfitUSD: -200, AmountUSD: 100},
		Trade{AssetType: "CFDs", Year: 2026, IsCopy: true, ProfitUSD: 25, AmountUSD: 100},
		Trade{AssetType: "Stocks", Year: 2025, IsCopy: true, ProfitUSD: -76, AmountUSD: 100},
	)
	d := BuildTradeStats(all, "", "2026", 0)
	c := BuildTradeStats(all, "copy", "2026", 0)
	a := BuildTradeStats(all, "all", "2026", 0)
	if d.Scope != "discretionary" || d.Stats.N != 8 || c.Stats.N != 2 || a.Stats.N != 10 {
		t.Fatalf("scope counts: %d %d %d", d.Stats.N, c.Stats.N, a.Stats.N)
	}
	if math.Abs(d.Stats.TotalUSD+c.Stats.TotalUSD-a.Stats.TotalUSD) > 1e-9 {
		t.Fatal("discretionary + copy must equal all")
	}
	// 2025 discretionary has no trades at all.
	if r := BuildTradeStats(all, "discretionary", "2025", 0); r.Stats.N != 0 {
		t.Fatalf("2025 discretionary should be empty: %+v", r.Stats)
	}
	// All years: by-year breakdown present and sums to the total.
	ally := BuildTradeStats(all, "all", "all", 0)
	if len(ally.ByYear) != 2 || ally.ByYear[0].Year != 2025 {
		t.Fatalf("byYear: %+v", ally.ByYear)
	}
	if got := ally.ByYear[0].Stats.TotalUSD + ally.ByYear[1].Stats.TotalUSD; math.Abs(got-ally.Stats.TotalUSD) > 1e-9 {
		t.Fatal("year breakdown must sum to total")
	}
	if len(d.ByYear) != 0 {
		t.Fatal("single-year view has no by-year breakdown")
	}
	// Asset breakdown in fixed order and sums to the total.
	if len(d.ByAsset) != 4 || d.ByAsset[0].AssetType != "Stocks" || d.ByAsset[3].AssetType != "CFDs" {
		t.Fatalf("byAsset: %+v", d.ByAsset)
	}
	sum := 0.0
	for _, x := range d.ByAsset {
		sum += x.Stats.TotalUSD
	}
	if math.Abs(sum-d.Stats.TotalUSD) > 1e-9 {
		t.Fatal("asset breakdown must sum to total")
	}
	// Small-position filter: drops the $3 crypto trade only, and says so.
	f := BuildTradeStats(all, "discretionary", "2026", 10)
	if f.Stats.N != 7 || f.ExcludedN != 1 || f.ExcludedUSD != 10 {
		t.Fatalf("filter: n=%d excl=%d/%v", f.Stats.N, f.ExcludedN, f.ExcludedUSD)
	}
	// Unknown amounts are kept and counted.
	u := BuildTradeStats([]Trade{{Year: 2026, ProfitUSD: 5}}, "discretionary", "2026", 100)
	if u.Stats.N != 1 || u.AmountUnknown != 1 || u.ExcludedN != 0 {
		t.Fatalf("unknown amount: %+v", u)
	}
}

// The per-trade sums must equal the closed-trade P&L the year table shows for the same
// scope and period, using the real store path (import, then both readers).
func TestTradeStatsReconcileWithYearTable(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	p := func(id, date string, y int, typ string, copy bool, usd float64, open string, amt float64) EtoroPosition {
		x := pos(id, date, y, typ, copy, usd, usd*0.9)
		x.OpenDate, x.AmountUSD = open, amt
		return x
	}
	_, err := st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{
		FileName: "a.xlsx",
		Positions: []EtoroPosition{
			p("1", "2026-02-03", 2026, "Stocks", false, 100.25, "2026-01-20", 500),
			p("2", "2026-03-04", 2026, "Stocks", false, -40.10, "2026-03-01", 300),
			p("3", "2026-03-05", 2026, "CFDs", false, 7.5, "2026-03-05", 50),
			p("4", "2026-04-04", 2026, "Stocks", true, -60.40, "2026-04-01", 100),
			p("5", "2026-04-05", 2026, "CFDs", true, 12, "2026-04-02", 100),
			p("6", "2025-05-05", 2025, "Stocks", true, -9, "2025-05-01", 100),
		},
		Period: period("2026-01-01", "2026-06-30", -3, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	row := yearRow(t, st, 2026)
	var disc, cp float64
	for _, a := range row.Assets {
		disc += a.RealisedDiscUSD
		cp += a.RealisedCopyUSD
	}
	for scope, want := range map[string]float64{"discretionary": disc, "copy": cp, "all": row.RealisedPnLUSD} {
		r, err := st.EtoroTradeStats(ctx, 1, scope, "2026", 0)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(r.Stats.TotalUSD-want) > 1e-9 {
			t.Fatalf("%s: trades %v vs year table %v", scope, r.Stats.TotalUSD, want)
		}
	}
	// The year table's scoped trade counts come from the same positions.
	var nd, nc int
	for _, a := range row.Assets {
		nd += a.TradeCountDisc
		nc += a.TradeCountCopy
		if a.TradeCountDisc+a.TradeCountCopy != a.TradeCount {
			t.Fatalf("%s: scoped counts %d+%d != %d", a.AssetType, a.TradeCountDisc, a.TradeCountCopy, a.TradeCount)
		}
	}
	if nd != 3 || nc != 2 {
		t.Fatalf("scoped counts: disc %d copy %d", nd, nc)
	}
	r, _ := st.EtoroTradeStats(ctx, 1, "discretionary", "2026", 0)
	if r.Stats.N != 3 || r.AmountUnknown != 0 {
		t.Fatalf("discretionary: %+v", r.Stats)
	}
	near(t, "median hold", r.Stats.MedianHold, 3) // 14 d, 3 d, 0 d -> 3

	// Re-importing without dates must not erase the stored open date/amount.
	_, err = st.ApplyEtoroStatement(ctx, 1, EtoroStatementData{FileName: "b.xlsx",
		Positions: []EtoroPosition{pos("1", "2026-02-03", 2026, "Stocks", false, 100.25, 90.225)},
		Period:    period("2026-01-01", "2026-06-30", -3, 10)})
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := st.EtoroTradeStats(ctx, 1, "discretionary", "2026", 0)
	if r2.Stats.HoldKnown != 3 {
		t.Fatalf("open date erased by a dateless re-import: holdKnown=%d", r2.Stats.HoldKnown)
	}
}
