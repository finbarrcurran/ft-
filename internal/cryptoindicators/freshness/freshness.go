// Package freshness holds the per-indicator rule for how old an indicator's DATA may be before
// it is flagged (SC-49 for the Farside ETF flow, SC-49b for the FRED and DefiLlama series).
// The cards, the chart endpoints, Diagnostics and the connectivity monitor all use it, so they
// cannot disagree. It judges the date of the data, never the time of the last refresh.
package freshness

import (
	"fmt"
	"time"

	"ft/internal/marketdata"
)

const (
	Trading  = "trading"  // US trading days (weekends and US holidays excluded)
	Calendar = "calendar" // plain days from the observation date
)

// Rule is the allowed age of one indicator's data.
type Rule struct {
	Unit  string
	Amber int           // age >= Amber is "stale" (amber); the monitor alerts here
	Red   int           // age >= Red is red
	Every time.Duration // monitor reminder interval while stale (daily series 24 h, monthly 7 days)
	Note  string        // why these numbers (shown in the spec / Diagnostics tooltip)
}

// Rules by crypto_indicators id. Only series whose data date is known are listed.
var Rules = map[string]Rule{
	// SC-49: Farside publishes each US trading day.
	"universal_etf_flow_7d": {Unit: Trading, Amber: 3, Red: 5, Every: 24 * time.Hour, Note: "Farside publishes every US trading day"},
	// SC-49b.
	"pal_us2y": {Unit: Trading, Amber: 5, Red: 8, Every: 24 * time.Hour, Note: "FRED DGS2, business days, 1-5 day lag"},
	"pal_dxy":  {Unit: Calendar, Amber: 14, Red: 21, Every: 24 * time.Hour, Note: "FRED DTWEXBGS, daily series published weekly, 6-11 day lag"},
	// CFNAI is dated the 1st of the month and released ~3-4 weeks after month end (2025-2026 FRED
	// release dates: the longest wait for the next value is ~87 days from the observation date).
	"pal_cfnai":                   {Unit: Calendar, Amber: 95, Red: 120, Every: 7 * 24 * time.Hour, Note: "FRED CFNAI, monthly; normal maximum age just before a release is ~87 days"},
	"universal_stablecoin_supply": {Unit: Calendar, Amber: 3, Red: 5, Every: 24 * time.Hour, Note: "DefiLlama stablecoincharts, daily including weekends"},
}

// For returns the rule of an indicator.
func For(id string) (Rule, bool) {
	r, ok := Rules[id]
	return r, ok
}

// Age is the age of asOf (YYYY-MM-DD) in the rule's unit, as of now.
func (r Rule) Age(asOf string, now time.Time) (int, error) {
	if r.Unit == Trading {
		return marketdata.USTradingDaysSince(asOf, now)
	}
	d, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return 0, fmt.Errorf("bad data date %q", asOf)
	}
	n := now.UTC()
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(d).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, nil
}

// Level maps an age to "", "amber" or "red".
func (r Rule) Level(age int) string {
	switch {
	case age >= r.Red:
		return "red"
	case age >= r.Amber:
		return "amber"
	}
	return ""
}

// UnitLabel is the human unit ("US trading days" / "days").
func (r Rule) UnitLabel() string {
	if r.Unit == Trading {
		return "US trading days"
	}
	return "days"
}
