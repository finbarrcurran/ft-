package monitor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

func TestInQuietHours(t *testing.T) {
	for _, c := range []struct {
		h    int
		want bool
	}{{0, true}, {9, true}, {10, false}, {15, false}, {21, false}, {22, true}, {23, true}} {
		if got := InQuietHours(time.Date(2026, 11, 20, c.h, 59, 0, 0, time.UTC)); got != c.want {
			t.Errorf("hour %d: got %v want %v", c.h, got, c.want)
		}
	}
}

// FT-170: an alert raised overnight is not sent; it goes out as one message at 10:00 UTC or later.
func TestMonitorAlertsHeldOvernightAndSentAfterTen(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	put := func(day string, n int) {
		if _, err := st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, ?, ?)
			ON CONFLICT(provider, day) DO UPDATE SET calls = excluded.calls`, day, n, n); err != nil {
			t.Fatal(err)
		}
	}
	put("2026-11-10", 6900)
	now := time.Date(2026, 11, 20, 21, 0, 0, 0, time.UTC) // before the window
	n := &fakeNotifier{}
	m := &Monitor{DB: st.DB, Now: func() time.Time { return now }, Notify: n, Checks: []Check{CoinGeckoBudgetCheck(st.DB)}}
	ctx := context.Background()
	m.Tick(ctx) // ok, first look
	put("2026-11-11", 100) // 7,000: down
	now = time.Date(2026, 11, 20, 23, 30, 0, 0, time.UTC)
	m.Tick(ctx)
	if len(n.msgs) != 0 {
		t.Fatalf("23:30 UTC: nothing may be sent, got %v", n.msgs)
	}
	now = time.Date(2026, 11, 21, 9, 50, 0, 0, time.UTC)
	m.Tick(ctx)
	if len(n.msgs) != 0 {
		t.Fatalf("09:50 UTC: still held, got %v", n.msgs)
	}
	var held int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM monitor_events WHERE delivered = 2`).Scan(&held)
	if held != 1 {
		t.Fatalf("held events = %d, want 1", held)
	}
	now = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "held overnight") || !strings.Contains(n.msgs[0], "soft stop") {
		t.Fatalf("10:00 UTC: want one held-alert message, got %v", n.msgs)
	}
	now = now.Add(10 * time.Minute)
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("held alerts must be sent once, got %d messages", len(n.msgs))
	}
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM monitor_events WHERE delivered = 2`).Scan(&held)
	if held != 0 {
		t.Fatalf("held after sending = %d, want 0", held)
	}
}

// A refused send keeps the alerts held; the next tick retries.
func TestHeldAlertsRetryWhenTelegramFails(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 2, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := &Monitor{DB: st.DB, Now: func() time.Time { return now }, Notify: n}
	m.alert(context.Background(), "x", "down", "🔴 test alert", now)
	now = time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC)
	n.fail = true
	m.flushHeld(context.Background(), now)
	var held int
	_ = st.DB.QueryRow(`SELECT COUNT(*) FROM monitor_events WHERE delivered = 2`).Scan(&held)
	if held != 1 {
		t.Fatalf("failed send: held = %d, want 1", held)
	}
	n.fail = false
	m.flushHeld(context.Background(), now.Add(10*time.Minute))
	if len(n.msgs) != 1 {
		t.Fatalf("retry: messages = %d, want 1", len(n.msgs))
	}
}
