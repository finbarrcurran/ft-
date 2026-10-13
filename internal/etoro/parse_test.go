package etoro

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// fixture builds a SYNTHETIC eToro statement (no real data). Closed positions and dividends are
// given as rows; Financial Summary lines as label/usd/eur triples.
type fixPos struct {
	id, closeDate, typ, copied string
	usd, eur                   float64
}
type fixDiv struct {
	id, date, name, typ string
	usd, eur            float64
}
type fixLine struct {
	label    string
	usd, eur float64
}

func buildStatement(t *testing.T, start, end string, pos []fixPos, divs []fixDiv, lines []fixLine) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Account Summary")
	f.SetSheetRow("Account Summary", "A1", &[]any{"Details"})
	f.SetSheetRow("Account Summary", "A2", &[]any{"Name", "Totals"})
	f.SetSheetRow("Account Summary", "A3", &[]any{"Start Date", start + " 00:00:00"})
	f.SetSheetRow("Account Summary", "A4", &[]any{"End Date", end + " 23:59:59"})
	f.NewSheet("Closed Positions")
	f.SetSheetRow("Closed Positions", "A1", &[]any{"Position ID", "Open Date", "Close Date", "Profit(USD)", "Profit(EUR)", "Copied From", "Type", "Amount"})
	for i, p := range pos {
		cf := "-"
		if p.copied != "" {
			cf = p.copied
		}
		f.SetSheetRow("Closed Positions", fmt.Sprintf("A%d", i+2), &[]any{p.id, "01/01/2025 10:00:00", p.closeDate + " 12:00:00", p.usd, p.eur, cf, p.typ, "250.50"})
	}
	f.NewSheet("Dividends")
	f.SetSheetRow("Dividends", "A1", &[]any{"Date of Payment", "Instrument Name", "Net Dividend Received (USD)", "Net Dividend Received (EUR)", "Position ID", "Type"})
	for i, d := range divs {
		f.SetSheetRow("Dividends", fmt.Sprintf("A%d", i+2), &[]any{d.date + " 00:00:00", d.name, d.usd, d.eur, d.id, d.typ})
	}
	f.NewSheet("Financial Summary")
	f.SetSheetRow("Financial Summary", "A1", &[]any{"Name", "Amount in (USD)", "Amount in (EUR)"})
	for i, l := range lines {
		f.SetSheetRow("Financial Summary", fmt.Sprintf("A%d", i+2), &[]any{l.label, fmt.Sprintf("%.2f", l.usd), fmt.Sprintf("%.2f", l.eur)})
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ddmm(iso string) string { return iso[8:10] + "/" + iso[5:7] + "/" + iso[0:4] }

// the multi-year fixture, shaped like a 2025-07-01 -> 2026-10-09 statement: the $299.52 of
// interest ($199.52) and refunds ($100.00) that used to sit outside the comparison is in it.
func multiYear(t *testing.T) []byte {
	pos := []fixPos{
		{"1001", ddmm("2025-08-10"), "Stocks", "", 100.00, 90.00},
		{"1002", ddmm("2025-12-20"), "Crypto", "somecopytrader", -20.00, -18.00},
		{"1003", ddmm("2026-02-03"), "Stocks", "", 150.00, 135.00},
		{"1004", ddmm("2026-09-30"), "ETF", "", 14.50, 13.05},
	}
	divs := []fixDiv{
		{"1001", ddmm("2025-11-15"), "ACME", "Stocks", 40.00, 36.00},
		{"1003", ddmm("2026-03-10"), "ACME", "Stocks", 59.84, 53.86},
	}
	lines := []fixLine{
		{"CFDs (Profit or Loss)", 0, 0},
		{"Crypto (Profit or Loss)", -20.00, -18.00},
		{"Stocks (Profit or Loss)", 250.00, 225.00},
		{"ETFs (Profit or Loss)", 14.50, 13.05},
		{"Stocks Lending", 0, 0},
		{"Total Interest payments by etoro EU", 199.52, 177.94},
		{"Stock and ETF Dividends (Profit)", 99.84, 89.86},
		{"CFD Dividends (Profit or Loss)", 0.86, 0.77},
		{"Index adjustments", 0, 0},
		{"Income from Refunds", 100.00, 89.18},
		{"Spread fee on stocks", -2.32, -2.07},
		{"Fees (overnight, withdrawal, admin)", -4.53, -4.04},
		{"SDRT Charge", -20.69, -18.45},
		{"Income from Airdrops", 0, 0},
		{"Income from Staking", 0, 0},
		{"Income from Corporate Actions", 0, 0},
	}
	return buildStatement(t, "01/07/2025", "09/10/2026", pos, divs, lines)
}

func parse(t *testing.T, b []byte) *Statement {
	t.Helper()
	// buildStatement writes DD/MM/YYYY dates for Account Summary via the callers' strings
	st, err := Parse(bytes.NewReader(b), "synthetic.xlsx", time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func yearOf(st *Statement, y int) *YearPerf {
	for i := range st.Years {
		if st.Years[i].Year == y {
			return &st.Years[i]
		}
	}
	return nil
}

func hasWarning(st *Statement, sub string) bool {
	for _, w := range st.Warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestMultiYearAttributionAndReconciliationCloses(t *testing.T) {
	st := parse(t, multiYear(t))
	y25, y26 := yearOf(st, 2025), yearOf(st, 2026)
	if y25 == nil || y26 == nil || len(st.Years) != 2 {
		t.Fatalf("want one bucket per year (2025, 2026), got %+v", st.Years)
	}
	count := func(y *YearPerf) int {
		n := 0
		for _, a := range y.Assets {
			n += a.TradeCount
		}
		return n
	}
	if count(y25) != 2 || count(y26) != 2 {
		t.Fatalf("positions must land in the year of their own close date: 2025=%d 2026=%d", count(y25), count(y26))
	}
	if hasWarning(st, "attributed") && hasWarning(st, "anyway") {
		t.Fatal("no 'attributed anyway' warning for in-range positions")
	}
	if !hasWarning(st, "spans 2025–2026") || !hasWarning(st, "Income & other") {
		t.Fatalf("the multi-year / whole-period explanation is missing: %v", st.Warnings)
	}
	// Realised: 2025 = 100 - 20 = 80; 2026 = 150 + 14.5 = 164.5.
	if y25.RealisedPnLUSD != 80 || y26.RealisedPnLUSD != 164.5 {
		t.Fatalf("realised by year wrong: %v %v", y25.RealisedPnLUSD, y26.RealisedPnLUSD)
	}
	// Whole-period lines only in the End Date year.
	if y26.InterestUSD != 199.52 || y26.RefundsUSD != 100.00 || y25.InterestUSD != 0 || y25.RefundsUSD != 0 {
		t.Fatalf("income & other must sit in the End Date year: 2025 %+v / 2026 %+v", y25, y26)
	}
	// Dividends are dated: 2025 gets 40, 2026 gets 59.84 + the 0.86 CFD dividend the sheet does not list.
	if y25.DividendsUSD != 40 || y26.DividendsUSD != 60.70 {
		t.Fatalf("dividends by year: %v %v", y25.DividendsUSD, y26.DividendsUSD)
	}
	// Both-currency reconciliation closes ($0.00 and €0.00): the $299.52 gap is classified.
	p := st.Period
	if p.ReconNetDeltaUSD != 0 || p.ReconNetDeltaEUR != 0 || p.ReconPnLDeltaUSD != 0 || p.ReconPnLDeltaEUR != 0 {
		t.Fatalf("reconciliation must close in both currencies: %+v", p)
	}
	if p.UnclassifiedUSD != 0 || hasWarning(st, "Unclassified") {
		t.Fatal("nothing should be unclassified")
	}
	// The years add up to the statement's net.
	sumUSD := y25.NetUSD + y26.NetUSD
	sumEUR := y25.NetEUR + y26.NetEUR
	if abs(sumUSD-p.NetUSD) > 0.005 || abs(sumEUR-p.NetEUR) > 0.005 {
		t.Fatalf("years must add up to the Financial Summary net: %.2f/%.2f vs %.2f/%.2f", sumUSD, sumEUR, p.NetUSD, p.NetEUR)
	}
	if len(st.Positions) != 4 || len(st.Dividends) != 2 {
		t.Fatalf("positions/dividends rows: %d %d", len(st.Positions), len(st.Dividends))
	}
}

func TestUnclassifiedLineIsFlaggedAndClosesOnceClassified(t *testing.T) {
	lines := []fixLine{
		{"Stocks (Profit or Loss)", 10, 9},
		{"Mystery credit from eToro", 5.00, 4.50},
	}
	pos := []fixPos{{"2001", ddmm("2026-02-03"), "Stocks", "", 10, 9}}
	b := buildStatement(t, "01/01/2026", "30/06/2026", pos, nil, lines)
	st := parse(t, b)
	if !hasWarning(st, "Unclassified: Mystery credit from eToro $5.00 / €4.50") {
		t.Fatalf("unknown label must be surfaced, got %v", st.Warnings)
	}
	if st.Period.ReconNetDeltaUSD != 5.00 || st.Period.ReconNetDeltaEUR != 4.50 || st.Period.UnclassifiedUSD != 5.00 {
		t.Fatalf("the unclassified amount must show as the reconciliation delta, not vanish into Net: %+v", st.Period)
	}
	// Classify it: the delta closes and the warning goes away.
	extraComponents["Mystery credit from eToro"] = compOther
	defer delete(extraComponents, "Mystery credit from eToro")
	st = parse(t, b)
	if hasWarning(st, "Unclassified") || st.Period.ReconNetDeltaUSD != 0 || st.Period.OtherIncomeUSD != 5.00 {
		t.Fatalf("once classified the delta must close: %v %+v", st.Warnings, st.Period)
	}
}

func TestUSDOnlyDeltaTriggersAWarningThatNamesTheCurrency(t *testing.T) {
	// EUR agrees; USD is off by $2.00.
	pos := []fixPos{{"3001", ddmm("2026-02-03"), "Stocks", "", 100, 90}}
	lines := []fixLine{{"Stocks (Profit or Loss)", 102.00, 90.00}}
	st := parse(t, buildStatement(t, "01/01/2026", "30/06/2026", pos, nil, lines))
	if !hasWarning(st, "does not reconcile") || !hasWarning(st, "delta $2.00 USD, €0.00 EUR") {
		t.Fatalf("a USD-only delta must warn and say which currency: %v", st.Warnings)
	}
	// Within tolerance in both currencies: silent.
	lines = []fixLine{{"Stocks (Profit or Loss)", 100.40, 90.30}}
	st = parse(t, buildStatement(t, "01/01/2026", "30/06/2026", pos, nil, lines))
	if hasWarning(st, "does not reconcile") {
		t.Fatalf("a sub-$1/€1 delta must not warn: %v", st.Warnings)
	}
}

func TestSingleYearStatementKeepsFinancialSummaryAuthoritative(t *testing.T) {
	pos := []fixPos{{"4001", ddmm("2026-03-03"), "Stocks", "", 100, 90}}
	divs := []fixDiv{{"4001", ddmm("2026-04-01"), "ACME", "Stocks", 5, 4.5}}
	lines := []fixLine{
		{"Stocks (Profit or Loss)", 100, 90},
		{"Stock and ETF Dividends (Profit)", 5, 4.5},
		{"Total Interest payments by etoro EU", 3, 2.7},
		{"SDRT Charge", -1, -0.9},
	}
	st := parse(t, buildStatement(t, "01/01/2026", "30/06/2026", pos, divs, lines))
	if len(st.Years) != 1 || st.Years[0].Year != 2026 {
		t.Fatalf("one year expected: %+v", st.Years)
	}
	y := st.Years[0]
	if y.NetUSD != 107 || y.NetEUR != 96.3 || y.InterestUSD != 3 || !y.IsYTD {
		t.Fatalf("single-year statement must keep the Financial Summary net: %+v", y)
	}
	if hasWarning(st, "spans") {
		t.Fatalf("no multi-year note for a single-year statement: %v", st.Warnings)
	}
}

func TestOutsideWindowAndMissingIDWarnings(t *testing.T) {
	pos := []fixPos{
		{"5001", ddmm("2026-03-03"), "Stocks", "", 10, 9},
		{"5002", ddmm("2025-01-15"), "Stocks", "", 10, 9}, // before the 2026-01-01 start: genuinely outside
		{"", ddmm("2026-04-03"), "Stocks", "", 10, 9},     // no Position ID
	}
	st := parse(t, buildStatement(t, "01/01/2026", "30/06/2026", pos, nil, []fixLine{{"Stocks (Profit or Loss)", 30, 27}}))
	if !hasWarning(st, "1 closed positions have a close date outside this statement's own window") {
		t.Fatalf("genuine outside-window position must warn: %v", st.Warnings)
	}
	if !hasWarning(st, "1 closed positions had no Position ID") {
		t.Fatalf("missing Position ID must warn: %v", st.Warnings)
	}
	if yearOf(st, 2025) == nil {
		t.Fatal("the outside-window position is still counted in the year of its own close date")
	}
}

// Regression: bracketed negatives ("(11.50)") must parse as negative numbers, not 0.
func TestParseNumAccountingNegatives(t *testing.T) {
	cases := map[string]float64{
		"(11.50)": -11.50, "(1,234.56)": -1234.56, "11.50": 11.50, "-6.39": -6.39, "1,384.49": 1384.49,
		"": 0, "-": 0, "N/A": 0, "( 2.00 )": -2.00, "\u22123.5": -3.5, "$12.00": 12, "(abc)": 0,
	}
	for in, want := range cases {
		if got := parseNum(in); got != want {
			t.Errorf("parseNum(%q) = %v, want %v", in, got, want)
		}
	}
}

// A statement whose Closed Positions profits are formatted with brackets must still reconcile.
func TestBracketedNegativeProfitsReconcile(t *testing.T) {
	pos := []fixPos{
		{"7001", ddmm("2026-02-03"), "Stocks", "", 100, 90},
		{"7002", ddmm("2026-03-03"), "Stocks", "", -30, -27},
	}
	b := buildStatement(t, "01/01/2026", "30/06/2026", pos, nil, []fixLine{{"Stocks (Profit or Loss)", 70, 63}})
	// Rewrite the two profit columns of the second row in accounting style, as eToro's file does.
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	style, _ := f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("0.00;(0.00)")})
	_ = f.SetCellValue("Closed Positions", "D3", -30.0)
	_ = f.SetCellValue("Closed Positions", "E3", -27.0)
	_ = f.SetCellStyle("Closed Positions", "D3", "E3", style)
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		t.Fatal(err)
	}
	st := parse(t, out.Bytes())
	if st.Period.ClosedSumUSD != 70 || st.Period.ReconPnLDeltaUSD != 0 || hasWarning(st, "does not reconcile") {
		t.Fatalf("the loss must be counted: closed %.2f delta %.2f warnings %v", st.Period.ClosedSumUSD, st.Period.ReconPnLDeltaUSD, st.Warnings)
	}
}

func strPtr(s string) *string { return &s }

// SC-51: the open date and invested amount are read from the Closed Positions sheet.
func TestClosedPositionOpenDateAndAmount(t *testing.T) {
	st := parse(t, multiYear(t))
	if len(st.Positions) == 0 {
		t.Fatal("no positions")
	}
	for _, p := range st.Positions {
		if p.OpenDate != "2025-01-01" || p.AmountUSD != 250.50 {
			t.Fatalf("position %s: open=%q amount=%v", p.PositionID, p.OpenDate, p.AmountUSD)
		}
	}
}
