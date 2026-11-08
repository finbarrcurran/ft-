package monitor

// SC-56 P4 — report-date alerts. Two pieces:
//   - the "report_dates" check (state): holdings and watchlist stocks whose date could not be fetched, is
//     missing, or is a past date shown as "next"; one message when it goes down, a daily reminder while it
//     lasts, a recovery message when it clears, nothing while green (the monitor's own rules);
//   - the daily digest (events): date changes in the last 24 hours for owned and watchlist tickers, sent
//     once per UTC day and only when there is something to say.
// Both use the SC-55 sender (Telegram via FT_TELEGRAM_BOT_TOKEN / FT_TELEGRAM_CHAT_ID, the chat the ft-bot
// uses: @FinsFTAlerts_bot).

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

const calendarDomain = "report dates"

// A transient fetch failure must persist this many consecutive hourly runs to count; a 404 counts at once.
const calendarTransientRuns = 3

// CalendarChecks returns the report-date check.
func CalendarChecks(db *sql.DB) []Check {
	return []Check{
		{Source: "report_dates", Label: "Report dates", Domain: calendarDomain,
			MaxAge: 36 * time.Hour, Eval: calendarEval(db)},
		{Source: "report_dates_second", Label: "Report dates: second source", Domain: calendarDomain,
			MaxAge: 36 * time.Hour, Eval: calendarSecondEval(db)},
	}
}

// A single page can fail once for a network reason; a Yahoo layout change fails every lookup. The check goes
// down when at least this many tickers have failed the second source at least this many times in a row.
const (
	calendarSecondTickers = 3
	calendarSecondRuns    = 2
)

func calendarSecondEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		rows, err := db.QueryContext(ctx, `
			SELECT ticker, COALESCE(second_error, ''), second_error_count
			  FROM ticker_calendar
			 WHERE second_error_count >= ?
			 ORDER BY second_error_count DESC, ticker`, calendarSecondRuns)
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		defer rows.Close()
		var items []string
		lastErr := ""
		for rows.Next() {
			var tk, e string
			var n int
			if err := rows.Scan(&tk, &e, &n); err != nil {
				return false, time.Time{}, false, "", err
			}
			items = append(items, fmt.Sprintf("%s x%d", tk, n))
			if lastErr == "" {
				lastErr = e
			}
		}
		if err := rows.Err(); err != nil {
			return false, time.Time{}, false, "", err
		}
		if len(items) < calendarSecondTickers {
			return false, now, true, "second source (earnings page) answering", nil
		}
		if len(lastErr) > 110 {
			lastErr = lastErr[:110] + "…"
		}
		total, more := len(items), ""
		if total > 8 {
			more = fmt.Sprintf(" +%d more", total-8)
			items = items[:8]
		}
		return true, now, true, fmt.Sprintf("the Yahoo earnings page failed for %d tickers in a row (%s%s); next dates after a report may be missing; Yahoo may have changed the page. Last error: %s",
			total, strings.Join(items, ", "), more, lastErr), nil
	}
}

func calendarEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		today := now.UTC().Format("2006-01-02")
		// The job has run if any row was updated in the last 6 hours; before that "missing" is not a fault
		// (right after a deploy, or while the job is off).
		var recent int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticker_calendar WHERE updated_at >= ?`, now.Add(-6*time.Hour).Unix()).Scan(&recent); err != nil {
			return false, time.Time{}, false, "", err
		}
		if recent == 0 {
			return false, now, true, "report-date job has not run in the last 6 hours (not judged)", nil
		}
		rows, err := db.QueryContext(ctx, `
			SELECT u.ticker, COALESCE(c.earnings_status, ''), c.earnings_date, COALESCE(c.last_error, ''),
			       COALESCE(c.error_count, 0), COALESCE(c.error_permanent, 0)
			  FROM (
			        SELECT ticker FROM stock_holdings WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != ''
			        UNION
			        SELECT ticker FROM watchlist WHERE deleted_at IS NULL AND kind = 'stock' AND ticker != ''
			       ) u
			  LEFT JOIN ticker_calendar c ON c.ticker = u.ticker
			 ORDER BY u.ticker`)
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		defer rows.Close()
		groups := map[string][]string{}
		for rows.Next() {
			var tk, status, errText string
			var date sql.NullString
			var errCount, perm int
			if err := rows.Scan(&tk, &status, &date, &errText, &errCount, &perm); err != nil {
				return false, time.Time{}, false, "", err
			}
			switch {
			case status == "NOT_APPLICABLE" || status == "NOT_ANNOUNCED":
				// genuine states, not faults
			case status == "" || status == "UNKNOWN":
				groups["missing"] = append(groups["missing"], tk)
			case status == "FETCH_ERROR":
				if perm != 0 || errCount >= calendarTransientRuns {
					msg := errText
					if len(msg) > 90 {
						msg = msg[:90] + "…"
					}
					groups["fetch error"] = append(groups["fetch error"], fmt.Sprintf("%s (%s)", tk, msg))
				}
			case date.Valid && date.String < today && (status == "DATE_KNOWN" || status == "DATE_ESTIMATED"):
				groups["past date shown as next"] = append(groups["past date shown as next"], fmt.Sprintf("%s (%s)", tk, date.String))
			}
		}
		if err := rows.Err(); err != nil {
			return false, time.Time{}, false, "", err
		}
		if len(groups) == 0 {
			return false, now, true, "all holdings and watchlist stocks have a date or an explicit state", nil
		}
		order := make([]string, 0, len(groups))
		for k := range groups {
			order = append(order, k)
		}
		sort.Strings(order)
		var parts []string
		for _, k := range order {
			l := groups[k]
			more := ""
			if len(l) > 8 {
				more = fmt.Sprintf(" +%d more", len(l)-8)
				l = l[:8]
			}
			parts = append(parts, fmt.Sprintf("%s: %s%s", k, strings.Join(l, ", "), more))
		}
		return true, now, true, strings.Join(parts, "; "), nil
	}
}

// CalendarDigest sends the daily date-change digest if there is anything in it and none was sent today.
// It returns whether a message was sent.
func CalendarDigest(ctx context.Context, db *sql.DB, n Notifier, now time.Time) (bool, error) {
	day := now.UTC().Format("2006-01-02")
	var already int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticker_calendar_digest_sent WHERE day = ?`, day).Scan(&already); err != nil {
		return false, err
	}
	if already > 0 {
		return false, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT c.ticker, c.prev_earnings_date, c.earnings_date, c.earnings_is_estimate,
		       EXISTS (SELECT 1 FROM stock_holdings h WHERE h.ticker = c.ticker AND h.deleted_at IS NULL)
		  FROM ticker_calendar c
		 WHERE c.date_changed_at >= ?
		   AND (EXISTS (SELECT 1 FROM stock_holdings h WHERE h.ticker = c.ticker AND h.deleted_at IS NULL)
		        OR EXISTS (SELECT 1 FROM watchlist w WHERE w.ticker = c.ticker AND w.kind = 'stock' AND w.deleted_at IS NULL))
		 ORDER BY c.ticker`, now.Add(-24*time.Hour).Unix())
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var tk string
		var prev, cur sql.NullString
		var est, owned int
		if err := rows.Scan(&tk, &prev, &cur, &est, &owned); err != nil {
			return false, err
		}
		who := "watchlist"
		if owned != 0 {
			who = "owned"
		}
		next := "no date yet (reported)"
		if cur.Valid {
			next = cur.String
			if est != 0 {
				next += " (estimate)"
			}
		}
		switch {
		case prev.Valid && cur.Valid:
			lines = append(lines, fmt.Sprintf("• %s [%s]: %s → %s", tk, who, prev.String, next))
		case cur.Valid:
			lines = append(lines, fmt.Sprintf("• %s [%s]: new report date %s", tk, who, next))
		default:
			lines = append(lines, fmt.Sprintf("• %s [%s]: was %s, now %s", tk, who, prev.String, next))
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(lines) == 0 {
		return false, nil // silent when there is nothing to say
	}
	text := "📅 FT report dates: " + fmt.Sprint(len(lines)) + " change(s) in the last 24 h\n" + strings.Join(lines, "\n")
	if n != nil {
		if err := n.Send(ctx, text); err != nil {
			return false, err
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO ticker_calendar_digest_sent (day, sent_at, lines) VALUES (?, ?, ?)`, day, now.Unix(), len(lines)); err != nil {
		return true, err
	}
	return true, nil
}
