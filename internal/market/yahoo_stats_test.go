package market

import (
	"testing"
	"time"
)

// SC-57: ATH is the highest monthly high in the returned history, labelled with that range; 52-week values
// come from the meta only; a response without them gives nils, never a computed guess.
const chartFixture = `{"chart":{"result":[{"meta":{"currency":"USD","regularMarketTime":1793000000,"fiftyTwoWeekHigh":9.5,"fiftyTwoWeekLow":2.1},
"timestamp":[1630454400,1633046400,1635724800,1638316800],
"indicators":{"quote":[{"high":[10.0,14.9,null,6.0]}]}}]}}`

func TestParseChartRange_ATHAndRange(t *testing.T) {
	s, err := ParseChartRange([]byte(chartFixture), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if s.ATH == nil || *s.ATH != 14.9 || s.ATHDate != "2021-10-01" {
		t.Fatalf("ath %v %q", s.ATH, s.ATHDate)
	}
	if s.HistoryFrom != "2021-09-01" || s.HistoryTo != "2021-12-01" {
		t.Fatalf("range %q..%q", s.HistoryFrom, s.HistoryTo)
	}
	if s.High52w == nil || *s.High52w != 9.5 || s.Low52w == nil || *s.Low52w != 2.1 || s.ChartAsOf != 1793000000 {
		t.Fatalf("52w %v %v asof %d", s.High52w, s.Low52w, s.ChartAsOf)
	}
}

func TestParseChartRange_MissingValuesAreNil(t *testing.T) {
	raw := `{"chart":{"result":[{"meta":{"currency":"EUR"},"timestamp":[],"indicators":{"quote":[{"high":[]}]}}]}}`
	s, err := ParseChartRange([]byte(raw), time.Unix(42, 0))
	if err != nil {
		t.Fatal(err)
	}
	if s.ATH != nil || s.High52w != nil || s.Low52w != nil || s.HistoryFrom != "" {
		t.Fatalf("expected nils, got %+v", s)
	}
	if s.ChartAsOf != 42 {
		t.Fatalf("as-of falls back to the fetch time, got %d", s.ChartAsOf)
	}
	if _, err := ParseChartRange([]byte(`{"chart":{"result":[]}}`), time.Now()); err == nil {
		t.Fatal("empty result must be an error")
	}
}
