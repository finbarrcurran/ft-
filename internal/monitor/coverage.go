package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// statementCoverageAllowed is how long the coverage end may stay put (SC-51).
const statementCoverageAllowed = 35 * 24 * time.Hour

// statementCoverageEval judges the latest End Date of the imported eToro statements. With no
// statement imported there is nothing to keep fresh, so it stays quiet.
func statementCoverageEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		var end sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT MAX(range_end) FROM etoro_statement_periods WHERE superseded = 0`).Scan(&end); err != nil {
			return false, time.Time{}, false, "", err
		}
		if !end.Valid || end.String == "" {
			return false, time.Time{}, false, "no eToro statement imported yet", nil
		}
		e, err := time.Parse("2006-01-02", end.String)
		if err != nil {
			return false, time.Time{}, false, "", fmt.Errorf("bad coverage end %q", end.String)
		}
		next := e.AddDate(0, 0, 1).Format("2006-01-02")
		age := now.UTC().Sub(e)
		if age > statementCoverageAllowed {
			return true, e, true, fmt.Sprintf("Performance data ends %s (%d days ago); upload a statement starting %s", end.String, int(age.Hours()/24), next), nil
		}
		return false, e, true, fmt.Sprintf("Performance data ends %s", end.String), nil
	}
}
