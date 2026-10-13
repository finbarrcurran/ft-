package monitor

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"ft/internal/cryptoindicators/freshness"
)

// SC-49b — data-date freshness for the FRED and DefiLlama crypto indicators. Judged by
// crypto_indicators.data_as_of (the date of the data), never by when it was last refreshed.

// fetchQuiet is how long the refresh may go without succeeding before the alert blames the fetch.
const fetchQuiet = 30 * time.Hour

var indicatorChecks = []struct{ ID, Label string }{
	{"pal_us2y", "US 2-year yield data (FRED DGS2)"},
	{"pal_dxy", "Dollar index data (FRED DTWEXBGS)"},
	{"pal_cfnai", "Chicago Fed activity index data (FRED CFNAI)"},
	{"universal_stablecoin_supply", "Stablecoin supply data (DefiLlama)"},
}

// indicatorChecksFor builds one Check per ruled indicator.
func indicatorChecksFor(db *sql.DB) []Check {
	out := make([]Check, 0, len(indicatorChecks))
	for _, c := range indicatorChecks {
		rule, _ := freshness.For(c.ID)
		out = append(out, Check{
			Source: "ci_" + c.ID, Label: c.Label, MaxAge: time.Duration(rule.Amber) * 24 * time.Hour,
			ReminderEvery: rule.Every, Eval: indicatorFreshnessEval(db, c.ID),
		})
	}
	return out
}

// indicatorFreshnessEval alerts when the data date is at or past the amber age of its rule, and
// names the cause: the fetch failing versus the source not publishing newer data.
func indicatorFreshnessEval(db *sql.DB, id string) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		rule, ok := freshness.For(id)
		if !ok {
			return false, time.Time{}, false, "", fmt.Errorf("no freshness rule for %s", id)
		}
		var asOf, fetchErr sql.NullString
		var updated sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT data_as_of, fetch_error, updated_at FROM crypto_indicators WHERE id = ?`, id).Scan(&asOf, &fetchErr, &updated)
		if err == sql.ErrNoRows {
			return false, time.Time{}, false, "indicator not present", nil
		}
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		if !asOf.Valid || asOf.String == "" {
			return false, time.Time{}, false, "no data date recorded yet (the next refresh records it)", nil
		}
		age, err := rule.Age(asOf.String, now)
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		asOfT, _ := time.Parse("2006-01-02", asOf.String)
		detail := fmt.Sprintf("newest data is %s (%d %s old; alert at %d)", asOf.String, age, rule.UnitLabel(), rule.Amber)
		if age < rule.Amber {
			return false, asOfT, true, detail, nil
		}
		fetchFailed := (fetchErr.Valid && fetchErr.String != "") || !updated.Valid || now.Sub(time.Unix(updated.Int64, 0)) > fetchQuiet
		if fetchFailed {
			msg := "FETCH FAILED"
			if fetchErr.Valid && fetchErr.String != "" {
				msg += ": " + trunc(fetchErr.String, 90)
			} else {
				msg += ": the indicator refresh has not succeeded recently"
			}
			return true, asOfT, true, detail + " — " + msg, nil
		}
		return true, asOfT, true, detail + " — SOURCE not updated (the fetch is working, the provider has no newer data)", nil
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
