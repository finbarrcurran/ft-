package theses

// SC-56 P2 — revision_needed now reads the LAST REPORTED date, so it survives the next date rolling forward.

import (
	"context"
	"testing"
	"time"
)

func (e *testEnv) cal(ticker, next, lastReported, status string) {
	e.t.Helper()
	var n, lr any
	if next != "" {
		n = next
	}
	if lastReported != "" {
		lr = lastReported
	}
	if _, err := e.db.Exec(`INSERT OR REPLACE INTO ticker_calendar (ticker, earnings_date, earnings_status, earnings_source, earnings_fetched_at, last_reported_date, updated_at)
		VALUES (?, ?, ?, 'yahoo_calendar', 1, ?, 1)`, ticker, n, status, lr); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) urgency(ticker string) (urgency, next string) {
	e.t.Helper()
	var n *string
	if err := e.db.QueryRow(`SELECT earnings_urgency, next_earnings_date FROM theses_index WHERE ticker = ? AND status != 'superseded'`, ticker).Scan(&urgency, &n); err != nil {
		e.t.Fatal(err)
	}
	if n != nil {
		next = *n
	}
	return
}

func TestRevisionNeeded_DerivedFromLastReportedDate(t *testing.T) {
	e := newEnv(t)
	seed(e) // AAA, BBB, CCC locked 2026-05-18
	today := time.Now().UTC()
	in := func(days int) string { return today.AddDate(0, 0, days).Format("2006-01-02") }

	// AAA reported after the lock and the next date has already rolled forward: still revision_needed.
	e.cal("AAA", in(90), "2026-10-29", "DATE_ESTIMATED")
	// BBB reported before the lock; the next report is in 2 days.
	e.cal("BBB", in(2), "2026-05-10", "DATE_KNOWN")
	// CCC: nothing known.
	e.cal("CCC", "", "", "NOT_ANNOUNCED")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if u, n := e.urgency("AAA"); u != "revision_needed" || n != in(90) {
		t.Fatalf("AAA: %s next %q (the rolled-forward date must stay visible)", u, n)
	}
	if u, _ := e.urgency("BBB"); u != "red" {
		t.Fatalf("BBB: %s, want red", u)
	}
	if u, n := e.urgency("CCC"); u != "none" || n != "" {
		t.Fatalf("CCC: %s %q", u, n)
	}

	// Re-scoring (a new lock date after the report) clears the nudge.
	if _, err := e.db.Exec(`UPDATE theses_index SET locked_date = '2026-11-01' WHERE ticker = 'AAA'`); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if u, _ := e.urgency("AAA"); u != "none" {
		t.Fatalf("AAA after re-score: %s", u)
	}

	// REPORTED with no next date: no past date is exposed as "next".
	e.cal("CCC", "", "2026-11-04", "REPORTED")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if u, n := e.urgency("CCC"); u != "revision_needed" || n != "" {
		t.Fatalf("CCC reported after lock: %s next %q", u, n)
	}
}

func TestList_CarriesCalendarProvenance(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.cal("AAA", "2027-02-03", "2026-10-29", "DATE_ESTIMATED")
	if _, err := e.db.Exec(`UPDATE ticker_calendar SET earnings_is_estimate = 1, earnings_source = 'yahoo_earnings_page' WHERE ticker = 'AAA'`); err != nil {
		t.Fatal(err)
	}
	e.cal("BBB", "", "", "FETCH_ERROR")
	if _, err := e.db.Exec(`UPDATE ticker_calendar SET last_error = 'http 404: Quote not found' WHERE ticker = 'BBB'`); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	rows, err := e.eng.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Row{}
	for _, r := range rows {
		by[r.Ticker] = r
	}
	a := by["AAA"]
	if a.EarningsStatus != "DATE_ESTIMATED" || !a.EarningsIsEstimate || a.EarningsSource != "yahoo_earnings_page" ||
		a.LastReportedDate == nil || *a.LastReportedDate != "2026-10-29" || a.EarningsFetchedAt == nil {
		t.Fatalf("AAA row: %+v", a)
	}
	if b := by["BBB"]; b.EarningsStatus != "FETCH_ERROR" || b.EarningsNote != "http 404: Quote not found" {
		t.Fatalf("BBB row: %+v", b)
	}
	// existing fields are unchanged in name and meaning
	if a.NextEarningsDate == nil || *a.NextEarningsDate != "2027-02-03" || a.EarningsUrgency == "" {
		t.Fatalf("legacy fields: %+v", a)
	}
}

func TestGet_CarriesLastReportedDateForTheRevisionPrompt(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.cal("AAA", "2027-02-03", "2026-10-29", "DATE_ESTIMATED")
	var id int64
	if err := e.db.QueryRow(`SELECT id FROM theses_index WHERE ticker = 'AAA'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f, err := e.eng.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if f.LastReportedDate == nil || *f.LastReportedDate != "2026-10-29" {
		t.Fatalf("Get lastReportedDate = %v", f.LastReportedDate)
	}
}
