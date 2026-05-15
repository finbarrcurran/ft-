// Package marketdata holds exchange-hour calendars and the helpers that
// answer "is X market open right now?" / "when does it next change state?"
//
// Spec 2 D5 ships US-only (NYSE+NASDAQ share hours). Spec 5 extends with
// LSE / EURONEXT / XETRA / TSE / HKEX / B3.
package marketdata

import (
	"time"
)

// usHolidays is a hand-curated list of NYSE / NASDAQ closure dates. Kept in
// code (rather than embed:json) because it's small and rarely changes — set
// a calendar reminder for December annually to refresh next year.
//
// Source: nyse.com/markets/hours-calendars. Confirmed for 2025–2027.
var usHolidays = map[string]bool{
	// 2025
	"2025-01-01": true, // New Year's Day
	"2025-01-20": true, // MLK Day
	"2025-02-17": true, // Presidents' Day
	"2025-04-18": true, // Good Friday
	"2025-05-26": true, // Memorial Day
	"2025-06-19": true, // Juneteenth
	"2025-07-04": true, // Independence Day
	"2025-09-01": true, // Labor Day
	"2025-11-27": true, // Thanksgiving
	"2025-12-25": true, // Christmas

	// 2026
	"2026-01-01": true,
	"2026-01-19": true,
	"2026-02-16": true,
	"2026-04-03": true, // Good Friday
	"2026-05-25": true,
	"2026-06-19": true,
	"2026-07-03": true, // observed (Jul 4 is Saturday)
	"2026-09-07": true,
	"2026-11-26": true,
	"2026-12-25": true,

	// 2027
	"2027-01-01": true,
	"2027-01-18": true,
	"2027-02-15": true,
	"2027-03-26": true, // Good Friday
	"2027-05-31": true,
	"2027-06-18": true, // observed (Jun 19 is Saturday)
	"2027-07-05": true, // observed (Jul 4 is Sunday)
	"2027-09-06": true,
	"2027-11-25": true,
	"2027-12-24": true, // observed (Dec 25 is Saturday)
}

// USStatus describes NYSE/NASDAQ at a moment.
type USStatus struct {
	Open           bool      `json:"open"`
	NextChange     time.Time `json:"nextChange"`
	NextChangeKind string    `json:"nextChangeKind"` // "open" or "close"
}

// USMarketStatus returns the state of the US markets at `now`. Hours are
// 09:30–16:00 ET on weekdays excluding holidays. Half-days (e.g. day after
// Thanksgiving close at 13:00) are treated as full closed for v1.
func USMarketStatus(now time.Time) USStatus {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		// Should never happen on Ubuntu with tzdata installed. Bail to UTC.
		loc = time.UTC
	}
	ny := now.In(loc)

	if isClosed(ny) {
		return USStatus{
			Open:           false,
			NextChange:     nextUSOpen(ny, loc),
			NextChangeKind: "open",
		}
	}

	openT := time.Date(ny.Year(), ny.Month(), ny.Day(), 9, 30, 0, 0, loc)
	closeT := time.Date(ny.Year(), ny.Month(), ny.Day(), 16, 0, 0, 0, loc)

	switch {
	case ny.Before(openT):
		return USStatus{Open: false, NextChange: openT, NextChangeKind: "open"}
	case !ny.Before(closeT):
		return USStatus{
			Open:           false,
			NextChange:     nextUSOpen(ny, loc),
			NextChangeKind: "open",
		}
	default:
		return USStatus{Open: true, NextChange: closeT, NextChangeKind: "close"}
	}
}

// isClosed reports whether a given NY-local time falls on a weekend or holiday.
func isClosed(ny time.Time) bool {
	switch ny.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	}
	return usHolidays[ny.Format("2006-01-02")]
}

// nextUSOpen finds the next 09:30 ET that isn't a weekend or holiday.
func nextUSOpen(after time.Time, loc *time.Location) time.Time {
	// Start at 09:30 today (in NY local), advance day-by-day until we hit a
	// weekday that isn't a holiday AND is in the future.
	cand := time.Date(after.Year(), after.Month(), after.Day(), 9, 30, 0, 0, loc)
	for !cand.After(after) || isClosed(cand) {
		cand = cand.AddDate(0, 0, 1)
		// Reset to 09:30 in case AddDate crossed a DST boundary; constructing
		// fresh via Date ensures we land exactly on 09:30 wall-clock time.
		cand = time.Date(cand.Year(), cand.Month(), cand.Day(), 9, 30, 0, 0, loc)
	}
	return cand
}
