package etoro

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"github.com/xuri/excelize/v2"
)

type actRow struct {
	date, typ, details string
	amt                float64
}

// withFlows adds an Account Activity sheet and the Account Summary flow lines to a synthetic statement.
func withFlows(t *testing.T, base []byte, rows []actRow, summary map[string]float64) []byte {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(base))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := 5
	for _, k := range []string{"Deposits", "Transfer to Trading (from eToro Money)", "Withdrawals", "Transfer to eToro Money (from Trading)", "Deposit/Withdrawal FX Conversion Fee", "Withdrawal Fees", "Internal Transfer", "Credits"} {
		if v, ok := summary[k]; ok {
			f.SetSheetRow("Account Summary", fmt.Sprintf("A%d", r), &[]any{k, fmt.Sprintf("%.2f", v), "N/A"})
			r++
		}
	}
	f.NewSheet("Account Activity")
	f.SetSheetRow("Account Activity", "A1", &[]any{"Date", "Type", "Details", "Amount", "Units / Contracts", "Realized Equity Change", "Realized Equity", "Balance", "Position ID", "Asset type", "NWA"})
	for i, a := range rows {
		f.SetSheetRow("Account Activity", fmt.Sprintf("A%d", i+2), &[]any{a.date, a.typ, a.details, fmt.Sprintf("%.2f", a.amt), "", "", "", "", "", "", ""})
	}
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A statement shaped like the real one: deposits, a transfer in, a withdrawal, transfers out with a
// reversal, and conversion fees. Rows must add up to the Account Summary lines exactly.
func realisticFlows() ([]actRow, map[string]float64) {
	rows := []actRow{
		{"31/08/2025 17:25:06", "Deposit", "100.00 EUR CreditCard", 115.45},
		{"31/08/2025 17:25:06", "Deposit Conversion Fee", "-", -1.5},
		{"16/10/2025 19:54:11", "Deposit", "862.00 EUR eToroMoney", 1000.05},
		{"16/10/2025 19:54:11", "Deposit Conversion Fee", "-", -7.59},
		{"20/10/2025 14:27:43", "Transfer: EUR > USD", "EUR", 2131.86},
		{"04/03/2026 16:56:25", "Withdraw Request", "-", -1001},
		{"03/03/2026 19:30:19", "Transfer: USD > EUR", "EUR", -184.78},
		{"03/03/2026 19:30:19", "Withdrawal Conversion Fee", "-", -0.82},
		{"03/03/2026 19:30:19", "Transfer: USD > EUR", "EUR", -184.78}, // identical row, same second: a separate flow
		{"06/03/2026 08:01:19", "Withdraw Request Cancelled", "EUR", 102.94},
		{"05/03/2026 10:00:00", "Open Position", "AAPL/USD", -50}, // not a flow
		{"05/03/2026 10:00:00", "Dividend", "AAPL", 1.2},          // not a flow
	}
	sum := map[string]float64{
		"Deposits": 1115.50, "Transfer to Trading (from eToro Money)": 2131.86, "Withdrawals": -1001,
		"Transfer to eToro Money (from Trading)": -184.78*2 + 102.94, "Deposit/Withdrawal FX Conversion Fee": -1.5 - 7.59 - 0.82,
	}
	return rows, sum
}

func TestCapitalFlowsParseAndReconcileExactly(t *testing.T) {
	rows, sum := realisticFlows()
	st := parse(t, withFlows(t, multiYear(t), rows, sum))
	if len(st.Flows) != 10 {
		t.Fatalf("flows: %d, want 10 (positions and dividends are not flows)", len(st.Flows))
	}
	if !st.FlowSum.Present || !st.FlowSum.Reconciled {
		t.Fatalf("must reconcile: %+v warnings %v", st.FlowSum, st.Warnings)
	}
	if hasWarning(st, "do not reconcile") {
		t.Fatalf("unexpected warning: %v", st.Warnings)
	}
	kinds := map[string]int{}
	var total float64
	for _, f := range st.Flows {
		kinds[f.Kind]++
		if f.Kind != "fx_fee" {
			total += f.AmountUSD
		}
	}
	if kinds["deposit"] != 2 || kinds["transfer_in"] != 1 || kinds["withdrawal"] != 1 || kinds["transfer_out"] != 2 || kinds["reversal"] != 1 || kinds["fx_fee"] != 3 {
		t.Fatalf("kinds: %v", kinds)
	}
	if math.Abs(total-(1115.50+2131.86-1001-184.78*2+102.94)) > 1e-9 {
		t.Fatalf("net flow: %v", total)
	}
	// Two identical rows in the same second get different keys; a re-parse gives the same keys.
	keys := map[string]bool{}
	for _, f := range st.Flows {
		if keys[f.Key] {
			t.Fatalf("duplicate key %s", f.Key)
		}
		keys[f.Key] = true
	}
	st2 := parse(t, withFlows(t, multiYear(t), rows, sum))
	for i := range st.Flows {
		if st.Flows[i].Key != st2.Flows[i].Key {
			t.Fatal("keys must be stable across parses")
		}
	}
	if st.Flows[0].OccurredAt != "2025-08-31T17:25:06" || st.Flows[0].Day != "2025-08-31" {
		t.Fatalf("date parsing: %+v", st.Flows[0])
	}
}

func TestCapitalFlowsMismatchAndUnknownTypesAreReported(t *testing.T) {
	rows, sum := realisticFlows()
	sum["Deposits"] += 5 // the summary disagrees with the rows
	rows = append(rows, actRow{"01/04/2026 10:00:00", "Credit Adjustment Deposit Bonus", "-", 10})
	st := parse(t, withFlows(t, multiYear(t), rows, sum))
	if st.FlowSum.Reconciled || !hasWarning(st, "do not reconcile on inflows") {
		t.Fatalf("a $5 delta must be flagged: %v", st.Warnings)
	}
	if !hasWarning(st, "unrecognised flow-like activity types") {
		t.Fatalf("an unknown flow-like type must be reported: %v", st.Warnings)
	}
	sum["Credits"] = 3
	if st := parse(t, withFlows(t, multiYear(t), realisticRows(), sum)); !hasWarning(st, `"Credits"`) {
		t.Fatalf("a non-zero Credits line must be reported: %v", st.Warnings)
	}
}

// Fee rows are rounded to the cent by eToro: a couple of cents of drift is not a mismatch,
// a dollar is.
func TestFXFeeRoundingDriftIsTolerated(t *testing.T) {
	rows, sum := realisticFlows()
	sum["Deposit/Withdrawal FX Conversion Fee"] -= 0.02
	if st := parse(t, withFlows(t, multiYear(t), rows, sum)); !st.FlowSum.Reconciled || hasWarning(st, "do not reconcile") {
		t.Fatalf("2 cents of rounding must pass: %v", st.Warnings)
	}
	sum["Deposit/Withdrawal FX Conversion Fee"] -= 1
	if st := parse(t, withFlows(t, multiYear(t), rows, sum)); st.FlowSum.Reconciled || !hasWarning(st, "FX conversion fees") {
		t.Fatalf("a dollar must be flagged: %v", st.Warnings)
	}
}

func realisticRows() []actRow { r, _ := realisticFlows(); return r }

// A statement without the Activity sheet yields no flows and no invented figures.
func TestStatementWithoutActivitySheetHasNoFlows(t *testing.T) {
	st := parse(t, multiYear(t))
	if len(st.Flows) != 0 || st.FlowSum.Present {
		t.Fatalf("%+v", st.FlowSum)
	}
}
