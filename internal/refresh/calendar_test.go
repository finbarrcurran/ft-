package refresh

// SC-56 — report-date state machine and job, driven by fixtures taken from the 2026-11-06 dry run
// (internal/market/testdata) and a scratch database. Nothing here calls Yahoo.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ft/internal/domain"
	"ft/internal/market"
	"ft/internal/monitor"
	"ft/internal/store"
)

var testNow = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC) // "today" is 2026-11-07

// fakeSource answers from fixtures. events maps a provider symbol to a calendarEvents fixture file (or an
// error); pages maps a symbol to second-source rows.
type fakeSource struct {
	mu       sync.Mutex
	events   map[string]any // string = fixture file, error = failure, *market.CalendarEvents = literal
	pages    map[string][]market.EarningsRow
	pageErr  map[string]error
	evCalls  []string
	pgCalls  []string
	fixtures string
}

func (f *fakeSource) Events(_ context.Context, symbol string) (*market.CalendarEvents, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evCalls = append(f.evCalls, symbol)
	switch v := f.events[symbol].(type) {
	case nil:
		return nil, &market.HTTPError{Status: 404, Msg: "http 404: Quote not found for symbol: " + symbol}
	case error:
		return nil, v
	case *market.CalendarEvents:
		return v, nil
	case string:
		b, err := os.ReadFile(filepath.Join("..", "market", "testdata", v))
		if err != nil {
			return nil, err
		}
		return market.ParseCalendarEvents(symbol, b)
	}
	return nil, errors.New("fakeSource: bad fixture")
}

func (f *fakeSource) EarningsPage(_ context.Context, symbol string) ([]market.EarningsRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pgCalls = append(f.pgCalls, symbol)
	if err := f.pageErr[symbol]; err != nil {
		return nil, err
	}
	return f.pages[symbol], nil
}

func (f *fakeSource) pageCallsFor(sym string) int {
	n := 0
	for _, s := range f.pgCalls {
		if s == sym {
			n++
		}
	}
	return n
}

