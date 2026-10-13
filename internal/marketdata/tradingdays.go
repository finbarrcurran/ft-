package marketdata

import (
	"fmt"
	"time"
)

// USTradingDaysSince returns how many US trading days have passed after `asOf`
// (YYYY-MM-DD), counting up to and including today's US-Eastern date: weekends and
// US exchange holidays do not count. (SC-49: the age of ETF-flow data.)
//
// Examples: data dated Friday, looked at on the following Monday morning -> 1; the same
// data on Wednesday -> 3. Flows publish after the close, so an as-of one trading day
// behind is normal. Future or same-day dates return 0.
func USTradingDaysSince(asOf string, now time.Time) (int, error) {
	d, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return 0, fmt.Errorf("bad as-of date %q: %w", asOf, err)
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.UTC
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	n = time.Time{}
	count := 0
	for day := d.AddDate(0, 0, 1); !day.After(today); day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		if _, hol := IsHoliday("US", day.Format("2006-01-02")); hol {
			continue
		}
		count++
		if count > 1000 { // an absurdly old as-of; the exact number no longer matters
			break
		}
	}
	return count, nil
}

// StaleLevel maps a trading-day age to the badge: none at <= 2 days, "amber" at 3-4,
// "red" at >= 5. (SC-49 thresholds; the same rule is used by the card, the chart and
// the monitor so they can never disagree.)
func StaleLevel(ageTradingDays int) string {
	switch {
	case ageTradingDays >= 5:
		return "red"
	case ageTradingDays >= 3:
		return "amber"
	}
	return ""
}
