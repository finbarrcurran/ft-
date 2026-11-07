package monitor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

type calEnv struct {
	t   *testing.T
	st  *store.Store
	n   *fakeNotifier
	mon *Monitor
	now time.Time
}

func newCalEnv(t *testing.T) *calEnv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	st.DB.Exec(`INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'fin@example.test', 'x', 0)`)
	st.DB.Exec(`DELETE FROM watchlist`)
	e := &calEnv{t: t, st: st, n: &fakeNotifier{}, now: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)}
	e.mon = &Monitor{DB: st.DB, Checks: CalendarChecks(st.DB), Notify: e.n, Now: func() time.Time { return e.now }}
	return e
}

func (e *calEnv) exec(q string, a ...any) {
	e.t.Helper()
	if _, err := e.st.DB.Exec(q, a...); err != nil {
		e.t.Fatal(err)
	}
}

func (e *calEnv) holding(tk string) {
	e.exec(`INSERT INTO stock_holdings (user_id, name, ticker, invested_usd, updated_at) VALUES (1, ?, ?, 100, 0)`, tk+" Inc", tk)
}
func (e *calEnv) watch(tk string) {
	e.exec(`INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, ?, 'stock', 0, 0)`, tk)
}

// row writes a ticker_calendar row updated "now".
func (e *calEnv) row(tk, status, date string, errCount int, perm bool, errText string) {
	p := 0
	if perm {
		p = 1
	}
	var d any
	if date != "" {
		d = date
	}
	e.exec(`INSERT OR REPLACE INTO ticker_calendar (ticker, earnings_date, earnings_status, last_error, error_count, error_permanent, updated_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, tk, d, status, errText, errCount, p, e.now.Unix())
}

func (e *calEnv) tick() []string {
	e.mon.Tick(context.Background())
	m := e.n.msgs
	e.n.msgs = nil
	return m
}

func TestReportDatesCheck_CleanDaySilent_404AlertsOnce_RecoveryMessage(t *testing.T) {
	e := newCalEnv(t)
	e.holding("AVGO")
	e.holding("GLD")
	e.watch("CEG")
	e.watch("RR.L")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	e.row("GLD", "NOT_APPLICABLE", "", 0, false, "")
	e.row("CEG", "DATE_ESTIMATED", "2026-11-09", 0, false, "")
	e.row("RR.L", "NOT_ANNOUNCED", "", 0, false, "")
	for i := 0; i < 3; i++ {
		if m := e.tick(); len(m) != 0 {
			t.Fatalf("clean day alerted: %q", m)
		}
		e.now = e.now.Add(time.Hour)
		e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	}
	// an injected 404 on a watchlist ticker
	e.row("CEG", "FETCH_ERROR", "2026-11-09", 1, true, "http 404: Quote not found for symbol: CEG")
	m := e.tick()
	if len(m) != 1 || !strings.Contains(m[0], "FT report dates: Report dates looks down") || !strings.Contains(m[0], "CEG") || !strings.Contains(m[0], "404") {
		t.Fatalf("404 alert: %q", m)
	}
	// still failing an hour later: no second message
	e.now = e.now.Add(time.Hour)
	e.row("CEG", "FETCH_ERROR", "2026-11-09", 2, true, "http 404: Quote not found for symbol: CEG")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	if m := e.tick(); len(m) != 0 {
		t.Fatalf("repeat alert: %q", m)
	}
	// cleared: one recovery message
	e.now = e.now.Add(time.Hour)
	e.row("CEG", "DATE_ESTIMATED", "2026-11-09", 0, false, "")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	m = e.tick()
	if len(m) != 1 || !strings.Contains(m[0], "is back") {
		t.Fatalf("recovery: %q", m)
	}
	e.now = e.now.Add(time.Hour)
	e.row("CEG", "DATE_ESTIMATED", "2026-11-09", 0, false, "")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	if m := e.tick(); len(m) != 0 {
		t.Fatalf("alert after recovery: %q", m)
	}
}

func TestReportDatesCheck_TransientNeedsThreeRuns_PastAsNextAndMissingCount(t *testing.T) {
	e := newCalEnv(t)
	e.holding("AVGO")
	e.holding("LLY")
	e.watch("CEG")
	e.watch("NOW")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	e.row("LLY", "REPORTED", "", 0, false, "")
	e.row("CEG", "DATE_KNOWN", "2026-11-09", 0, false, "")
	e.row("NOW", "DATE_KNOWN", "2027-01-27", 0, false, "")
	if m := e.tick(); len(m) != 0 {
		t.Fatalf("baseline: %q", m)
	}
	// a 429 that has lasted one and two runs: not yet
	for n := 1; n <= 2; n++ {
		e.now = e.now.Add(time.Hour)
		e.row("CEG", "FETCH_ERROR", "2026-11-09", n, false, "yahoo http 429 after 4 attempts (rate-limited)")
		e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
		if m := e.tick(); len(m) != 0 {
			t.Fatalf("alerted after %d transient failure(s): %q", n, m)
		}
	}
	e.now = e.now.Add(time.Hour)
	e.row("CEG", "FETCH_ERROR", "2026-11-09", 3, false, "yahoo http 429 after 4 attempts (rate-limited)")
	e.row("AVGO", "DATE_KNOWN", "2026-12-09", 0, false, "")
	if m := e.tick(); len(m) != 1 || !strings.Contains(m[0], "CEG") {
		t.Fatalf("third transient failure: %q", m)
	}
	// a past date still shown as next, and a ticker with no row at all, are faults too
	e.now = e.now.Add(time.Hour)
	e.row("CEG", "DATE_KNOWN", "2026-11-09", 0, false, "")
	e.row("AVGO", "DATE_KNOWN", "2026-11-01", 0, false, "")
	e.watch("NEWCO")
	m := e.tick()
	if len(m) != 0 { // already down -> still down: reminder only after 24h
		t.Fatalf("unexpected message while still down: %q", m)
	}
	rs := e.mon.Evaluate(context.Background())
	var detail string
	for _, r := range rs {
		if r.Source == "report_dates" {
			detail = r.Detail
		}
	}
	if !strings.Contains(detail, "past date shown as next: AVGO (2026-11-01)") || !strings.Contains(detail, "missing: NEWCO") {
		t.Fatalf("detail: %s", detail)
	}
}

func TestReportDatesCheck_NotJudgedWhenTheJobHasNotRun(t *testing.T) {
	e := newCalEnv(t)
	e.holding("AVGO") // no row at all, and no row updated in the last 6 hours
	if m := e.tick(); len(m) != 0 {
		t.Fatalf("alerted before the job ever ran: %q", m)
	}
}

func TestCalendarDigest_OncePerDay_SilentWhenNothingChanged(t *testing.T) {
	e := newCalEnv(t)
	e.holding("CEG")
	e.watch("NOW")
	e.holding("LLY")
	e.watch("BE")
	ctx := context.Background()
	set := func(tk, prev, cur string, est int, changedAgo time.Duration) {
		var p, c any
		if prev != "" {
			p = prev
		}
		if cur != "" {
			c = cur
		}
		e.exec(`INSERT OR REPLACE INTO ticker_calendar (ticker, earnings_date, earnings_is_estimate, earnings_status, prev_earnings_date, date_changed_at, updated_at)
			VALUES (?, ?, ?, 'DATE_KNOWN', ?, ?, ?)`, tk, c, est, p, e.now.Add(-changedAgo).Unix(), e.now.Unix())
	}
	// nothing changed in 24h -> nothing sent, nothing recorded
	set("CEG", "2026-11-05", "2026-11-09", 0, 30*time.Hour)
	if sent, err := CalendarDigest(ctx, e.st.DB, e.n, e.now); err != nil || sent || len(e.n.msgs) != 0 {
		t.Fatalf("silent day: sent %v err %v msgs %q", sent, err, e.n.msgs)
	}
	// changes within 24h for an owned and a watchlist ticker; a thesis-only ticker is ignored
	set("CEG", "2026-11-09", "2026-11-12", 0, 3*time.Hour)
	set("NOW", "", "2027-01-27", 1, 2*time.Hour)
	e.exec(`INSERT OR REPLACE INTO ticker_calendar (ticker, earnings_date, earnings_status, prev_earnings_date, date_changed_at, updated_at) VALUES ('ZZZ', '2027-03-01', 'DATE_KNOWN', '2027-02-01', ?, ?)`, e.now.Unix(), e.now.Unix())
	sent, err := CalendarDigest(ctx, e.st.DB, e.n, e.now)
	if err != nil || !sent || len(e.n.msgs) != 1 {
		t.Fatalf("digest: sent %v err %v msgs %q", sent, err, e.n.msgs)
	}
	msg := e.n.msgs[0]
	for _, want := range []string{"2 change(s)", "CEG [owned]: 2026-11-09 → 2026-11-12", "NOW [watchlist]: new report date 2027-01-27 (estimate)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("digest lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "ZZZ") {
		t.Fatalf("thesis-only ticker in the digest:\n%s", msg)
	}
	// a second call the same day does not repeat it
	if sent, _ := CalendarDigest(ctx, e.st.DB, e.n, e.now.Add(time.Hour)); sent || len(e.n.msgs) != 1 {
		t.Fatalf("repeat digest: sent %v msgs %q", sent, e.n.msgs)
	}
	// next day, the changes are older than 24h -> silent again
	if sent, _ := CalendarDigest(ctx, e.st.DB, e.n, e.now.Add(26*time.Hour)); sent {
		t.Fatal("digest repeated the old changes the next day")
	}
}
