package visser46

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/health"
	"ft/internal/market"
	"ft/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGecko is a fake CoinGecko server: it counts every request by endpoint and can run a hook before answering.
type fakeGecko struct {
	srv    *httptest.Server
	mu     sync.Mutex
	hits   map[string]int
	total  int
	hook   func(n int, w http.ResponseWriter, r *http.Request) bool // true = the hook answered
	closes int                                                      // points per market_chart answer
	gotKey string
}

func newFake(t *testing.T) *fakeGecko {
	f := &fakeGecko{hits: map[string]int{}, closes: 366}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.total++
		n := f.total
		f.gotKey = r.Header.Get("x-cg-demo-api-key")
		ep := health.CoinGeckoEndpoint(r.URL.String())
		f.hits[ep]++
		hook := f.hook
		f.mu.Unlock()
		if hook != nil && hook(n, w, r) {
			return
		}
		switch ep {
		case "markets":
			var rows []map[string]any
			for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
				rows = append(rows, map[string]any{"id": id, "symbol": id, "current_price": 2.5, "market_cap": 1e9, "market_cap_rank": 77,
					"price_change_percentage_24h_in_currency": 1.0, "price_change_percentage_7d_in_currency": 2.0, "price_change_percentage_30d_in_currency": 3.0})
			}
			json.NewEncoder(w).Encode(rows)
		case "market_chart":
			var pts [][]float64
			today := time.Now().UTC().Truncate(24 * time.Hour)
			for i := f.closes - 1; i >= 0; i-- {
				pts = append(pts, []float64{float64(today.AddDate(0, 0, -i).UnixMilli()), 100 + float64(i)})
			}
			json.NewEncoder(w).Encode(map[string]any{"prices": pts})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGecko) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.total }

func setup(t *testing.T) (*Service, *fakeGecko, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	health.Init(st)
	health.InitEndpoint(st)
	t.Cleanup(func() { health.Init(nil); health.InitEndpoint(nil) })
	f := newFake(t)
	old := market.CoinGeckoBase
	market.CoinGeckoBase = f.srv.URL
	t.Cleanup(func() { market.CoinGeckoBase = old })
	oldB := RetryBackoff
	RetryBackoff = time.Millisecond
	t.Cleanup(func() { RetryBackoff = oldB })
	t.Setenv("FT_COINGECKO_API_KEY", "demo-key")
	svc := New(st)
	svc.Gap = time.Millisecond
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		return nil, errors.New("yahoo not stubbed")
	}
	return svc, f, st
}

func turnOn(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.SetPreference(context.Background(), store.PrefPartBEnabled, "on"); err != nil {
		t.Fatal(err)
	}
}

func TestShipsOffAndMakesNoCall(t *testing.T) {
	svc, f, st := setup(t)
	ctx := context.Background()
	if v, _ := st.GetPreference(ctx, store.PrefPartBEnabled); v != "off" {
		t.Fatalf("migration default = %q, want off", v)
	}
	if v, _ := st.GetPreference(ctx, store.PrefPartBSoftStop); v != "7000" {
		t.Fatalf("soft stop default = %q, want 7000", v)
	}
	if _, _, err := svc.RefreshOnce(ctx); !errors.Is(err, ErrKillSwitchOff) {
		t.Fatalf("refresh with switch off: %v", err)
	}
	rep, err := svc.Backfill(ctx, BackfillOptions{})
	if err != nil || !errors.Is(rep.StoppedBy, ErrKillSwitchOff) {
		t.Fatalf("backfill with switch off: %v / %v", err, rep.StoppedBy)
	}
	// the refresh loop only reads a preference per interval
	svc.Interval = 5 * time.Millisecond
	lctx, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()
	svc.RunRefreshLoop(lctx)
	if f.count() != 0 {
		t.Fatalf("calls with the kill switch off: %d, want 0", f.count())
	}
	// fail closed: a garbage value is off
	_ = st.SetPreference(ctx, store.PrefPartBEnabled, "ON ")
	if st.PartBEnabled(ctx) {
		t.Fatal("stray value must read as off")
	}
}

func TestNoKeyRefuses(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	t.Setenv("FT_COINGECKO_API_KEY", "")
	if _, _, err := svc.RefreshOnce(context.Background()); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
	if f.count() != 0 {
		t.Fatal("a keyless call was made")
	}
}

