package market

import (
	"testing"
	"time"
)

// SC-57: ATH is the highest monthly high in the returned history, labelled with that range; 52-week values
// come from the meta only; a response without them gives nils, never a computed guess.
const chartFixture = `{"chart":{"result":[{"meta":{"currency":"USD","regularMarketTime":1793000000,"regularMarketPrice":4.41,"fiftyTwoWeekHigh":9.5,"fiftyTwoWeekLow":2.1},
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
	if s.Price == nil || *s.Price != 4.41 {
		t.Fatalf("price %v", s.Price)
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

// SC-57 v1.110.1: the all-time-high DAY comes from daily bars (AUR: the weekly bar starting 2021-11-15 holds the high,
// which fell on 2021-11-19), and the history bar start is kept separately.
const dailyFixture = `{"chart":{"result":[{"timestamp":[1636934400,1637020800,1637107200,1637193600,1637280000,1637539200],
"indicators":{"quote":[{"high":[10.04,9.94,10.55,11.83,17.77,15.69]}]}}]}}`

func TestParseDailyHighDay(t *testing.T) {
	day, high, ok := ParseDailyHighDay([]byte(dailyFixture))
	if !ok || day != "2021-11-19" || high != 17.77 {
		t.Fatalf("got %q %v %v", day, high, ok)
	}
	if _, _, ok := ParseDailyHighDay([]byte(`{"chart":{"result":[]}}`)); ok {
		t.Fatal("an empty response must not resolve a day")
	}
}

func TestParseChartRange_KeepsBarStart(t *testing.T) {
	s, err := ParseChartRange([]byte(chartFixture), time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if s.ATHBarStart != 1633046400 {
		t.Fatalf("bar start %d", s.ATHBarStart)
	}
}
