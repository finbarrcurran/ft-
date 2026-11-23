package monitor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

// Jordi-46 Part B: the monitor sends one Telegram alert when the month's keyed CoinGecko calls reach the soft stop.
func TestCoinGeckoBudgetAlertsAtSoftStop(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	put := func(day string, n int) {
		if _, err := st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, ?, ?)
			ON CONFLICT(provider, day) DO UPDATE SET calls = excluded.calls`, day, n, n); err != nil {
			t.Fatal(err)
		}
	}
	put("2026-10-30", 9000) // last month does not count
	put("2026-11-10", 6900)
	n := &fakeNotifier{}
	m := &Monitor{DB: st.DB, Now: func() time.Time { return now }, Notify: n, Checks: []Check{CoinGeckoBudgetCheck(st.DB)}}
	// first look: past the boot grace? Run twice, as the monitor does, with time moving on
	m.Tick(context.Background())
	if len(n.msgs) != 0 {
		t.Fatalf("6,900 of 7,000: no alert, got %v", n.msgs)
	}
	put("2026-11-11", 100) // 7,000
	now = now.Add(time.Hour)
	m.Tick(context.Background())
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "7000") || !strings.Contains(n.msgs[0], "soft stop") {
		t.Fatalf("at the soft stop: want one alert, got %v", n.msgs)
	}
	now = now.Add(time.Hour)
	m.Tick(context.Background())
	if len(n.msgs) != 1 {
		t.Fatalf("no second alert within the reminder interval, got %d", len(n.msgs))
	}
	// the limit is configurable: raising it recovers the source
	_ = st.SetPreference(context.Background(), "coingecko_partb_soft_stop", "9000")
	now = now.Add(time.Hour)
	m.Tick(context.Background())
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "is back") {
		t.Fatalf("raising the soft stop: want a recovery message, got %v", n.msgs)
	}
}