type env struct {
	t   *testing.T
	st  *store.Store
	src *fakeSource
	svc *Service
	now time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`); err != nil {
		t.Fatal(err)
	}
	// The scratch DB starts with the migration's seeded watchlist rows; the tests want a known set.
	if _, err := st.DB.Exec(`DELETE FROM watchlist`); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, src: &fakeSource{events: map[string]any{}, pages: map[string][]market.EarningsRow{}, pageErr: map[string]error{}}, now: testNow}
	e.svc = &Service{Store: st, Calendar: e.src, Now: func() time.Time { return e.now }, CalendarGap: time.Nanosecond}
	return e
}

func (e *env) holding(ticker string) {
	e.t.Helper()
	tk := ticker
	if _, err := e.st.InsertStockHolding(context.Background(), &domain.StockHolding{UserID: 1, Name: ticker + " Inc", Ticker: &tk, InvestedUSD: 100}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) watch(ticker string) {
	e.t.Helper()
	if _, err := e.st.DB.Exec(`INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, ?, 'stock', 0, 0)`, ticker); err != nil {
		e.t.Fatal(err)
	}
}

// thesis adds a live thesis row (no sync involved).
func (e *env) thesis(ticker, locked string) {
	e.t.Helper()
	if _, err := e.st.DB.Exec(`INSERT INTO theses_index (ticker, adapter, version, status, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, created_at, updated_at)
		VALUES (?, 'pharma', 1, 'locked', ?, 'theses/pharma/x.md', 'u', 'x', 'x', 'sha', 0, 0)`, ticker, locked); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) run() CalendarSummary {
	e.t.Helper()
	return e.svc.RefreshCalendar(context.Background())
}

func (e *env) cal(ticker string) *store.TickerCalendar {
	e.t.Helper()
	c, err := e.st.GetTickerCalendar(context.Background(), ticker)
	if err != nil || c == nil {
		e.t.Fatalf("%s: no calendar row (%v)", ticker, err)
	}
	return c
}

func (e *env) want(ticker, status, date string) *store.TickerCalendar {
	e.t.Helper()
	c := e.cal(ticker)
	got := ""
	if c.EarningsDate != nil {
		got = *c.EarningsDate
	}
	if c.Status != status || got != date {
		e.t.Fatalf("%s: status %s date %q, want %s %q (err %q)", ticker, c.Status, got, status, date, c.LastError)
	}
	return c
}

func rows(dates ...string) []market.EarningsRow {
	var out []market.EarningsRow
	for _, d := range dates {
		reported := strings.HasPrefix(d, "r:")
		out = append(out, market.EarningsRow{Date: strings.TrimPrefix(d, "r:"), Reported: reported})
	}
	return out
}

func TestCalendar_P1_CoverageSymbolMappingAndNotApplicable(t *testing.T) {
	e := newEnv(t)
	e.holding("AVGO") // holding
	e.holding("GLD")  // ETF
	e.watch("CEG")    // watchlist only
	e.thesis("BESI", "2026-06-13")
	e.thesis("BYD", "2026-06-11")
	e.thesis("WBA", "2026-05-18")
	e.src.events = map[string]any{
		"AVGO": "calendar_AVGO.json", "CEG": "calendar_CEG.json",
		"BESI.AS": "calendar_AVGO.json", // only the mapped symbol exists; bare BESI is a 404
		"1211.HK": "calendar_CRWV.json",
	}
	sum := e.run()
	if sum.Total != 6 {
		t.Fatalf("set size %d, want 6 (holdings + watchlist + theses, deduplicated)", sum.Total)
	}
	e.want("AVGO", store.CalDateKnown, "2026-12-09")
	e.want("CEG", store.CalDateKnown, "2026-11-09")
	e.want("BESI", store.CalDateKnown, "2026-12-09") // resolved through BESI.AS
	e.want("BYD", store.CalDateKnown, "2026-11-10")  // resolved through 1211.HK
	for _, na := range []string{"GLD", "WBA"} {
		c := e.want(na, store.CalNotApplicable, "")
		if c.NotApplicableWhy == "" {
			t.Fatalf("%s: no reason", na)
		}
	}
	for _, sym := range e.src.evCalls {
		if sym == "GLD" || sym == "WBA" || sym == "BESI" || sym == "BYD" {
			t.Fatalf("provider asked for %q (flagged, or not mapped)", sym)
		}
	}
	// holdings keep their back-compat column
	var ed *string
	if err := e.st.DB.QueryRow(`SELECT earnings_date FROM stock_holdings WHERE ticker = 'AVGO'`).Scan(&ed); err != nil || ed == nil || *ed != "2026-12-09" {
		t.Fatalf("stock_holdings.earnings_date = %v (%v)", ed, err)
	}
}

func TestCalendar_P2_RollForwardFromTheSecondSource(t *testing.T) {
	e := newEnv(t)
	for _, tk := range []string{"LLY", "AEM", "PAAS"} {
		e.holding(tk)
	}
	e.src.events = map[string]any{"LLY": "calendar_LLY.json", "AEM": "calendar_AEM.json", "PAAS": "calendar_PAAS.json"}
	// the three next dates the 2026-11-06 dry run found at the second source
	e.src.pages["LLY"] = rows("2027-02-03", "r:2026-10-29")
	e.src.pages["AEM"] = rows("2027-02-11", "r:2026-10-28")
	e.src.pages["PAAS"] = rows("2027-02-24", "r:2026-11-04")
	e.run()
	for tk, want := range map[string]string{"LLY": "2027-02-03", "AEM": "2027-02-11", "PAAS": "2027-02-24"} {
		c := e.want(tk, store.CalDateEstimated, want)
		if c.Source != "yahoo_earnings_page" || !c.IsEstimate || c.LastReportedDate == nil {
			t.Fatalf("%s: %+v", tk, c)
		}
	}
	if got := *e.cal("LLY").LastReportedDate; got != "2026-10-29" {
		t.Fatalf("LLY last reported %s", got)
	}
	// nothing is shown as next if it is in the past
	for _, tk := range []string{"LLY", "AEM", "PAAS"} {
		if d := e.cal(tk).EarningsDate; d == nil || *d < "2026-11-07" {
			t.Fatalf("%s next date %v", tk, d)
		}
	}
}

func TestCalendar_P2_NoSecondSourceDateGivesReportedNotPast(t *testing.T) {
	e := newEnv(t)
	e.holding("BE")
	e.src.events = map[string]any{"BE": "calendar_BE.json"} // 2026-10-22, past
	e.src.pages["BE"] = rows("r:2026-10-22")                // no future row
	e.run()
	c := e.want("BE", store.CalReported, "")
	if c.LastReportedDate == nil || *c.LastReportedDate != "2026-10-22" {
		t.Fatalf("last reported %v", c.LastReportedDate)
	}
}

func TestCalendar_P2_SecondSourceAskedAtMostDailyAndBudgeted(t *testing.T) {
	e := newEnv(t)
	e.holding("BE")
	e.src.events = map[string]any{"BE": "calendar_BE.json"}
	e.src.pages["BE"] = rows("r:2026-10-22")
	e.run()
	e.now = e.now.Add(time.Hour)
	e.run()
	e.now = e.now.Add(time.Hour)
	e.run()
	if n := e.src.pageCallsFor("BE"); n != 1 {
		t.Fatalf("second source asked %d times within a day, want 1", n)
	}
	e.now = e.now.Add(25 * time.Hour)
	e.run()
	if n := e.src.pageCallsFor("BE"); n != 2 {
		t.Fatalf("after a day: %d calls, want 2", n)
	}

	// budget: 12 past-dated tickers, at most secondPerRun page fetches in one run
	e2 := newEnv(t)
	for i := 0; i < 12; i++ {
		tk := string(rune('A'+i)) + "XX"
		e2.holding(tk)
		e2.src.events[tk] = "calendar_BE.json"
		e2.src.pages[tk] = rows("2027-02-01")
	}
	e2.run()
	if n := len(e2.src.pgCalls); n != secondPerRun {
		t.Fatalf("page fetches in one run = %d, want %d", n, secondPerRun)
	}
	e2.run() // the next run finishes the rest
	if n := len(e2.src.pgCalls); n != 12 {
		t.Fatalf("after the second run: %d page fetches, want 12", n)
	}
}

func TestCalendar_P2_NeverClearOnPartialData(t *testing.T) {
	e := newEnv(t)
	e.holding("AVGO")
	e.src.events = map[string]any{"AVGO": "calendar_AVGO.json"}
	e.run()
	e.want("AVGO", store.CalDateKnown, "2026-12-09")
	// Yahoo now answers with an ex-dividend date and no earnings date (SU.PA-shaped fixture)
	e.src.events["AVGO"] = "calendar_SU.PA.json"
	e.now = e.now.Add(time.Hour)
	e.run()
	c := e.want("AVGO", store.CalDateKnown, "2026-12-09")
	if c.ExDividendDate == nil || *c.ExDividendDate != "2026-05-11" {
		t.Fatalf("ex-dividend not updated: %v", c.ExDividendDate)
	}
	var h *string
	_ = e.st.DB.QueryRow(`SELECT earnings_date FROM stock_holdings WHERE ticker = 'AVGO'`).Scan(&h)
	if h == nil || *h != "2026-12-09" {
		t.Fatalf("stock_holdings.earnings_date cleared: %v", h)
	}
	// and an answer that lacks the ex-dividend date does not clear that either
	e.src.events["AVGO"] = &market.CalendarEvents{EarningsDates: []string{"2026-12-09"}}
	e.now = e.now.Add(time.Hour)
	e.run()
	if c := e.cal("AVGO"); c.ExDividendDate == nil {
		t.Fatal("ex-dividend cleared by an answer without one")
	}
}

func TestCalendar_P2_StoredDatePassingBecomesReported(t *testing.T) {
	e := newEnv(t)
	e.holding("CRWV")
	e.src.events = map[string]any{"CRWV": "calendar_CRWV.json"} // 2026-11-10
	e.run()
	e.want("CRWV", store.CalDateKnown, "2026-11-10")
	// Three days later Yahoo says nothing (no earnings date): the stored date has passed.
	e.src.events["CRWV"] = &market.CalendarEvents{}
	e.now = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	e.run()
	c := e.want("CRWV", store.CalReported, "")
	if c.LastReportedDate == nil || *c.LastReportedDate != "2026-11-10" {
		t.Fatalf("last reported %v", c.LastReportedDate)
	}
}

func TestCalendar_P3_StatesAndProvenance(t *testing.T) {
	e := newEnv(t)
	e.holding("SU.PA")
	e.holding("RR.L")
	e.holding("NOW")
	e.holding("GLD")
	e.src.events = map[string]any{"SU.PA": "calendar_SU.PA.json", "RR.L": "calendar_RR.L.json", "NOW": "calendar_NOW.json"}
	e.src.pages["SU.PA"] = rows("2027-02-18")
	e.src.pages["RR.L"] = rows() // the page lists nothing
	sum := e.run()
	c := e.want("SU.PA", store.CalDateEstimated, "2027-02-18")
	if c.Source != "yahoo_earnings_page" || c.FetchedAt == nil {
		t.Fatalf("SU.PA provenance: %+v", c)
	}
	e.want("RR.L", store.CalNotAnnounced, "")
	n := e.want("NOW", store.CalDateEstimated, "2027-01-27")
	if n.Source != "yahoo_calendar" || !n.IsEstimate {
		t.Fatalf("NOW: %+v", n)
	}
	e.want("GLD", store.CalNotApplicable, "")
	if sum.ByState[store.CalDateEstimated] != 2 || sum.ByState[store.CalNotAnnounced] != 1 || sum.ByState[store.CalNotApplicable] != 1 {
		t.Fatalf("state counts: %v", sum.ByState)
	}
}

func TestCalendar_P3_FetchErrorsAreStoredLoggedAndRecover(t *testing.T) {
	e := newEnv(t)
	e.holding("AVGO")
	e.holding("CEG")
	e.src.events = map[string]any{"AVGO": "calendar_AVGO.json", "CEG": "calendar_CEG.json"}
	e.run()
	// an injected 404 for CEG; a 429 for AVGO
	e.src.events["CEG"] = nil
	e.src.events["AVGO"] = &market.HTTPError{Status: 429, Msg: "yahoo http 429 after 4 attempts (rate-limited)"}
	e.now = e.now.Add(time.Hour)
	sum := e.run()
	ceg := e.cal("CEG")
	if ceg.Status != store.CalFetchError || !strings.Contains(ceg.LastError, "404") || !ceg.ErrorPermanent || ceg.ErrorCount != 1 {
		t.Fatalf("CEG after 404: %+v", ceg)
	}
	if ceg.EarningsDate == nil || *ceg.EarningsDate != "2026-11-09" {
		t.Fatalf("a failed fetch must keep the stored date, got %v", ceg.EarningsDate)
	}
	avgo := e.cal("AVGO")
	if avgo.Status != store.CalFetchError || avgo.ErrorPermanent || !strings.Contains(avgo.LastError, "429") {
		t.Fatalf("AVGO after 429: %+v", avgo)
	}
	if len(sum.Failed) != 2 || sum.ByState[store.CalFetchError] != 2 {
		t.Fatalf("summary: failed %v states %v", sum.Failed, sum.ByState)
	}
	// second consecutive failure counts up; recovery clears everything
	e.now = e.now.Add(time.Hour)
	e.run()
	if c := e.cal("CEG"); c.ErrorCount != 2 {
		t.Fatalf("error count %d", c.ErrorCount)
	}
	e.src.events["CEG"] = "calendar_CEG.json"
	e.src.events["AVGO"] = "calendar_AVGO.json"
	e.now = e.now.Add(time.Hour)
	e.run()
	c := e.want("CEG", store.CalDateKnown, "2026-11-09")
	if c.LastError != "" || c.ErrorCount != 0 || c.ErrorPermanent {
		t.Fatalf("CEG did not recover cleanly: %+v", c)
	}
}

func TestCalendar_P3_DateChangeIsRecordedButNotTheFirstPopulation(t *testing.T) {
	e := newEnv(t)
	e.holding("CEG")
	e.src.events = map[string]any{"CEG": "calendar_CEG.json"} // 2026-11-09
	e.run()
	if c := e.cal("CEG"); c.DateChangedAt != nil {
		t.Fatal("first population must not count as a change")
	}
	e.src.events["CEG"] = &market.CalendarEvents{EarningsDates: []string{"2026-11-12"}}
	e.now = e.now.Add(time.Hour)
	e.run()
	c := e.cal("CEG")
	if c.DateChangedAt == nil || c.PrevEarningsDate == nil || *c.PrevEarningsDate != "2026-11-09" || *c.EarningsDate != "2026-11-12" {
		t.Fatalf("change not recorded: %+v", c)
	}
}

func TestCalendar_P2_ReportedHistoryBackfillForThesisTickers(t *testing.T) {
	e := newEnv(t)
	e.holding("CEG")
	e.thesis("CEG", "2026-05-28")
	e.src.events = map[string]any{"CEG": "calendar_CEG.json"} // next 2026-11-09, last report unknown
	e.src.pages["CEG"] = rows("2026-11-09", "r:2026-08-05")
	e.run()
	c := e.want("CEG", store.CalDateKnown, "2026-11-09") // the primary date stands
	if c.Source != "yahoo_calendar" || c.LastReportedDate == nil || *c.LastReportedDate != "2026-08-05" {
		t.Fatalf("CEG: %+v", c)
	}
}

type memNotifier struct{ msgs []string }

func (m *memNotifier) Send(_ context.Context, text string) error {
	m.msgs = append(m.msgs, text)
	return nil
}

// SC-56 P4 end to end: the hourly job ticks the report-dates check; an injected 404 gives one alert and,
// once cleared, one recovery message; a clean day gives none.
func TestCalendar_P4_InjectedFetchErrorAlertsOnceThenRecovers(t *testing.T) {
	e := newEnv(t)
	n := &memNotifier{}
	e.svc.Alerts = &monitor.Monitor{DB: e.st.DB, Checks: monitor.CalendarChecks(e.st.DB), Notify: n, Now: func() time.Time { return e.now }}
	e.holding("AVGO")
	e.watch("CEG")
	e.src.events = map[string]any{"AVGO": "calendar_AVGO.json", "CEG": "calendar_CEG.json"}
	for i := 0; i < 3; i++ { // clean hours
		e.run()
		e.now = e.now.Add(time.Hour)
	}
	if len(n.msgs) != 0 {
		t.Fatalf("clean day alerted: %q", n.msgs)
	}
	e.src.events["CEG"] = nil // Yahoo now answers 404 for CEG
	e.run()
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "CEG") || !strings.Contains(n.msgs[0], "404") {
		t.Fatalf("after the 404: %q", n.msgs)
	}
	e.now = e.now.Add(time.Hour)
	e.run() // still failing, same day: no repeat
	if len(n.msgs) != 1 {
		t.Fatalf("repeat alert: %q", n.msgs)
	}
	e.src.events["CEG"] = "calendar_CEG.json"
	e.now = e.now.Add(time.Hour)
	e.run()
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "is back") {
		t.Fatalf("recovery: %q", n.msgs)
	}
}

// History lookups (last reported date) may never crowd out the lookups that find a missing next date.
func TestCalendar_BudgetKeepsRoomForNextDateLookups(t *testing.T) {
	e := newEnv(t)
	// 6 thesis tickers with a future date but unknown history (history lookups) ...
	for i := 0; i < 6; i++ {
		tk := string(rune('A'+i)) + "HH"
		e.holding(tk)
		e.thesis(tk, "2026-05-28")
		e.src.events[tk] = "calendar_CEG.json"
		e.src.pages[tk] = rows("r:2026-08-05")
	}
	// ... and 6 tickers whose date has passed (next-date lookups).
	for i := 0; i < 6; i++ {
		tk := string(rune('A'+i)) + "PP"
		e.holding(tk)
		e.src.events[tk] = "calendar_BE.json"
		e.src.pages[tk] = rows("2027-02-01")
	}
	e.run()
	hist, next := 0, 0
	for _, s := range e.src.pgCalls {
		if strings.HasSuffix(s, "HH") {
			hist++
		} else {
			next++
		}
	}
	if next != 6 || hist != secondPerRun-6 || hist > historyPerRun {
		t.Fatalf("page fetches: next-date %d (want 6), history %d (want %d: what is left of %d), cap %d", next, hist, secondPerRun-6, secondPerRun, historyPerRun)
	}
}