func TestRefreshOneKeyedCallAndCounters(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	n, missing, err := svc.RefreshOnce(ctx)
	if err != nil || n != 42 || len(missing) != 0 {
		t.Fatalf("n=%d missing=%v err=%v", n, missing, err)
	}
	if f.count() != 1 || f.hits["markets"] != 1 || f.gotKey != "demo-key" {
		t.Fatalf("hits=%v key=%q: want ONE keyed markets call", f.hits, f.gotKey)
	}
	snaps, _ := st.ListIndexSnapshots(ctx)
	if len(snaps) != 42 || *snaps["bittensor"].PriceUSD != 2.5 || *snaps["bittensor"].Chg30d != 3.0 || *snaps["bittensor"].MCapRank != 77 {
		t.Fatalf("snapshot rows: %d", len(snaps))
	}
	// counters: per endpoint and feature, keyed, and the monthly keyed total
	now := time.Now().UTC()
	rows, _ := st.CoinGeckoEndpointUsage(ctx, now.Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))
	if len(rows) != 1 || rows[0].Endpoint != "markets" || rows[0].Feature != FeatureRefresh || !rows[0].Keyed || rows[0].Calls != 1 || rows[0].OK != 1 {
		t.Fatalf("endpoint usage = %+v", rows)
	}
	if k, _ := st.CoinGeckoMonthKeyed(ctx, now); k != 1 {
		t.Fatalf("monthly keyed = %d", k)
	}
	// the call carried all 42 ids and the 24h,7d,30d changes
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillDryRunMakesNoCall(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st) // even with the switch on, a dry run calls nothing
	_ = st.InsertPriceHistoryBatch(context.Background(), "BTC", "crypto", series(365, time.Now().UTC().AddDate(0, 0, -1), 1))
	var out bytes.Buffer
	rep, err := svc.Backfill(context.Background(), BackfillOptions{DryRun: true, Out: &out})
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != 0 {
		t.Fatalf("dry run made %d calls", f.count())
	}
	if rep.PlannedCalls != 36 || len(rep.HeldSkipped) != 6 || len(rep.StocksPlanned) != 4 {
		t.Fatalf("plan = %+v", rep)
	}
	if !strings.Contains(out.String(), "CoinGecko calls it would make: 36") {
		t.Fatalf("output:\n%s", out.String())
	}
}

func TestBackfillStoresAndFlagsThin(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	f.hook = func(n int, w http.ResponseWriter, r *http.Request) bool { // HASH has only 90 days
		if strings.Contains(r.URL.Path, "/hash-2/") {
			f.mu.Lock()
			f.closes = 90
			f.mu.Unlock()
		} else {
			f.mu.Lock()
			f.closes = 366
			f.mu.Unlock()
		}
		return false
	}
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		var b []market.OHLCBar
		for i := 300; i >= 1; i-- {
			b = append(b, market.OHLCBar{Date: time.Now().UTC().AddDate(0, 0, -i).Format("2006-01-02"), Close: 50})
		}
		return b, nil
	}
	rep, err := svc.Backfill(ctx, BackfillOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.StoppedBy != nil || len(rep.Failed) != 0 {
		t.Fatalf("rep = %+v", rep)
	}
	if f.hits["market_chart"] != 36 || f.count() != 36 {
		t.Fatalf("calls = %v, want 42 market_chart", f.hits)
	}
	n, _, _ := st.CountPriceHistory(ctx, "TAO", "crypto")
	if n < 365 {
		t.Fatalf("TAO closes = %d", n)
	}
	// today's point is an intraday price, not a close
	if _, newest, _ := st.CountPriceHistory(ctx, "TAO", "crypto"); newest == time.Now().UTC().Format("2006-01-02") {
		t.Fatal("today's intraday point was stored as a close")
	}
	if len(rep.Thin) != 1 || !strings.HasPrefix(rep.Thin[0], "HASH") {
		t.Fatalf("thin = %v, want HASH only", rep.Thin)
	}
	if len(rep.StocksDone) != 4 {
		t.Fatalf("stocks = %v", rep.StocksDone)
	}
	if k, _ := st.CoinGeckoMonthKeyed(ctx, time.Now().UTC()); k != 36 {
		t.Fatalf("keyed counter = %d", k)
	}
	// run again: everything is backfilled, nothing is called
	before := f.count()
	rep2, _ := svc.Backfill(ctx, BackfillOptions{})
	if f.count()-before != 1 || rep2.PlannedCalls != 1 { // only HASH is still under 365
		t.Fatalf("second run: calls=%d planned=%d", f.count()-before, rep2.PlannedCalls)
	}
}

