package store

import (
	"context"
	"path/filepath"
	"testing"
)

// FT-142 follow-up: the backfill only touches dates that already have a history row, only where the value
// differs, never creates rows, and labels its source.
func TestCashBackfillPlanAndApply(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"2026-11-10", "2026-11-11", "2026-11-12"} {
		if _, err := st.DB.Exec(`INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES (?,100,60,40,1)`, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetPortfolioCashSource(ctx, "2026-11-12", 500, "manual"); err != nil {
		t.Fatal(err)
	}
	cash := map[string]float64{"2026-11-11": 450, "2026-11-12": 500, "2026-11-13": 480, "2026-11-01": 9}
	p, err := st.PlanCashBackfill(ctx, cash)
	if err != nil {
		t.Fatal(err)
	}
	// 11-10 has no eToro day; 11-11 is NULL (changes); 11-12 already 500 (no change); 11-13 and 11-01 have no row
	if p.HistoryRows != 3 || p.Matched != 2 || p.Unmatched != 1 || p.Changes != 1 || p.Updates["2026-11-11"] != 450 || len(p.Updates) != 1 {
		t.Fatalf("plan: %+v", p)
	}
	if err := st.ApplyCashBackfill(ctx, p, "etoro_api:balances/history"); err != nil {
		t.Fatal(err)
	}
	var n int
	var v float64
	var src string
	st.DB.QueryRow(`SELECT cash_value_usd, cash_source FROM portfolio_value_history WHERE date='2026-11-11'`).Scan(&v, &src)
	st.DB.QueryRow(`SELECT COUNT(*) FROM portfolio_value_history`).Scan(&n)
	if v != 450 || src != "etoro_api:balances/history" || n != 3 {
		t.Fatalf("after apply: %v %q rows=%d", v, src, n)
	}
	var kept string
	st.DB.QueryRow(`SELECT cash_source FROM portfolio_value_history WHERE date='2026-11-12'`).Scan(&kept)
	if kept != "manual" {
		t.Fatalf("an unchanged row keeps its label, got %q", kept)
	}
}
