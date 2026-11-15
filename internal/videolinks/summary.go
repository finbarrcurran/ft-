package videolinks

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Summary returns, for each of the given tickers that has any commentary, when each commentator last mentioned it:
// the data behind the "J 3d · C 12d" chip on the Watchlist and AI Nexus rows (FT-149). It is the same newest-link rule
// as Commentary.Last, so a chip and its drawer always agree.
func Summary(ctx context.Context, db *sql.DB, tickers []string, now time.Time) (map[string]map[string]Last, error) {
	want := make(map[string]bool, len(tickers))
	for _, t := range tickers {
		want[strings.ToUpper(strings.TrimSpace(t))] = true
	}
	rows, err := db.QueryContext(ctx, `SELECT l.ticker, d.source, MAX(d.published_at) FROM video_links l JOIN video_digests d ON d.id = l.digest_id GROUP BY l.ticker, d.source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]Last{}
	for rows.Next() {
		var tk, src, date string
		if err := rows.Scan(&tk, &src, &date); err != nil {
			return nil, err
		}
		if !want[tk] {
			continue
		}
		d, _ := time.Parse("2006-01-02", date)
		if out[tk] == nil {
			out[tk] = map[string]Last{}
		}
		out[tk][src] = Last{Date: date, DaysAgo: int(now.UTC().Sub(d).Hours() / 24)}
	}
	return out, rows.Err()
}

// KnownTicker reports whether FT holds, watches or tracks the ticker (the commentary API answers only for those).
func KnownTicker(ctx context.Context, db *sql.DB, ticker string) bool {
	ix, err := BuildIndex(ctx, db, nil)
	return err == nil && ix.Entity(ticker) != nil
}
