package freshness

import (
	"testing"
	"time"

	"ft/internal/marketdata"
)

func day(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", s+" 15:00")
	return t
}

func level(t *testing.T, id, asOf, now string) (int, string) {
	t.Helper()
	r, ok := For(id)
	if !ok {
		t.Fatalf("no rule for %s", id)
	}
	age, err := r.Age(asOf, day(now))
	if err != nil {
		t.Fatal(err)
	}
	return age, r.Level(age)
}

func TestCFNAIBoundaries(t *testing.T) {
	// CFNAI is dated 2026-08-01. ages: 73 (today), 87 (normal worst case), 94, 95, 96, 119, 120, 121.
	base := day("2026-08-01")
	for _, c := range []struct {
		age  int
		want string
	}{{73, ""}, {87, ""}, {94, ""}, {95, "amber"}, {96, "amber"}, {119, "amber"}, {120, "red"}, {121, "red"}} {
		now := base.AddDate(0, 0, c.age).Format("2006-01-02")
		if age, lvl := level(t, "pal_cfnai", "2026-08-01", now); age != c.age || lvl != c.want {
			t.Fatalf("age %d: got %d %q, want %q", c.age, age, lvl, c.want)
		}
	}
}

func TestDXYAndStablecoinBoundaries(t *testing.T) {
	for _, c := range []struct {
		id   string
		age  int
		want string
	}{
		{"pal_dxy", 11, ""}, {"pal_dxy", 13, ""}, {"pal_dxy", 14, "amber"}, {"pal_dxy", 20, "amber"}, {"pal_dxy", 21, "red"},
		{"universal_stablecoin_supply", 0, ""}, {"universal_stablecoin_supply", 2, ""}, {"universal_stablecoin_supply", 3, "amber"},
		{"universal_stablecoin_supply", 4, "amber"}, {"universal_stablecoin_supply", 5, "red"},
	} {
		asOf := day("2026-10-01")
		now := asOf.AddDate(0, 0, c.age).Format("2006-01-02")
		if age, lvl := level(t, c.id, "2026-10-01", now); age != c.age || lvl != c.want {
			t.Fatalf("%s age %d: got %d %q, want %q", c.id, c.age, age, lvl, c.want)
		}
	}
}

// pal_us2y counts US trading days; Thanksgiving week has a holiday and a Friday half-day.
func TestUS2YTradingDaysAcrossAHoliday(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	// Data dated Fri 2026-11-20. Thanksgiving is Thu 2026-11-26.
	for _, c := range []struct {
		now  string
		age  int
		want string
	}{
		{"2026-11-24", 2, ""}, {"2026-11-27", 4, ""}, // 23,24,25,27 (26th is a holiday)
		{"2026-11-30", 5, "amber"}, {"2026-12-02", 7, "amber"}, {"2026-12-03", 8, "red"},
	} {
		if age, lvl := level(t, "pal_us2y", "2026-11-20", c.now); age != c.age || lvl != c.want {
			t.Fatalf("%s: got %d %q, want %d %q", c.now, age, lvl, c.age, c.want)
		}
	}
}

// Nothing may be amber/red today on the live data dates (2026-10-13).
func TestLiveDataIsFreshToday(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	for id, asOf := range map[string]string{"pal_cfnai": "2026-08-01", "pal_dxy": "2026-10-02", "pal_us2y": "2026-10-08", "universal_stablecoin_supply": "2026-10-13"} {
		if age, lvl := level(t, id, asOf, "2026-10-13"); lvl != "" {
			t.Fatalf("%s as of %s must not be flagged today (age %d, %q)", id, asOf, age, lvl)
		}
	}
	// And the figures the work order quotes.
	if a, _ := level(t, "pal_cfnai", "2026-08-01", "2026-10-13"); a != 73 {
		t.Fatalf("cfnai age %d", a)
	}
	if a, _ := level(t, "pal_us2y", "2026-10-08", "2026-10-13"); a != 3 {
		t.Fatalf("us2y trading days %d", a)
	}
}

func TestEveryRuleIsConsistent(t *testing.T) {
	for id, r := range Rules {
		if r.Amber <= 0 || r.Red <= r.Amber || r.Every <= 0 || (r.Unit != Trading && r.Unit != Calendar) {
			t.Fatalf("%s: bad rule %+v", id, r)
		}
	}
	if Rules["pal_cfnai"].Every != 7*24*time.Hour || Rules["pal_us2y"].Every != 24*time.Hour {
		t.Fatal("reminder intervals: monthly 7 d, daily 24 h")
	}
}
