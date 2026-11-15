package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// CoinGeckoBudgetCheck alerts (one Telegram message, then the usual daily reminder, then a recovery message) when
// the keyed CoinGecko calls of the UTC month reach the Part B soft stop (preference coingecko_partb_soft_stop,
// default 7,000 of the Demo key's 10,000). It counts the same provider_daily 'coingecko_key' calls the Part B
// gate reads, whether or not Part B is switched on: the baseline alone can approach the limit.
func CoinGeckoBudgetCheck(db *sql.DB) Check {
	return Check{
		Source: "coingecko_budget", Label: "CoinGecko keyed calls this month", MaxAge: 24 * time.Hour, Domain: "CoinGecko budget",
		Eval: func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
			first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			var n int
			if err := db.QueryRowContext(ctx,
				`SELECT COALESCE(SUM(calls),0) FROM provider_daily WHERE provider = 'coingecko_key' AND day >= ? AND day < ?`,
				first.Format("2006-01-02"), first.AddDate(0, 1, 0).Format("2006-01-02")).Scan(&n); err != nil {
				return false, time.Time{}, false, "", err
			}
			limit := 7000
			var v string
			if err := db.QueryRowContext(ctx, `SELECT value FROM user_preferences WHERE key = 'coingecko_partb_soft_stop'`).Scan(&v); err == nil {
				if x, err := strconv.Atoi(v); err == nil && x > 0 {
					limit = x
				}
			}
			if n >= limit {
				return true, now, true, fmt.Sprintf("%d keyed CoinGecko calls so far this month, at or over the soft stop of %d (the Demo key allows 10,000). Part B has stopped making new calls; the existing hourly and 04:00 calls continue. Raise coingecko_partb_soft_stop or wait for the 1st.", n, limit), nil
			}
			return false, now, true, fmt.Sprintf("%d of %d keyed calls this month", n, limit), nil
		},
	}
}