func TestKillSwitchFlippedMidBackfillStopsAtOnce(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	f.hook = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 3 {
			_ = st.SetPreference(context.Background(), store.PrefPartBEnabled, "off") // Fin flips it while call 3 is in flight
		}
		return false
	}
	rep, _ := svc.Backfill(context.Background(), BackfillOptions{})
	if f.count() != 3 {
		t.Fatalf("calls = %d, want exactly 3 (the one in flight finishes, the next is refused)", f.count())
	}
	if !errors.Is(rep.StoppedBy, ErrKillSwitchOff) || rep.TokensAttempted != 3 {
		t.Fatalf("rep = %+v", rep)
	}
}

func TestSoftStopStopsNewCalls(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	_ = st.SetPreference(ctx, store.PrefPartBSoftStop, "5")
	rep, _ := svc.Backfill(ctx, BackfillOptions{})
	if f.count() != 5 || !errors.Is(rep.StoppedBy, ErrSoftStop) {
		t.Fatalf("calls = %d stoppedBy = %v, want 5 and the soft stop", f.count(), rep.StoppedBy)
	}
	// the refresh is refused too: no new call
	if _, _, err := svc.RefreshOnce(ctx); !errors.Is(err, ErrSoftStop) || f.count() != 5 {
		t.Fatalf("refresh above the soft stop: err=%v calls=%d", err, f.count())
	}
	// raising the limit lets it run again
	_ = st.SetPreference(ctx, store.PrefPartBSoftStop, "7000")
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
}

func Test429RetryIsGatedAndCounted(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	f.hook = func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n == 1 {
			http.Error(w, "rate", 429)
			return true
		}
		return false
	}
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatalf("retry after a 429 should succeed: %v", err)
	}
	now := time.Now().UTC()
	rows, _ := st.CoinGeckoEndpointUsage(ctx, now.Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))
	if len(rows) != 1 || rows[0].Calls != 2 || rows[0].Fail429 != 1 || rows[0].OK != 1 {
		t.Fatalf("usage = %+v", rows)
	}
	// the switch goes off during the backoff: the retry is NOT made
	f.mu.Lock()
	f.total = 0
	f.hook = func(n int, w http.ResponseWriter, r *http.Request) bool {
		_ = st.SetPreference(ctx, store.PrefPartBEnabled, "off")
		http.Error(w, "rate", 429)
		return true
	}
	f.mu.Unlock()
	if _, _, err := svc.RefreshOnce(ctx); !errors.Is(err, ErrKillSwitchOff) || f.count() != 1 {
		t.Fatalf("err=%v calls=%d: want the retry refused", err, f.count())
	}
}

func TestDailyCloseBelowSoftStopNeedsNoCall(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// below the soft stop (1 keyed call of 7000): the close uses the stored snapshot, no extra call
	before := f.count()
	// a clock just after 00:00 UTC next day is not needed: use today with the snapshot fetched now
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		return []market.OHLCBar{{Date: time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), Close: 42}}, nil
	}
	res, err := svc.DailyClose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if f.count() != before || res.ExemptCall {
		t.Fatal("below the soft stop the daily close made a CoinGecko call")
	}
	if res.Appended != 36 || len(res.MissingTokens) != 0 || len(res.StocksUpdated) != 4 {
		t.Fatalf("res = %+v", res)
	}
	pts, _ := st.ListPriceHistory(ctx, "TAO", "crypto")
	if len(pts) != 1 || pts[0].Close != 2.5 || pts[0].Date != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("TAO closes = %v", pts)
	}
	// a snapshot older than today's 00:00 UTC is listed as missing, never filled
	svc.Now = func() time.Time { return time.Now().UTC().AddDate(0, 0, 1) }
	res, _ = svc.DailyClose(ctx)
	if res.Appended != 0 || len(res.MissingTokens) != 36 {
		t.Fatalf("stale snapshot: %+v", res)
	}
	// kill switch off: tokens still append from a fresh snapshot, stocks (Yahoo) are not fetched
	svc.Now = nil
	_ = st.SetPreference(ctx, store.PrefPartBEnabled, "off")
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		t.Fatal("Yahoo called with the switch off")
		return nil, nil
	}
	if res, _ = svc.DailyClose(ctx); res.Appended != 36 || len(res.StocksUpdated) != 0 {
		t.Fatalf("switch off: %+v", res)
	}
}

