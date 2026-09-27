package etoro

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// statement builds a minimal eToro-shaped workbook for the given range with
// the given still-open positions (ticker/currency, units, amount).
func statement(t *testing.T, start, end string, opens [][3]string) *bytes.Reader {
	t.Helper()
	f := excelize.NewFile()
	f.SetSheetName("Sheet1", sheetAccountSummary)
	f.SetSheetRow(sheetAccountSummary, "A1", &[]any{"Start Date", start})
	f.SetSheetRow(sheetAccountSummary, "A2", &[]any{"End Date", end})
	f.NewSheet(sheetClosedPos)
	f.SetSheetRow(sheetClosedPos, "A1", &[]any{"Position ID", "Action", "ISIN"})
	f.NewSheet(sheetAccountActivity)
	f.SetSheetRow(sheetAccountActivity, "A1", &[]any{"Date", "Type", "Details", "Amount", "Units / Contracts", "Position ID", "Asset type"})
	for i, o := range opens {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		f.SetSheetRow(sheetAccountActivity, cell, &[]any{start, "Open Position", o[0], o[2], o[1], strings.Repeat("9", i+1), "Stocks"})
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestHoldingsCoverage(t *testing.T) {
	full, err := ParseHoldings(statement(t, "01/01/2024 00:00:00", "27/09/2026 23:59:59",
		[][3]string{{"GLD/USD", "2", "900"}, {"RHM.DE/EUR", "0.2", "262.97"}}), "full.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if full.StartDate != "2024-01-01" || full.EndDate != "2026-09-27" || len(full.Holdings) != 2 {
		t.Fatalf("parse: start=%s end=%s holdings=%d", full.StartDate, full.EndDate, len(full.Holdings))
	}
	if ok, why := full.Coverage("2025-03-10", "2026-09-20"); !ok {
		t.Errorf("full-history statement should be complete: %s", why)
	}
	if ok, _ := full.Coverage("", ""); !ok {
		t.Error("no SC-42 data: a non-empty rebuild is accepted")
	}

	// Seen live 2026-09-27: a 1–26 Sep statement rebuilds nothing, silently.
	empty, _ := ParseHoldings(statement(t, "01/09/2026 00:00:00", "26/09/2026 23:59:59", nil), "sep.xlsx")
	if ok, why := empty.Coverage("", ""); ok || !strings.Contains(why, "no open positions") {
		t.Errorf("empty rebuild must be incomplete: %v %q", ok, why)
	}

	late, _ := ParseHoldings(statement(t, "01/06/2026 00:00:00", "27/09/2026 23:59:59",
		[][3]string{{"MU/USD", "1", "200"}}), "late.xlsx")
	if ok, why := late.Coverage("2025-03-10", "2026-09-20"); ok || !strings.Contains(why, "starts 2026-06-01") {
		t.Errorf("start after oldest open lot must be incomplete: %v %q", ok, why)
	}

	stale, _ := ParseHoldings(statement(t, "01/01/2024 00:00:00", "31/05/2026 23:59:59",
		[][3]string{{"GLD/USD", "2", "900"}}), "stale.xlsx")
	if ok, why := stale.Coverage("2025-03-10", "2026-09-20"); ok || !strings.Contains(why, "ends 2026-05-31") {
		t.Errorf("end before newest open lot must be incomplete: %v %q", ok, why)
	}
}
