package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// snapshotAllowed is how long the weekly FT snapshot (SC-46) may go untaken: a Sunday run plus a
// Monday catch-up leave 8 days between two on-time snapshots at most, so a missed week alerts.
const snapshotAllowed = 8 * 24 * time.Hour

// snapshotEval alerts when the newest snapshot is older than snapshotAllowed. Before the first
// snapshot exists there is nothing to be late for, so it stays quiet.
func snapshotEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		var ts sql.NullInt64
		if err := db.QueryRowContext(ctx, `SELECT MAX(generated_at) FROM ft_snapshots`).Scan(&ts); err != nil {
			return false, time.Time{}, false, "", err
		}
		if !ts.Valid {
			return false, time.Time{}, false, "no snapshot taken yet", nil
		}
		last := time.Unix(ts.Int64, 0).UTC()
		age := now.Sub(last)
		if age > snapshotAllowed {
			return true, last, true, fmt.Sprintf("the last weekly snapshot was taken %s (%d days ago); it should run every Sunday evening — check the FT service log", last.Format("2006-01-02"), int(age.Hours()/24)), nil
		}
		return false, last, true, fmt.Sprintf("last weekly snapshot %s", last.Format("2006-01-02")), nil
	}
}