func series(n int, end time.Time, step float64) []store.PricePoint {
	out := make([]store.PricePoint, n)
	for i := 0; i < n; i++ {
		out[i] = store.PricePoint{Date: end.AddDate(0, 0, -(n - 1 - i)).Format("2006-01-02"), Close: 100 + step*float64(i)}
	}
	return out
}

func TestComputeFlags(t *testing.T) {
	up := make([]float64, 210)
	down := make([]float64, 210)
	for i := range up {
		up[i] = 100 + float64(i)
		down[i] = 400 - float64(i)
	}
	f := computeFlags(up)
	if !f.above50 || !f.above200 || !f.r50 || !f.r200 || !f.hasR200 {
		t.Fatalf("uptrend: %+v", f)
	}
	f = computeFlags(down)
	if f.above50 || f.above200 || f.r50 || f.r200 {
		t.Fatalf("downtrend: %+v", f)
	}
	f = computeFlags(up[:54]) // 54 closes: above 50D known, rising 50D (needs 55) not
	if !f.has50 || f.hasR50 || f.has200 {
		t.Fatalf("short: %+v", f)
	}
	// exact arithmetic: 55 closes, last 5 drop; SMA50 now vs 5 earlier
	c := make([]float64, 55)
	for i := range c {
		c[i] = 10
	}
	c[54] = 20
	f = computeFlags(c)
	if !f.above50 || !f.r50 { // 10.2 > 10
		t.Fatalf("%+v", f)
	}
}

func TestBreadthListsMissingAndNeverFills(t *testing.T) {
	svc, _, st := setup(t)
	ctx := context.Background()
	end := time.Now().UTC().AddDate(0, 0, -1)
	// BTC: 250 rising days (all four true). ZEC: 250 falling days. XMR: only 60 days (50D only). TAO: stale. rest: none.
	_ = st.InsertPriceHistoryBatch(ctx, "BTC", "crypto", series(250, end, 1))
	_ = st.InsertPriceHistoryBatch(ctx, "ZEC", "crypto", series(250, end, -0.1))
	_ = st.InsertPriceHistoryBatch(ctx, "XMR", "crypto", series(60, end, 1))
	_ = st.InsertPriceHistoryBatch(ctx, "TAO", "crypto", series(250, end.AddDate(0, 0, -30), 1))
	b, err := svc.Breadth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if b.Total.Members != 46 {
		t.Fatalf("members = %d", b.Total.Members)
	}
	// 50D: BTC yes, ZEC no, XMR yes => 2 of 3
	if b.Total.Above50.N != 3 || b.Total.Above50.Yes != 2 {
		t.Fatalf("above50 = %+v", b.Total.Above50)
	}
	// 200D: only BTC and ZEC have 200 closes
	if b.Total.Above200.N != 2 || b.Total.Above200.Yes != 1 || *b.Total.Above200.Pct != 50 {
		t.Fatalf("above200 = %+v", b.Total.Above200)
	}
	if b.Total.Rising200.N != 2 || b.Total.Rising200.Yes != 1 {
		t.Fatalf("rising200 = %+v", b.Total.Rising200)
	}
	miss := map[string]MissingSeries{}
	for _, m := range b.Missing {
		miss[m.Symbol] = m
	}
	if len(miss) != 44 { // all but BTC and ZEC; XMR is listed for 200D
		t.Fatalf("missing = %d, want 44", len(miss))
	}
	if !strings.Contains(strings.Join(miss["TAO"].Reasons, ";"), "stale") || !strings.Contains(strings.Join(miss["XMR"].Reasons, ";"), "above 200D: needs 200") || miss["GRASS"].Reasons[0] != "no stored closes" {
		t.Fatalf("reasons: %+v / %+v / %+v", miss["TAO"], miss["XMR"], miss["GRASS"])
	}
	// per vertical figures add up to the total
	sum := 0
	for _, g := range b.Groups {
		sum += g.Above50.N
	}
	if sum != b.Total.Above50.N {
		t.Fatalf("vertical sum %d != total %d", sum, b.Total.Above50.N)
	}
}

