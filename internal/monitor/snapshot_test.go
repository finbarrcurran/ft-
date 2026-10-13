package monitor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSnapshotMonitorAlertsOnceAtEightDaysAndRecovers(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := newMon(t, new(time.Time), &now, n)
	m.Checks = []Check{{Source: "snapshot", Label: "Weekly FT snapshot", MaxAge: 8 * 24 * time.Hour, ReminderEvery: 7 * 24 * time.Hour, Eval: snapshotEval(m.DB)}}
	put := func(ts time.Time) {
		if _, err := m.DB.Exec(`DELETE FROM ft_snapshots`); err != nil {
			t.Fatal(err)
		}
		if _, err := m.DB.Exec(`INSERT INTO ft_snapshots (week_start, generated_at, schema_version, payload_json) VALUES ('2026-10-12', ?, 1, '{}')`, ts.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	m.Tick(ctx) // no snapshot yet: nothing to be late for
	if len(n.msgs) != 0 {
		t.Fatalf("quiet before the first snapshot: %v", n.msgs)
	}
	put(time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC))
	m.Tick(ctx)
	now = now.Add(7 * 24 * time.Hour) // 7 days after Sunday 18:00 + 18h: 7.75 days
	m.Tick(ctx)
	if len(n.msgs) != 0 {
		t.Fatalf("7.75 days is within the 8-day allowance: %v", n.msgs)
	}
	now = now.Add(24 * time.Hour) // 8.75 days: a missed week
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "Weekly FT snapshot") {
		t.Fatalf("expected one alert: %v", n.msgs)
	}
	now = now.Add(24 * time.Hour)
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("no repeat inside a week: %v", n.msgs)
	}
	put(now) // the next snapshot succeeds
	m.Tick(ctx)
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "is back") {
		t.Fatalf("expected a recovery note: %v", n.msgs)
	}
}
