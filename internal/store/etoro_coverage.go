package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SC-51 Part B — how far the imported eToro statements reach.
//
// Coverage end = the latest End Date of any imported statement (not the newest close date);
// coverage start = the earliest Start Date. Both come from etoro_statement_periods
// (non-superseded rows). Statements should tile: each new one starts the day after the
// previous coverage end.

// EtoroCoverage is the span the imported statements cover.
type EtoroCoverage struct {
	Has       bool   `json:"has"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	NextStart string `json:"nextStart,omitempty"` // End + 1 day: where the next statement should start
	AgeDays   int    `json:"ageDays"`             // whole days from End to today (UTC)
	Stale     bool   `json:"stale"`               // AgeDays > StatementCoverageAllowedDays
}

// StatementCoverageAllowedDays is how long the coverage end may stay put before FT nags.
const StatementCoverageAllowedDays = 35

// EtoroCoverageFor reads the coverage; now is passed in for testing.
func (s *Store) EtoroCoverageFor(ctx context.Context, userID int64, now time.Time) (EtoroCoverage, error) {
	var start, end sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT MIN(range_start), MAX(range_end) FROM etoro_statement_periods WHERE user_id = ? AND superseded = 0`, userID).
		Scan(&start, &end)
	if err != nil {
		return EtoroCoverage{}, err
	}
	return BuildCoverage(start.String, end.String, now), nil
}

// BuildCoverage assembles the coverage struct from the two dates (pure).
func BuildCoverage(start, end string, now time.Time) EtoroCoverage {
	if start == "" || end == "" {
		return EtoroCoverage{}
	}
	c := EtoroCoverage{Has: true, Start: start, End: end, NextStart: dayAfter(end)}
	if e, err := time.Parse("2006-01-02", end); err == nil {
		today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
		c.AgeDays = int(today.Sub(e).Hours() / 24)
		c.Stale = c.AgeDays > StatementCoverageAllowedDays
	}
	return c
}

// StartCheck classifies where a new statement starts relative to the coverage.
type StartCheck struct {
	Kind          string `json:"kind"`                    // first | clean | gap | overlap
	ExpectedStart string `json:"expectedStart,omitempty"` // coverage end + 1
	MissingFrom   string `json:"missingFrom,omitempty"`   // gap only: first uncovered day
	MissingTo     string `json:"missingTo,omitempty"`     // gap only: last uncovered day (= statement start - 1)
	Message       string `json:"message,omitempty"`
}

// ClassifyStatementStart compares a statement's Start Date with the coverage. A start after
// End+1 leaves days nobody has data for (a gap: positions closed in it will be missing); a
// start on or before End overlaps; End+1 is the clean tile.
func ClassifyStatementStart(cov EtoroCoverage, stmtStart string) StartCheck {
	if !cov.Has || stmtStart == "" {
		return StartCheck{Kind: "first"}
	}
	exp := cov.NextStart
	switch {
	case stmtStart == exp:
		return StartCheck{Kind: "clean", ExpectedStart: exp}
	case stmtStart > exp:
		to := previousDay(stmtStart)
		return StartCheck{Kind: "gap", ExpectedStart: exp, MissingFrom: exp, MissingTo: to,
			Message: fmt.Sprintf("Positions closed between %s and %s will be missing: your imported statements end %s and this one starts %s.", exp, to, cov.End, stmtStart)}
	default:
		return StartCheck{Kind: "overlap", ExpectedStart: exp,
			Message: fmt.Sprintf("This statement starts %s, before the imported coverage ends (%s). Positions are de-duplicated by Position ID, but fees, interest and refunds on the overlapping days may be counted twice.", stmtStart, cov.End)}
	}
}

func previousDay(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}