func TestBreadthGapsAreListed(t *testing.T) {
	svc, _, st := setup(t)
	ctx := context.Background()
	end := time.Now().UTC().AddDate(0, 0, -1)
	// 200 closes with a 140-day hole in the middle: the last 50 are clean, the 200-close window is not
	pts2 := append(series(100, end.AddDate(0, 0, -140), 1), series(100, end, 1)...)
	_ = st.InsertPriceHistoryBatch(ctx, "BTC", "crypto", pts2)
	b, _ := svc.Breadth(ctx)
	var m MissingSeries
	for _, x := range b.Missing {
		if x.Symbol == "BTC" {
			m = x
		}
	}
	if !strings.Contains(strings.Join(m.Reasons, ";"), "gaps") {
		t.Fatalf("BTC reasons = %v", m.Reasons)
	}
	if b.Total.Above50.N != 1 { // the last 50 closes are clean
		t.Fatalf("above50 = %+v", b.Total.Above50)
	}
}

// The scope fence: a full Part B run (refresh, backfill, daily close) leaves the watchlist, holdings, theses and
// alert tables exactly as they were.
func TestPartBLeavesProtectedTablesUntouched(t *testing.T) {
	svc, _, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		return []market.OHLCBar{{Date: time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02"), Close: 10}}, nil
	}
	protected := []string{"watchlist", "stock_holdings", "crypto_holdings", "crypto_theses", "holding_theses", "theses_index"}
	snap := func() map[string]string {
		out := map[string]string{}
		for _, tb := range protected {
			rows, err := st.DB.Query("SELECT * FROM " + tb + " ORDER BY 1")
			if err != nil {
				t.Fatalf("%s: %v", tb, err)
			}
			cols, _ := rows.Columns()
			var b strings.Builder
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				_ = rows.Scan(ptrs...)
				fmt.Fprintln(&b, vals...)
			}
			rows.Close()
			out[tb] = b.String()
		}
		return out
	}
	// give the watchlist a row for a member token so a stray write would show
	if _, err := st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO watchlist (user_id, ticker, kind, company_name, added_at, updated_at) VALUES (1,'TAO','crypto','Bittensor',1,1)`); err != nil {
		t.Fatal(err)
	}
	before := snap()
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Backfill(ctx, BackfillOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DailyClose(ctx); err != nil {
		t.Fatal(err)
	}
	after := snap()
	for _, tb := range protected {
		if before[tb] != after[tb] {
			t.Errorf("%s changed:\nbefore %q\nafter  %q", tb, before[tb], after[tb])
		}
	}
}

func exemptUsage(t *testing.T, st *store.Store) int {
	n, err := st.CoinGeckoFeatureCallsOn(context.Background(), time.Now().UTC().Format("2006-01-02"), FeatureDailyCloseExempt)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Above the soft stop the refresh is refused, but the daily close makes exactly ONE exempt keyed markets call a day.
func TestDailyCloseExemptCallAboveSoftStop(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	_ = st.SetPreference(ctx, store.PrefPartBSoftStop, "100")
	_, _ = st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, 150, 150)`, time.Now().UTC().Format("2006-01-02"))
	if _, _, err := svc.RefreshOnce(ctx); !errors.Is(err, ErrSoftStop) || f.count() != 0 {
		t.Fatalf("refresh above the stop: %v calls=%d", err, f.count())
	}
	res, err := svc.DailyClose(ctx)
	if err != nil || f.count() != 1 || f.hits["markets"] != 1 || !res.ExemptCall || res.Appended != 36 {
		t.Fatalf("err=%v calls=%d res=%+v", err, f.count(), res)
	}
	if exemptUsage(t, st) != 1 {
		t.Fatal("the exempt call is not counted under its own feature tag")
	}
	now := time.Now().UTC()
	rows, _ := st.CoinGeckoEndpointUsage(ctx, now.Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))
	if len(rows) != 1 || rows[0].Feature != FeatureDailyCloseExempt {
		t.Fatalf("usage = %+v", rows)
	}
	// a second attempt the same day (also after a restart: a fresh Service reads today's counter) makes no call
	if res, _ = svc.DailyClose(ctx); f.count() != 1 || res.ExemptCall {
		t.Fatalf("second attempt: calls=%d %+v", f.count(), res)
	}
	svc2 := New(st)
	svc2.StockBars = svc.StockBars
	if res, _ = svc2.DailyClose(ctx); f.count() != 1 || res.ExemptCall {
		t.Fatalf("after restart: calls=%d", f.count())
	}
}

