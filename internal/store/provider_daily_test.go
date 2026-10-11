package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestProviderDailyCounters(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	_ = st.RecordProviderHealth(ctx, "finnhub", true, "")
	_ = st.RecordProviderHealth(ctx, "finnhub", true, "")
	_ = st.RecordProviderHealth(ctx, "finnhub", false, errors.New("http 429: finnhub rate limit").Error())
	_ = st.RecordProviderHealth(ctx, "finnhub", false, "http 403: forbidden")
	_ = st.RecordProviderHealth(ctx, "finnhub", false, "http 503: down")
	_ = st.RecordProviderHealth(ctx, "finnhub", false, "request failed")
	_ = st.RecordProviderHealth(ctx, "yahoo", true, "")
	days, err := st.ProviderDaily(ctx, 7)
	if err != nil || len(days) != 2 {
		t.Fatalf("days: %v %v", days, err)
	}
	var f ProviderDay
	for _, d := range days {
		if d.Provider == "finnhub" {
			f = d
		}
	}
	if f.Calls != 6 || f.OK != 2 || f.Fail != 4 || f.Fail429 != 1 || f.Fail4xx != 1 || f.Fail5xx != 1 || f.FailOther != 1 {
		t.Fatalf("counters wrong: %+v", f)
	}
	// Retired providers are removed by the migration.
	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM provider_health WHERE provider IN ('twelvedata','cryptocompare')`).Scan(&n)
	if n != 0 {
		t.Fatal("retired providers must not remain in provider_health")
	}
}
