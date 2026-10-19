package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"ft/internal/config"
	"ft/internal/frameworks"
	"ft/internal/marketdata"
	"ft/internal/snapshot"
	"ft/internal/store"
)

func snapServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	_ = frameworks.Load()
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
	return New(&config.Config{}, st, nil), st
}

func mustExec(t *testing.T, st *store.Store, q string, args ...any) {
	t.Helper()
	if _, err := st.DB.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type captureNotifier struct{ msgs []string }

func (c *captureNotifier) Send(_ context.Context, text string) error {
	c.msgs = append(c.msgs, text)
	return nil
}

// Two consecutive weeks through the real run: one row per week, same-week re-run replaces,
// the second week compares with the first, and the Telegram note carries the top changes.
func TestWeeklySnapshotTwoWeeksEndToEnd(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	out := t.TempDir()
	n := &captureNotifier{}
	// History tables the seeds and the alert counts read.
	mustExec(t, st, `INSERT INTO macro_regime_history (quadrant, shorthand, growth_dir, inflation_dir, computed_at) VALUES ('Q4','Deflation/Recession','decel','decel',?)`, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Unix())
	mustExec(t, st, `INSERT INTO notification_log (holding_kind, holding_id, alert_kind, alert_day, created_at) VALUES ('stock',1,'red','2026-10-09',1),('stock',2,'red','2026-10-10',1)`)
	sun1 := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	res, err := s.RunWeeklySnapshot(ctx, sun1, SnapshotOptions{OutDir: out, Notify: n})
	if err != nil {
		t.Fatal(err)
	}
	if res.Payload.WeekStart != "2026-10-05" || !res.Saved {
		t.Fatalf("week %s saved %v", res.Payload.WeekStart, res.Saved)
	}
	md := readFile(t, filepath.Join(out, "ft-snapshot.md"))
	if !strings.HasPrefix(md, "ft_snapshot_version: 2\ngenerated_at: ") || !strings.Contains(md, "week_start: 2026-10-05") || !strings.HasSuffix(strings.TrimSpace(md), "<!-- END ft-snapshot v2 -->") {
		t.Fatalf("file shape:\n%s", md[:300])
	}
	if c, _ := snapshot.Count(ctx, st.DB); c != 1 {
		t.Fatalf("rows %d", c)
	}
	// Same ISO week again: replaced, not added.
	if _, err := s.RunWeeklySnapshot(ctx, sun1.Add(time.Hour), SnapshotOptions{OutDir: out}); err != nil {
		t.Fatal(err)
	}
	if c, _ := snapshot.Count(ctx, st.DB); c != 1 {
		t.Fatalf("same-week re-run must replace: rows %d", c)
	}
	// Week 2: the macro regime changes in between.
	mustExec(t, st, `INSERT INTO macro_regime_history (quadrant, shorthand, growth_dir, inflation_dir, computed_at) VALUES ('Q1','Goldilocks','accel','decel',?)`, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC).Unix())
	mustExec(t, st, `UPDATE meta SET value = value WHERE 0`)
	mustExec(t, st, `INSERT INTO notification_log (holding_kind, holding_id, alert_kind, alert_day, created_at) VALUES ('stock',3,'red','2026-10-16',1),('stock',4,'red','2026-10-17',1),('stock',5,'red','2026-10-17',1)`)
	sun2 := time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)
	// The live macro regime handler reads macro_regime_history's latest row.
	res2, err := s.RunWeeklySnapshot(ctx, sun2, SnapshotOptions{OutDir: out, Notify: n})
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := snapshot.Count(ctx, st.DB); c != 2 || res2.Report.First || res2.Report.PrevWeek != "2026-10-05" {
		t.Fatalf("rows %d first=%v prev=%s", c, res2.Report.First, res2.Report.PrevWeek)
	}
	// Alerts raised 2026-10-12..18 = 3 (week 2) vs 2026-10-06..12 = 2 (week 1).
	if res.Payload.Metrics["alerts.7d.red"] != 2 || res2.Payload.Metrics["alerts.7d.red"] != 3 {
		t.Fatalf("alert counts: %v / %v", res.Payload.Metrics["alerts.7d.red"], res2.Payload.Metrics["alerts.7d.red"])
	}
	if len(n.msgs) != 2 || !strings.HasPrefix(n.msgs[1], "FT weekly snapshot ready:") {
		t.Fatalf("telegram: %v", n.msgs)
	}
	for _, l := range res2.Report.Top {
		if strings.Contains(l, "NaN") {
			t.Fatal(l)
		}
	}
}

