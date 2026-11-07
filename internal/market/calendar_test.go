package market

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseCalendarEvents_Fixtures(t *testing.T) {
	cases := []struct {
		file       string
		dates      []string
		estimate   bool
		exDividend string
	}{
		{"calendar_AVGO.json", []string{"2026-12-09"}, false, "2026-09-21"}, // normal date
		{"calendar_NOW.json", []string{"2027-01-27"}, true, ""},             // Yahoo says estimate
		{"calendar_LLY.json", []string{"2026-10-29"}, false, "2026-11-13"},  // past date still returned
		{"calendar_SU.PA.json", nil, false, "2026-05-11"},                   // no earnings date, ex-dividend only
		{"calendar_RR.L.json", nil, false, "2026-08-06"},
	}
	for _, c := range cases {
		ev, err := ParseCalendarEvents("X", fixture(t, c.file))
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if strings.Join(ev.EarningsDates, ",") != strings.Join(c.dates, ",") || ev.IsEstimate != c.estimate || ev.ExDividend != c.exDividend {
			t.Fatalf("%s: got %+v, want dates %v estimate %v exdiv %q", c.file, ev, c.dates, c.estimate, c.exDividend)
		}
	}
}

func TestParseCalendarEvents_EmptyResultAndGarbage(t *testing.T) {
	if _, err := ParseCalendarEvents("X", []byte(`{"quoteSummary":{"result":[],"error":null}}`)); !errors.Is(err, ErrNoCalendar) {
		t.Fatalf("empty result: %v", err)
	}
	if _, err := ParseCalendarEvents("X", []byte(`<html>nope`)); err == nil || errors.Is(err, ErrNoCalendar) {
		t.Fatalf("garbage should be a parse error, got %v", err)
	}
}

func TestHTTPErrorKeepsTheOldMessage(t *testing.T) {
	var err error = &HTTPError{Status: 404, Msg: "http 404: Not Found"}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 404 || err.Error() != "http 404: Not Found" {
		t.Fatal("HTTPError")
	}
}

func TestParseEarningsPage_RealPages(t *testing.T) {
	f, err := os.Open("testdata/earnings_page_LLY.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := ParseEarningsPage(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Date != "2027-02-03" || rows[0].Reported {
		t.Fatalf("first row (next report) = %+v", rows[0])
	}
	if rows[1].Date != "2026-10-29" || !rows[1].Reported {
		t.Fatalf("second row (last report) = %+v", rows[1])
	}

	f2, _ := os.Open("testdata/earnings_page_SU.PA.html")
	defer f2.Close()
	rows, err = ParseEarningsPage(f2)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Date != "2027-02-18" || rows[0].Reported {
		t.Fatalf("SU.PA first row = %+v", rows[0])
	}
}

func TestParseEarningsPage_GenericLayoutIsAnErrorNotAnEmptyAnswer(t *testing.T) {
	f, err := os.Open("testdata/earnings_page_generic_layout.html")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if rows, err := ParseEarningsPage(f); !errors.Is(err, ErrEarningsPageLayout) || rows != nil {
		t.Fatalf("generic page: rows %v err %v", rows, err)
	}
	if _, err := ParseEarningsPage(strings.NewReader("<html><body>no table</body></html>")); !errors.Is(err, ErrEarningsPageLayout) {
		t.Fatalf("no table: %v", err)
	}
}
