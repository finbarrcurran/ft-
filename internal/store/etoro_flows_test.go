package store

import (
	"context"
	"math"
	"testing"
)

func flowStmt(file string, flows ...EtoroFlow) EtoroStatementData {
	return EtoroStatementData{FileName: file, Flows: flows, Period: period("2025-07-01", "2026-10-09", 0, 0), FlowsJSON: `{"present":true}`}
}

func TestCapitalFlowsAreIdempotentAndSummarised(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	f := []EtoroFlow{
		{Key: "a1", OccurredAt: "2025-08-31T17:25:06", Day: "2025-08-31", Kind: "deposit", AmountUSD: 115.45},
		{Key: "a2", OccurredAt: "2025-10-16T19:54:11", Day: "2025-10-16", Kind: "deposit", AmountUSD: 1000.05},
		{Key: "a3", OccurredAt: "2025-10-20T14:27:43", Day: "2025-10-20", Kind: "transfer_in", AmountUSD: 2131.86},
		{Key: "a4", OccurredAt: "2026-03-04T16:56:25", Day: "2026-03-04", Kind: "withdrawal", AmountUSD: -1001},
		{Key: "a5", OccurredAt: "2026-03-03T19:30:19", Day: "2026-03-03", Kind: "transfer_out", AmountUSD: -300},
		{Key: "a6", OccurredAt: "2026-03-06T08:01:19", Day: "2026-03-06", Kind: "reversal", AmountUSD: 100},
		{Key: "a7", OccurredAt: "2025-08-31T17:25:06", Day: "2025-08-31", Kind: "fx_fee", AmountUSD: -1.5},
	}
	r1, err := st.ApplyEtoroStatement(ctx, 1, flowStmt("a.xlsx", f...))
	if err != nil || r1.FlowsNew != 7 || r1.FlowsKnown != 0 {
		t.Fatalf("first: %+v %v", r1, err)
	}
	// Same statement again, and an overlapping one carrying two of the same rows plus one new.
	r2, _ := st.ApplyEtoroStatement(ctx, 1, flowStmt("a.xlsx", f...))
	r3, _ := st.ApplyEtoroStatement(ctx, 1, flowStmt("b.xlsx", f[0], f[3], EtoroFlow{Key: "b9", OccurredAt: "2026-06-01T10:00:00", Day: "2026-06-01", Kind: "transfer_in", AmountUSD: 174.46}))
	if r2.FlowsNew != 0 || r2.FlowsKnown != 7 || r3.FlowsNew != 1 || r3.FlowsKnown != 2 {
		t.Fatalf("idempotence: %+v %+v", r2, r3)
	}
	sum, err := st.CapitalFlowSummary(ctx, 1)
	if err != nil || !sum.Has || sum.First != "2025-08-31" || sum.Last != "2026-06-01" {
		t.Fatalf("summary: %+v %v", sum, err)
	}
	wantIn := 115.45 + 1000.05 + 2131.86 + 174.46
	wantOut := -1001.0 - 300 + 100
	if math.Abs(sum.InUSD-wantIn) > 1e-9 || math.Abs(sum.OutUSD-wantOut) > 1e-9 || math.Abs(sum.NetUSD-(wantIn+wantOut)) > 1e-9 || sum.FeesUSD != -1.5 {
		t.Fatalf("totals: %+v want in %v out %v", sum, wantIn, wantOut)
	}
	if len(sum.ByYear) != 2 || sum.ByYear[0].Year != 2025 || math.Abs(sum.ByYear[0].InUSD-(115.45+1000.05+2131.86)) > 1e-9 || sum.ByYear[1].Year != 2026 {
		t.Fatalf("by year: %+v", sum.ByYear)
	}
	// The reconciliation record is kept with the period.
	var js string
	if err := st.DB.QueryRow(`SELECT flows_summary_json FROM etoro_statement_periods ORDER BY id LIMIT 1`).Scan(&js); err != nil || js == "" {
		t.Fatalf("flows_summary_json: %q %v", js, err)
	}
}

func TestManualCapitalFlowFallback(t *testing.T) {
	ctx := context.Background()
	st := openStatementDB(t)
	id, err := st.AddManualFlow(ctx, 1, "2026-10-12", "out", 500, "withdrawal after the last statement")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct{ day, dir string; amt float64 }{{"12/10/2026", "out", 1}, {"2026-10-12", "sideways", 1}, {"2026-10-12", "in", 0}, {"2026-10-12", "in", -5}} {
		if _, err := st.AddManualFlow(ctx, 1, bad.day, bad.dir, bad.amt, ""); err == nil {
			t.Errorf("%+v must be refused", bad)
		}
	}
	sum, _ := st.CapitalFlowSummary(ctx, 1)
	if sum.OutUSD != -500 || sum.Manual != 1 {
		t.Fatalf("manual row: %+v", sum)
	}
	// A statement row cannot be deleted through the manual path; the manual one can.
	if _, err := st.ApplyEtoroStatement(ctx, 1, flowStmt("a.xlsx", EtoroFlow{Key: "s1", OccurredAt: "2026-03-04T16:56:25", Day: "2026-03-04", Kind: "withdrawal", AmountUSD: -1001})); err != nil {
		t.Fatal(err)
	}
	fl, _ := st.ListCapitalFlows(ctx, 1, "2026-03-01", "2026-03-31")
	if len(fl) != 1 {
		t.Fatalf("date filter: %+v", fl)
	}
	if ok, _ := st.DeleteManualFlow(ctx, 1, fl[0].ID); ok {
		t.Fatal("a statement row must not be deletable")
	}
	if ok, err := st.DeleteManualFlow(ctx, 1, id); err != nil || !ok {
		t.Fatalf("manual delete: %v %v", ok, err)
	}
}
