package monitor

// SC-56b — one daily summary of held theses that need a re-score, instead of one nudge per holding every day.
//
// A thesis needs a re-score ("revision_needed") when the company has reported since the thesis was locked
// (theses_index.earnings_urgency, set by the thesis sync from ticker_calendar.last_reported_date). The summary
// lists only HELD tickers, with the days since the report, in one Telegram message through the same sender as
// the other monitors (@FinsFTAlerts_bot). It is sent at most once per UTC day (recorded in notification_log
// under holding_kind 'system', so a restart does not repeat it) and is silent when the list is empty.
// A failed send is not recorded, so the next call the same day retries.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const revisionSummaryKind = "thesis_revision_summary"

// revisionSummaryMax is how many lines one message carries; the rest are counted.
const revisionSummaryMax = 25

// RevisionSummary sends the daily summary if there is anything in it and none was sent today.
// It returns whether a message was sent.
func RevisionSummary(ctx context.Context, db *sql.DB, n Notifier, now time.Time) (bool, error) {
	day := now.UTC().Format("2006-01-02")
	var already int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log WHERE holding_kind = 'system' AND holding_id = 0 AND alert_kind = ? AND alert_day = ?`,
		revisionSummaryKind, day).Scan(&already); err != nil {
		return false, err
	}
	if already > 0 {
		return false, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT t.ticker, COALESCE(t.locked_date, ''), COALESCE(c.last_reported_date, '')
		  FROM theses_index t
		  LEFT JOIN ticker_calendar c ON c.ticker = t.ticker
		 WHERE t.status = 'locked' AND t.earnings_urgency = 'revision_needed'
		   AND EXISTS (SELECT 1 FROM stock_holdings h WHERE h.ticker = t.ticker AND h.deleted_at IS NULL)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	type item struct {
		ticker, locked, reported string
		days                     int
	}
	var items []item
	today, _ := time.Parse("2006-01-02", day)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ticker, &it.locked, &it.reported); err != nil {
			return false, err
		}
		it.days = -1
		if d, err := time.Parse("2006-01-02", it.reported); err == nil {
			it.days = int(today.Sub(d).Hours() / 24)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil // silent when there is nothing to re-score
	}
	// longest-waiting first, then by ticker
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && (items[j].days > items[j-1].days || (items[j].days == items[j-1].days && items[j].ticker < items[j-1].ticker)); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	lines := make([]string, 0, len(items))
	for i, it := range items {
		if i >= revisionSummaryMax {
			lines = append(lines, fmt.Sprintf("… and %d more", len(items)-revisionSummaryMax))
			break
		}
		switch {
		case it.days < 0:
			lines = append(lines, fmt.Sprintf("• %s — reported (date unknown), locked %s", it.ticker, it.locked))
		case it.days == 0:
			lines = append(lines, fmt.Sprintf("• %s — reported today (%s), locked %s", it.ticker, it.reported, it.locked))
		case it.days == 1:
			lines = append(lines, fmt.Sprintf("• %s — reported 1 day ago (%s), locked %s", it.ticker, it.reported, it.locked))
		default:
			lines = append(lines, fmt.Sprintf("• %s — reported %d days ago (%s), locked %s", it.ticker, it.days, it.reported, it.locked))
		}
	}
	noun := "theses"
	if len(items) == 1 {
		noun = "thesis"
	}
	text := fmt.Sprintf("📝 FT: %d held %s to re-score (the company reported after the thesis was locked)\n%s",
		len(items), noun, strings.Join(lines, "\n"))
	if n != nil {
		if err := n.Send(ctx, text); err != nil {
			return false, err
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO notification_log (holding_kind, holding_id, alert_kind, alert_day, acked_at, created_at)
		 VALUES ('system', 0, ?, ?, ?, ?)`, revisionSummaryKind, day, now.Unix(), now.Unix()); err != nil {
		return true, err
	}
	return true, nil
}