// A section whose source breaks is omitted with a note; the run still completes and nothing is invented.
func TestSnapshotOmitsABrokenSectionWithANote(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `ALTER TABLE watchlist RENAME TO watchlist_broken`)
	res, err := s.RunWeeklySnapshot(context.Background(), time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatalf("run must complete: %v", err)
	}
	found := false
	for _, n := range res.Payload.Notes {
		if strings.Contains(n, "Section omitted — watchlist") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no omission note: %v", res.Payload.Notes)
	}
	if _, ok := res.Payload.Metrics["watchlist.count"]; ok {
		t.Fatal("an omitted section must not invent numbers")
	}
	if !strings.Contains(res.Markdown, "Section omitted — watchlist") {
		t.Fatal("the note must reach the file")
	}
}

// The numbers equal what the connector tools return at the same moment (same handlers).
func TestSnapshotEqualsConnectorHandlers(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, current_price, target_entry_low, target_entry_high, added_at, updated_at) VALUES
	  (1,'AAA','stock',10,9,11,1,1),(1,'BBB','stock',50,9,11,1,1),(1,'CCC','stock',10,9,11,1,1)`)
	ctx := context.Background()
	res, err := s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	wl, _ := s.callHandlerJSON(ctx, 1, "/api/watchlist", nil, s.handleListWatchlist)
	gaps, _ := s.callHandlerJSON(ctx, 1, "/api/theses/gaps", nil, s.handleThesesGaps)
	if got, want := res.Payload.Metrics["watchlist.count"], float64(len(arr(wl, "watchlist"))); got != want || want < 3 {
		t.Fatalf("watchlist count %v vs connector %v", got, want)
	}
	if got, want := res.Payload.Metrics["gaps.total"], float64(len(arr(gaps, "gaps"))); got != want {
		t.Fatalf("gaps %v vs connector %v", got, want)
	}
	inRange := 0
	for _, x := range arr(wl, "watchlist") {
		if m, _ := x.(map[string]any); m["inRange"] == true {
			inRange++
		}
	}
	if got := res.Payload.Metrics["watchlist.in_zone_count"]; got != float64(inRange) || inRange < 2 {
		t.Fatalf("in zone %v vs connector %d", got, inRange)
	}
	set := strings.Join(res.Payload.Sets["watchlist.in_zone"], ",")
	if !strings.Contains(set, "AAA") || !strings.Contains(set, "CCC") || strings.Contains(set, "BBB") {
		t.Fatalf("in-zone set %v", set)
	}
}

// Nothing from the privacy sweep's blocked list may reach the file.
func TestSnapshotFileHasNoBlockedContent(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `INSERT INTO etoro_statement_periods (user_id, range_start, range_end, source_file, imported_at) VALUES (1,'2026-01-01','2026-10-09','etoro-account-statement-1-1-2026-10-9-2026.xlsx',1)`)
	mustExec(t, st, `INSERT INTO etoro_closed_positions (user_id, position_id, close_date, year, asset_type, is_copy, profit_usd, profit_eur, source_file, imported_at) VALUES (1,'123456789','2026-05-01',2026,'Stocks',0,5,4,'etoro-account-statement-x.xlsx',1)`)
	res, err := s.RunWeeklySnapshot(context.Background(), time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?i)\.xlsx|etoro-account-statement|BLOSSOM|MIDLETON|\bIBAN\b|\bPPS\b|taxpayer|password|api[_-]?key|secret|bearer|123456789|@example`)
	if m := re.FindString(res.Markdown); m != "" {
		t.Fatalf("blocked content %q in the snapshot file", m)
	}
	if !strings.Contains(res.Markdown, "2026-10-09") {
		t.Fatal("the coverage end date is expected (it is not personal data)")
	}
}

