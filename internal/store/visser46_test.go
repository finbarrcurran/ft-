package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPartBSwitchesAndCounters(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if st.PartBEnabled(ctx) || st.PartBSoftStop(ctx) != 7000 {
		t.Fatal("defaults: off and 7000")
	}
	_ = st.SetPreference(ctx, PrefPartBSoftStop, "garbage")
	if st.PartBSoftStop(ctx) != 7000 {
		t.Fatal("invalid soft stop must fall back to 7000")
	}
	// provider_daily drives the monthly keyed counter; the first of the month is the boundary
	for _, d := range []string{"2026-10-31", "2026-11-01", "2026-11-14"} {
		_, _ = st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, 10, 10)`, d)
	}
	_, _ = st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko', '2026-11-02', 99, 99)`)
	n, _ := st.CoinGeckoMonthKeyed(ctx, time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	if n != 20 {
		t.Fatalf("november keyed = %d, want 20 (keyless and October excluded)", n)
	}
	at := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	_ = st.recordCoinGeckoCallOn(ctx, at, "markets", "", true, true, false)
	_ = st.recordCoinGeckoCallOn(ctx, at, "markets", "", true, false, true)
	_ = st.recordCoinGeckoCallOn(ctx, at, "markets", "", false, true, false)
	_ = st.recordCoinGeckoCallOn(ctx, at, "global", "", false, true, false)
	rows, _ := st.CoinGeckoEndpointUsage(ctx, "2026-11-01", "2026-12-01")
	got := map[string]EndpointUsage{}
	for _, r := range rows {
		k := r.Endpoint
		if r.Keyed {
			k += "/keyed"
		}
		got[k] = r
	}
	if got["markets/keyed"].Calls != 2 || got["markets/keyed"].Fail429 != 1 || got["markets"].Calls != 1 || got["global"].Calls != 1 {
		t.Fatalf("usage = %+v", rows)
	}
}
