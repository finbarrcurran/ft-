package monitor

import (
	"context"
	"strings"
	"testing"
	"time"

	"ft/internal/marketdata"
)

func setIndicator(t *testing.T, m *Monitor, id, asOf, fetchErr string, updated time.Time) {
	t.Helper()
	var fe any
	if fetchErr != "" {
		fe = fetchErr
	}
	if _, err := m.DB.Exec(`UPDATE crypto_indicators SET data_as_of = ?, fetch_error = ?, updated_at = ? WHERE id = ?`, asOf, fe, updated.Unix(), id); err != nil {
		t.Fatal(err)
	}
}

func indMon(t *testing.T, id string, now *time.Time, n *fakeNotifier) *Monitor {
	t.Helper()
	m := newMon(t, new(time.Time), now, n)
	for _, c := range indicatorChecksFor(m.DB) {
		if c.Source == "ci_"+id {
			m.Checks = []Check{c}
		}
	}
	if len(m.Checks) != 1 {
		t.Fatalf("no check for %s", id)
	}
	return m
}

// A fresh refresh timestamp with an old data date must alert; the text names the cause; one
// alert only, daily reminders for a daily series, recovery on new data.
func TestDailyIndicatorAlertsOnceAndRecovers(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := indMon(t, "pal_dxy", &now, n)
	setIndicator(t, m, "pal_dxy", "2026-10-02", "", now) // 11 days old, fetched just now
	m.Tick(ctx)
	if len(n.msgs) != 0 {
		t.Fatalf("11 days is fine: %v", n.msgs)
	}
	now = now.AddDate(0, 0, 4) // 2026-10-17: data is 15 days old; the refresh keeps succeeding
	setIndicator(t, m, "pal_dxy", "2026-10-02", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "SOURCE not updated") || !strings.Contains(n.msgs[0], "2026-10-02") {
		t.Fatalf("expected one source-not-updated alert, got %v", n.msgs)
	}
	now = now.Add(2 * time.Hour)
	setIndicator(t, m, "pal_dxy", "2026-10-02", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("no repeat inside 24 h: %v", n.msgs)
	}
	now = now.Add(24 * time.Hour)
	setIndicator(t, m, "pal_dxy", "2026-10-02", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "STILL down") {
		t.Fatalf("daily reminder expected: %v", n.msgs)
	}
	setIndicator(t, m, "pal_dxy", "2026-10-16", "", now) // new data
	m.Tick(ctx)
	if len(n.msgs) != 3 || !strings.Contains(n.msgs[2], "is back") {
		t.Fatalf("recovery expected: %v", n.msgs)
	}
}

func TestIndicatorFetchFailedIsNamed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 13, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := indMon(t, "universal_stablecoin_supply", &now, n)
	setIndicator(t, m, "universal_stablecoin_supply", "2026-10-13", "defillama: timeout", now.Add(-time.Hour)) // 10 days old, fetch erroring
	m.Tick(ctx)
	// First sight of an already-stale source is folded into the "monitor is on" message.
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "FETCH FAILED") || !strings.Contains(n.msgs[0], "defillama: timeout") {
		t.Fatalf("expected a fetch-failed message, got %v", n.msgs)
	}
	// A refresh that simply stopped running is also a fetch problem.
	ev := indicatorFreshnessEval(m.DB, "universal_stablecoin_supply")
	setIndicator(t, m, "universal_stablecoin_supply", "2026-10-13", "", now.Add(-48*time.Hour))
	down, _, _, detail, err := ev(ctx, now)
	if err != nil || !down || !strings.Contains(detail, "FETCH FAILED") {
		t.Fatalf("stopped refresh: down=%v %q %v", down, detail, err)
	}
}

// CFNAI: monthly, 7-day reminders, alert only at 95 days from the observation date.
func TestCFNAIAlertsAt95DaysAndRemindsWeekly(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 13, 0, 0, 0, time.UTC) // 85 days after 2026-08-01
	n := &fakeNotifier{}
	m := indMon(t, "pal_cfnai", &now, n)
	setIndicator(t, m, "pal_cfnai", "2026-08-01", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 0 {
		t.Fatalf("85 days is normal for CFNAI: %v", n.msgs)
	}
	now = time.Date(2026, 11, 5, 13, 0, 0, 0, time.UTC) // 96 days: the October release never came
	setIndicator(t, m, "pal_cfnai", "2026-08-01", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "SOURCE not updated") {
		t.Fatalf("expected one alert at 96 days: %v", n.msgs)
	}
	now = now.Add(3 * 24 * time.Hour)
	setIndicator(t, m, "pal_cfnai", "2026-08-01", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("no reminder before 7 days: %v", n.msgs)
	}
	now = now.Add(5 * 24 * time.Hour) // 8 days after the alert
	setIndicator(t, m, "pal_cfnai", "2026-08-01", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "STILL down") {
		t.Fatalf("weekly reminder expected: %v", n.msgs)
	}
	setIndicator(t, m, "pal_cfnai", "2026-09-01", "", now)
	m.Tick(ctx)
	if len(n.msgs) != 3 || !strings.Contains(n.msgs[2], "is back") {
		t.Fatalf("recovery expected: %v", n.msgs)
	}
}

func TestUS2YUsesTradingDaysAndNoDataDateIsQuiet(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	m := indMon(t, "pal_us2y", &now, &fakeNotifier{})
	ev := indicatorFreshnessEval(m.DB, "pal_us2y")
	if down, _, found, _, _ := ev(ctx, now); down || found {
		t.Fatal("no data date recorded yet must be quiet")
	}
	setIndicator(t, m, "pal_us2y", "2026-10-08", "", now)
	if down, _, _, detail, _ := ev(ctx, now); down || !strings.Contains(detail, "3 US trading days") {
		t.Fatalf("3 trading days must be fine: %v %q", down, detail)
	}
	if down, _, _, _, _ := ev(ctx, time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)); !down { // 5 trading days
		t.Fatal("5 trading days must alert")
	}
}