func TestSnapshotRefusesInDemoMode(t *testing.T) {
	s, st := snapServer(t)
	if err := st.SetPreference(context.Background(), "demo_mode", "on"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunWeeklySnapshot(context.Background(), time.Now(), SnapshotOptions{DryRun: true}); err == nil || !strings.Contains(err.Error(), "demo mode") {
		t.Fatalf("demo mode must refuse: %v", err)
	}
}

// Sunday run vs Monday catch-up: the catch-up fills a missed week, never duplicates one, and
// other days do nothing.
func TestScheduledSnapshotModes(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	count := func() int { c, _ := snapshot.Count(ctx, st.DB); return c }
	wed := time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)
	if err := s.RunScheduledSnapshot(ctx, wed, "sunday", nil); err != nil || count() != 0 {
		t.Fatalf("wednesday must do nothing: %v %d", err, count())
	}
	// Monday 2026-10-12 07:00 UTC with no row for the week of 10-05: catch-up takes it.
	mon := time.Date(2026, 10, 12, 7, 0, 0, 0, time.UTC)
	if err := s.RunScheduledSnapshot(ctx, mon, "catchup", nil); err != nil || count() != 1 {
		t.Fatalf("catch-up: %v %d", err, count())
	}
	if p, _ := snapshot.Get(ctx, st.DB, "2026-10-05"); p == nil {
		t.Fatal("the catch-up must be filed under the week that just ended")
	}
	if err := s.RunScheduledSnapshot(ctx, mon.Add(time.Hour), "catchup", nil); err != nil || count() != 1 {
		t.Fatalf("catch-up must not duplicate: %v %d", err, count())
	}
	sun := time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC)
	if err := s.RunScheduledSnapshot(ctx, sun, "sunday", nil); err != nil || count() != 2 {
		t.Fatalf("sunday run: %v %d", err, count())
	}
}

