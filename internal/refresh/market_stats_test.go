package refresh

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"ft/internal/market"
	"ft/internal/store"
)

type fakeStats struct {
	m map[string]*market.MarketStats
}

func (f fakeStats) Stats(_ context.Context, sym string) (*market.MarketStats, error) {
	if v, ok := f.m[sym]; ok {
		return v, nil
	}
	e := errors.New("http 404: Quote not found for symbol: " + sym)
	return nil, e
}

func fp(v float64) *float64 { return &v }

// SC-57: a US name gets all four values with source and as-of; an unknown (non-US) name is stored as nulls with the
// error and does not fail the pass; a later failure never clears values already stored.
func TestRefreshMarketStats(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`DELETE FROM watchlist`); err != nil { // migrations seed a few rows
		t.Fatal(err)
	}
	for _, tk := range []string{"AUR", "ZZZ.XX"} {
		if _, err := st.DB.Exec(`INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, ?, 'stock', 0, 0)`, tk); err != nil {
			t.Fatal(err)
		}
	}
	fs := fakeStats{m: map[string]*market.MarketStats{"AUR": {
		Currency: "USD", High52w: fp(9.5), Low52w: fp(2.1), ATH: fp(14.9), ATHDate: "2021-10-01",
		HistoryFrom: "2021-09-01", HistoryTo: "2026-11-01", ChartAsOf: 1793000000, MarketCap: fp(8e9), CapAsOf: 1793000100,
	}}}
	svc := &Service{Store: st, MarketStats: fs, Now: func() time.Time { return testNow }, CalendarGap: -1}
	sum := svc.RefreshMarketStats(context.Background())
	if sum.Total != 2 || sum.Answered != 1 || sum.Failed != 1 {
		t.Fatalf("summary %+v", sum)
	}
	m := st.MarketStatsMap(context.Background())
	a := m["AUR"]
	if a == nil || a.High52w.Value == nil || a.Low52w.Value == nil || a.ATH.Value == nil || a.MarketCap.Value == nil {
		t.Fatalf("AUR %+v", a)
	}
	if a.ATH.HistoryFrom == nil || *a.ATH.HistoryFrom != "2021-09-01" || a.High52w.Source != "yahoo" || a.MarketCap.AsOf == nil || *a.ATH.AsOf != 1793000000 {
		t.Fatalf("labels %+v", a)
	}
	z := m["ZZZ.XX"]
	if z == nil || z.High52w.Value != nil || z.ATH.Value != nil || z.MarketCap.Value != nil || z.Note == "" {
		t.Fatalf("ZZZ %+v", z)
	}
	// now AUR fails: stored values stay, note records the error
	svc.MarketStats = fakeStats{m: map[string]*market.MarketStats{}}
	svc.RefreshMarketStats(context.Background())
	a = st.MarketStatsMap(context.Background())["AUR"]
	if a.ATH.Value == nil || *a.ATH.Value != 14.9 || a.MarketCap.Value == nil || a.Note == "" {
		t.Fatalf("values must survive a failed pass: %+v", a)
	}
}

// A short history can put the all-time high below the 52-week high (a new warrant line): shown, with a note, not "fixed".
func TestMarketStats_ShortHistoryNote(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	err = st.UpsertMarketStats(context.Background(), store.MarketStatsWrite{Ticker: "WARR", ChartOK: true, High52w: fp(31.99), ATH: fp(2.59),
		ATHDate: "2026-11-06", HistFrom: "2026-11-06", HistTo: "2026-11-06", ChartAsOf: 1, Now: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := st.MarketStatsMap(context.Background())["WARR"]
	if m == nil || m.ATH.Value == nil || *m.ATH.Value != 2.59 || m.Note == "" {
		t.Fatalf("%+v", m)
	}
}
