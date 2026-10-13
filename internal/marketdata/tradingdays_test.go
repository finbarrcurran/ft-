package marketdata

import (
	"testing"
	"time"
)

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func TestUSTradingDaysSince(t *testing.T) {
	if err := LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	// 2026-10-09 is a Friday; 2026-10-12 a Monday.
	cases := []struct {
		name  string
		asOf  string
		now   time.Time
		want  int
		level string
	}{
		{"Monday morning after Friday data", "2026-10-09", utc(2026, 10, 12, 13), 1, ""},
		{"Tuesday, Monday data (normal one-day lag)", "2026-10-12", utc(2026, 10, 13, 13), 1, ""},
		{"Tuesday with Friday data", "2026-10-09", utc(2026, 10, 13, 13), 2, ""},
		{"Wednesday with Friday data -> amber", "2026-10-09", utc(2026, 10, 14, 13), 3, "amber"},
		{"Thursday with Friday data -> amber", "2026-10-09", utc(2026, 10, 15, 13), 4, "amber"},
		{"Friday with Friday data a week old -> red", "2026-10-09", utc(2026, 10, 16, 13), 5, "red"},
		{"same day", "2026-10-13", utc(2026, 10, 13, 13), 0, ""},
		{"weekend does not count", "2026-10-09", utc(2026, 10, 11, 13), 0, ""},
		// Thanksgiving 2026 is Thursday 26 Nov (US market closed). Wed 25 data viewed Fri 27: only Fri counts.
		{"holiday week: Thanksgiving not counted", "2026-11-25", utc(2026, 11, 27, 15), 1, ""},
		{"holiday week: Monday after, Wednesday data", "2026-11-25", utc(2026, 11, 30, 15), 2, ""},
		// New York date, not UTC: 03:00 UTC Tuesday is still Monday evening in New York.
		{"New York date is used", "2026-10-09", utc(2026, 10, 13, 3), 1, ""},
	}
	for _, c := range cases {
		got, err := USTradingDaysSince(c.asOf, c.now)
		if err != nil || got != c.want {
			t.Errorf("%s: got %d (%v), want %d", c.name, got, err, c.want)
		}
		if lv := StaleLevel(got); lv != c.level {
			t.Errorf("%s: level %q, want %q", c.name, lv, c.level)
		}
	}
	if _, err := USTradingDaysSince("not-a-date", utc(2026, 10, 13, 13)); err == nil {
		t.Error("a bad date must error")
	}
}