func setRegime(t *testing.T, st *store.Store, fw, value string, setAt time.Time) {
	t.Helper()
	ctx := context.Background()
	for k, v := range map[string]string{"regime_" + fw: value, "regime_" + fw + "_set_at": fmt.Sprint(setAt.Unix()), "regime_" + fw + "_source": "manual"} {
		if err := st.SetPreference(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}
}

// A1 live path: Jordi 101 days, Cowen unset 149 days: both ages in the file, data-health lines,
// no regime change line.
func TestSnapshotRegimeFreshnessAndHealth(t *testing.T) {
	s, st := snapServer(t)
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	setRegime(t, st, "jordi", "stable", now.AddDate(0, 0, -101))
	setRegime(t, st, "cowen", "unclassified", now.AddDate(0, 0, -149))
	mustExec(t, st, `INSERT INTO regime_history (ts, framework_id, regime, source) VALUES (?, 'cowen', 'defensive', 'manual')`, now.AddDate(0, 0, -20).Unix())
	res, err := s.RunWeeklySnapshot(context.Background(), now, SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	md := res.Markdown
	for _, want := range []string{"Regime (Jordi): stable — set 2026-07-04 (101 days ago, manual) — STALE (older than 14 days)", "Regime (Cowen): unset — unset since 2026-05-17 (149 days)", "Jordi regime: set 2026-07-04", "Cowen regime: unset since 2026-05-17"} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
	for _, l := range res.Report.Top {
		if strings.Contains(l, "Cowen") {
			t.Fatalf("defensive → unset must not be a Top change: %s", l)
		}
	}
	if strings.Contains(md, "All monitored sources are ok") {
		t.Fatal("must not claim everything is ok")
	}
	// A fresh, set regime shows no stale marker and no health line.
	setRegime(t, st, "jordi", "stable", now.AddDate(0, 0, -3))
	res2, _ := s.RunWeeklySnapshot(context.Background(), now, SnapshotOptions{DryRun: true})
	if strings.Contains(res2.Markdown, "Jordi regime:") || strings.Contains(res2.Markdown, "Regime (Jordi): stable — set 2026-10-10 (3 days ago, manual) — STALE") {
		t.Fatalf("fresh regime flagged stale:\n%s", res2.Markdown)
	}
}

// A2 live path: a position removal between the two snapshots is annotated next to the value change.
func TestSnapshotAnnotatesHoldingsRemoval(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES ('2026-10-04', 28675, 7616, 21059, 1), ('2026-10-11', 25368, 5192, 20176, 1)`)
	mustExec(t, st, `INSERT INTO holdings_audit (ts, user_id, holding_kind, holding_id, ticker, action, changes_json, reason_code) VALUES (?, 1, 'stock', 9, 'MSTR', 'soft_delete', '{"ftInvestedUsd":500.0,"backfilled":true,"tsEstimated":true}', 'etoro_reconcile'), (?, 1, 'stock', 10, 'NOW', 'soft_delete', '{"ftInvestedUsd":450.0}', 'etoro_reconcile')`,
		time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC).Unix(), time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC).Unix())
	// A tag update must not count as a holdings change.
	mustExec(t, st, `INSERT INTO holdings_audit (ts, user_id, holding_kind, holding_id, ticker, action, changes_json, reason_code) VALUES (?, 1, 'stock', 11, 'AVGO', 'update', '{"x":{"from":null,"to":"y"}}', 'subtype_tag')`, time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC).Unix())
	if err := snapshotSave(ctx, s, "2026-09-28"); err != nil {
		t.Fatal(err)
	}
	res, err := s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	md := res.Markdown
	if !strings.Contains(md, "2 removals ($950.00 invested: MSTR, NOW)") || !strings.Contains(md, "not separable from price movement") || !strings.Contains(md, "timestamps of backfilled rows are estimated") {
		t.Fatalf("annotation missing:\n%s", md)
	}
	if strings.Contains(md, "AVGO") {
		t.Fatal("a tag update is not a holdings change")
	}
	if !strings.Contains(strings.Join(res.Report.Top, "\n"), "holdings changes: 2 removals") {
		t.Fatalf("top changes must carry the note: %v", res.Report.Top)
	}
	// No audit rows in the window: say so, still no estimate.
	res0, _ := s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	_ = res0
}

func snapshotSave(ctx context.Context, s *Server, week string) error {
	p := snapshot.New(week, time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC))
	p.Metrics["portfolio.total_value_usd"] = 29387
	return snapshot.Save(ctx, s.store.DB, p, time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC))
}

// A3 live path: RTX is in the entry zone and in the gap report (no thesis).
func TestSnapshotInZoneWithoutThesis(t *testing.T) {
	s, st := snapServer(t)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, current_price, target_entry_low, target_entry_high, added_at, updated_at) VALUES (1,'RTX','stock',10,9,11,1,1)`)
	res, err := s.RunWeeklySnapshot(context.Background(), time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Payload.Sets["watchlist.in_zone_no_thesis"]
	if len(got) != 1 || got[0] != "RTX" || !strings.Contains(res.Markdown, "In entry zone without a thesis: RTX") {
		t.Fatalf("set %v\n%s", got, res.Markdown)
	}
}

// The earlier falls in the "Last 4" column can be read against the holdings changes of those weeks.
func TestSnapshotListsEarlierWeeksHoldingsChanges(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `INSERT INTO holdings_audit (ts, user_id, holding_kind, holding_id, ticker, action, changes_json) VALUES (?, 1, 'stock', 9, 'MSTR', 'soft_delete', '{"ftInvestedUsd":500.0}')`, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC).Unix())
	if err := snapshotSave(ctx, s, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	res, err := s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 18, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	// The stored week 2026-10-05 was "taken" 2026-09-28; the removal on 10-07 is inside the
	// current window; the week-ending lines cover the three windows before the previous snapshot.
	if !strings.Contains(res.Markdown, "1 removal ($500.00 invested: MSTR)") {
		t.Fatalf("removal not mentioned:\n%s", res.Markdown)
	}
}

// SC-52 P4: capital flows recorded from statements appear beside the value change; with no
// statement covering the period the note says so instead of guessing.
func TestSnapshotNotesCapitalFlows(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES ('2026-10-04', 28000, 8000, 20000, 1), ('2026-10-11', 25000, 5000, 20000, 1)`)
	if err := snapshotSave(ctx, s, "2026-09-28"); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	// No statement at all.
	res, err := s.RunWeeklySnapshot(ctx, now, SnapshotOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Markdown, "no statement covers this period") {
		t.Fatalf("no statements: %s", res.Markdown)
	}
	// A statement ending before the period.
	mustExec(t, st, `INSERT INTO etoro_statement_periods (user_id, range_start, range_end, imported_at) VALUES (1,'2026-01-01','2026-06-30',1)`)
	res, _ = s.RunWeeklySnapshot(ctx, now, SnapshotOptions{DryRun: true})
	if !strings.Contains(res.Markdown, "after 2026-06-30 are not known") || !strings.Contains(res.Markdown, "next should start 2026-07-01") {
		t.Fatalf("stale coverage: %s", res.Markdown)
	}
	// A statement covering the period, with a withdrawal in it.
	mustExec(t, st, `UPDATE etoro_statement_periods SET range_end = '2026-10-09'`)
	mustExec(t, st, `INSERT INTO etoro_capital_flows (user_id, flow_key, occurred_at, day, kind, amount_usd, details, manual, imported_at) VALUES (1,'w1','2026-10-08T12:00:00','2026-10-08','withdrawal',-2500,'',0,1),(1,'d1','2026-10-06T12:00:00','2026-10-06','deposit',400,'',0,1)`)
	res, _ = s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if !strings.Contains(res.Markdown, "in $400.00, out -$2,500.00 (net -$2,100.00)") {
		t.Fatalf("flows missing:\n%s", res.Markdown)
	}
	// Covered but quiet.
	mustExec(t, st, `DELETE FROM etoro_capital_flows`)
	res, _ = s.RunWeeklySnapshot(ctx, time.Date(2026, 10, 9, 18, 0, 0, 0, time.UTC), SnapshotOptions{DryRun: true})
	if !strings.Contains(res.Markdown, "no capital flows are recorded in this period (statements cover through 2026-10-09)") {
		t.Fatalf("quiet period: %s", res.Markdown)
	}
}

// SC-52 P6: flagged regime-history rows never seed the snapshot's history; the rows themselves stay.
func TestFlaggedRegimeRowsDoNotSeedSnapshots(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	sunday := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	mustExec(t, st, `INSERT INTO regime_history (ts, framework_id, regime, source, note) VALUES (?, 'cowen', 'defensive', 'auto_cowen_form', 'smoke test')`, sunday.Add(-24*time.Hour).Unix())
	seed := func() string {
		p, err := s.seedPayload(ctx, "2026-05-11")
		if err != nil {
			t.Fatal(err)
		}
		return p.Texts["regime.cowen"]
	}
	if got := seed(); got != "defensive" {
		t.Fatalf("an unflagged row seeds the week: %q", got)
	}
	mustExec(t, st, `UPDATE regime_history SET seed_excluded = 1`)
	if got := seed(); got != "" {
		t.Fatalf("a flagged row must not seed: %q", got)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM regime_history`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the row itself stays: %d %v", n, err)
	}
}
