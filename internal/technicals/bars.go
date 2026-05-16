// Package technicals owns the Percoco execution-layer math (Spec 9c):
// ATR, vol-tier classification, S/R candidate detection, R-multiple
// arithmetic, position sizing, and auto-scoring of the 8-Q technical
// screen. Pure functions where possible; DB IO is delegated to
// internal/store.
//
// Timeframe convention: Percoco trades 5/15-min charts intraday. FT is
// adapted to swing/position timeframes — *weekly primary, daily
// secondary*. Every helper that takes "bars" assumes the caller has
// already aggregated to the right cadence.
package technicals

import "time"

// Bar is one OHLC bar at whatever cadence the caller chose (daily or
// weekly for Spec 9c). Fields use float64 throughout because SQLite
// REAL is 8-byte and that's enough precision for prices on the scales
// we care about (sub-cent → 5-figure prices).
type Bar struct {
	Date  time.Time
	Open  float64
	High  float64
	Low   float64
	Close float64
}

// AggregateToWeekly rolls a slice of daily bars (oldest-first) into
// weekly bars keyed by the ISO-week starting Monday (UTC). Each weekly
// bar's:
//   - Open  = Monday's open
//   - High  = max(High) across the week
//   - Low   = min(Low) across the week
//   - Close = Friday's close (or last available day if Fri missing)
//
// Partial weeks at the boundaries are included; callers can drop the
// last bar if they want completed-week-only data.
func AggregateToWeekly(daily []Bar) []Bar {
	if len(daily) == 0 {
		return nil
	}
	// Sort assumption: input is already chronological. We don't sort
	// here so callers can detect ordering bugs.
	var out []Bar
	var cur Bar
	curKey := ""
	for _, b := range daily {
		k := weekStart(b.Date).Format("2006-01-02")
		if k != curKey {
			if curKey != "" {
				out = append(out, cur)
			}
			cur = Bar{
				Date:  weekStart(b.Date),
				Open:  b.Open,
				High:  b.High,
				Low:   b.Low,
				Close: b.Close,
			}
			curKey = k
			continue
		}
		if b.High > cur.High {
			cur.High = b.High
		}
		if b.Low < cur.Low {
			cur.Low = b.Low
		}
		cur.Close = b.Close
	}
	out = append(out, cur)
	return out
}

// weekStart returns the Monday-anchored start of t's ISO week, in UTC.
func weekStart(t time.Time) time.Time {
	u := t.UTC()
	wd := int(u.Weekday())
	// Go's Weekday: Sunday=0, Monday=1, ...; we want Monday=0.
	offset := wd - 1
	if wd == 0 {
		offset = 6
	}
	return time.Date(u.Year(), u.Month(), u.Day()-offset, 0, 0, 0, 0, time.UTC)
}
