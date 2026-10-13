package monitor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStatementCoverageAlertsOnceRemindsWeeklyAndRecovers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) // 36 days after 2026-10-09
	n := &fakeNotifier{}
	m := newMon(t, new(time.Time), &now, n)
	set := func(end string) {
		_, _ = m.DB.ExecContext(ctx, `DELETE FROM etoro_statement_periods`)
		if _, err := m.DB.ExecContext(ctx, `INSERT INTO etoro_statement_periods (user_id, range_start, range_end, imported_at) VALUES (1, '2026-01-01', ?, 1)`, end); err != nil {
			t.Fatal(err)
		}
	}
	m.Checks = []Check{{Source: "statement_coverage", Label: "eToro statement coverage", MaxAge: 35 * 24 * time.Hour,
		ReminderEvery: 7 * 24 * time.Hour, Eval: statementCoverageEval(m.DB)}}

	m.Tick(ctx) // nothing imported: quiet
	if len(n.msgs) != 0 {
		t.Fatalf("no statements -> no alert: %v", n.msgs)
	}
	set("2026-10-09")
	now = now.Add(0)
	m.Tick(ctx) // first sight of a stale source is folded into the "monitor is on" message
	m.Tick(ctx)
	// Went healthy -> stale properly: start from a fresh coverage, then age it.
	set("2026-11-05")
	m.Tick(ctx) // 9 days: healthy (recovery note for the folded one, if any)
	base := len(n.msgs)
	now = now.Add(28 * 24 * time.Hour) // 2026-12-12: 37 days after 11-05
	m.Tick(ctx)
	if len(n.msgs) != base+1 || !strings.Contains(n.msgs[base], "upload a statement starting 2026-11-06") {
		t.Fatalf("expected one alert with the next start date, got %v", n.msgs[base:])
	}
	now = now.Add(24 * time.Hour)
	m.Tick(ctx)
	if len(n.msgs) != base+1 {
		t.Fatalf("no repeat within 7 days: %v", n.msgs[base:])
	}
	now = now.Add(7 * 24 * time.Hour)
	m.Tick(ctx)
	if len(n.msgs) != base+2 || !strings.Contains(n.msgs[base+1], "STILL down") {
		t.Fatalf("expected a weekly reminder: %v", n.msgs[base:])
	}
	set("2026-12-11") // a new statement is imported
	m.Tick(ctx)
	if len(n.msgs) != base+3 || !strings.Contains(n.msgs[base+2], "is back") {
		t.Fatalf("expected a recovery note: %v", n.msgs[base:])
	}
}

func TestStatementCoverageThirtySixDaysAlerts(t *testing.T) {
	ctx := context.Background()
	m := newMon(t, new(time.Time), &time.Time{}, &fakeNotifier{})
	if _, err := m.DB.ExecContext(ctx, `INSERT INTO etoro_statement_periods (user_id, range_start, range_end, imported_at) VALUES (1, '2026-01-01', '2026-10-09', 1)`); err != nil {
		t.Fatal(err)
	}
	ev := statementCoverageEval(m.DB)
	down, _, found, detail, err := ev(ctx, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)) // 36 days
	if err != nil || !down || !found || !strings.Contains(detail, "2026-10-10") {
		t.Fatalf("36 days: down=%v found=%v %q %v", down, found, detail, err)
	}
	down, _, _, _, _ = ev(ctx, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)) // 35 days
	if down {
		t.Fatal("35 days is still within the allowance")
	}
}