func TestDailyCloseExemptRefusedBySwitchAndKey(t *testing.T) {
	for _, c := range []struct{ name, switchV, key string }{{"switch off", "off", "demo-key"}, {"no key", "on", ""}} {
		t.Run(c.name, func(t *testing.T) {
			svc, f, st := setup(t)
			ctx := context.Background()
			_ = st.SetPreference(ctx, store.PrefPartBEnabled, c.switchV)
			t.Setenv("FT_COINGECKO_API_KEY", c.key)
			_ = st.SetPreference(ctx, store.PrefPartBSoftStop, "100")
			_, _ = st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, 150, 150)`, time.Now().UTC().Format("2006-01-02"))
			res, err := svc.DailyClose(ctx)
			if err != nil || f.count() != 0 || res.ExemptCall || exemptUsage(t, st) != 0 {
				t.Fatalf("err=%v calls=%d %+v", err, f.count(), res)
			}
		})
	}
}

func TestDailyCloseExemptFailedAttemptUsesTheDay(t *testing.T) {
	svc, f, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	_ = st.SetPreference(ctx, store.PrefPartBSoftStop, "100")
	_, _ = st.DB.Exec(`INSERT INTO provider_daily (provider, day, calls, ok) VALUES ('coingecko_key', ?, 150, 150)`, time.Now().UTC().Format("2006-01-02"))
	f.hook = func(n int, w http.ResponseWriter, r *http.Request) bool { http.Error(w, "rate", 429); return true }
	_, _ = svc.DailyClose(ctx)
	_, _ = svc.DailyClose(ctx)
	if f.count() != 1 {
		t.Fatalf("calls = %d, want 1 (no retry, no second attempt)", f.count())
	}
}

// FT-165: the 04:00 job is the only writer for held coins; for the other tokens the close only inserts and never
// changes a row that already exists. Held stocks are left alone too.
func TestDailyCloseNeverOverwritesAndSkipsHeld(t *testing.T) {
	svc, _, st := setup(t)
	turnOn(t, st)
	ctx := context.Background()
	today := time.Now().UTC().Format("2006-01-02")
	if _, _, err := svc.RefreshOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// existing 04:00-style rows: a held coin (BTC) and a non-held token (TAO) with a value the snapshot (2.5) would change
	for _, sym := range []string{"BTC", "TAO"} {
		_ = st.InsertPriceHistoryBatch(ctx, sym, "crypto", []store.PricePoint{{Date: today, Close: 99}})
	}
	// COIN is held as a stock: its rows are not touched; HOOD is not held
	if _, err := st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO stock_holdings (user_id, name, ticker, invested_usd, updated_at) VALUES (1, 'Coinbase', 'COIN', 100, 1)`); err != nil {
		t.Fatal(err)
	}
	yday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	_ = st.InsertPriceHistoryBatch(ctx, "HOOD", "stock", []store.PricePoint{{Date: yday, Close: 77}})
	svc.StockBars = func(ctx context.Context, tk, rng string) ([]market.OHLCBar, error) {
		return []market.OHLCBar{{Date: yday, Close: 1}}, nil // would change every row it touches
	}
	res, err := svc.DailyClose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	closeOf := func(tk, kind, date string) float64 {
		pts, _ := st.ListPriceHistory(ctx, tk, kind)
		for _, p := range pts {
			if p.Date == date {
				return p.Close
			}
		}
		return -1
	}
	if got := closeOf("TAO", "crypto", today); got != 99 {
		t.Errorf("existing non-held row changed to %v", got)
	}
	if got := closeOf("BTC", "crypto", today); got != 99 {
		t.Errorf("held coin row changed to %v", got)
	}
	if got := closeOf("HOOD", "stock", yday); got != 77 {
		t.Errorf("existing stock row changed to %v", got)
	}
	if n, _, _ := st.CountPriceHistory(ctx, "COIN", "stock"); n != 0 {
		t.Errorf("held stock COIN got %d rows from Part B", n)
	}
	// a token with no row yet still gets its close; held coins are listed as skipped
	if got := closeOf("VIRTUAL", "crypto", today); got != 2.5 {
		t.Errorf("new row = %v, want 2.5", got)
	}
	if len(res.HeldSkipped) < 2 { // BTC and COIN at least
		t.Errorf("held skipped = %v", res.HeldSkipped)
	}
	// the backfill skips held coins too and does not overwrite
	rep, _ := svc.Backfill(ctx, BackfillOptions{DryRun: true})
	found := map[string]bool{}
	for _, h := range rep.HeldSkipped {
		found[h] = true
	}
	if !found["BTC"] || !found["COIN"] || found["TAO"] {
		t.Errorf("backfill held skip = %v", rep.HeldSkipped)
	}
}
