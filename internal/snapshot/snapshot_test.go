package snapshot

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func payload(week string) *Payload { return New(week, time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)) }

// four consecutive weeks, values chosen so every delta is hand-computable
func weeks() (d, c, a, b *Payload) {
	d, c, a, b = payload("2026-09-21"), payload("2026-09-28"), payload("2026-10-05"), payload("2026-10-12")
	d.Metrics["portfolio.total_value_usd"] = 20000
	c.Metrics["portfolio.total_value_usd"] = 21000
	a.Metrics["portfolio.total_value_usd"] = 22000
	b.Metrics["portfolio.total_value_usd"] = 23100
	a.Metrics["thesis.score.RHM.DE"], b.Metrics["thesis.score.RHM.DE"] = 14, 12
	a.Metrics["alerts.7d.red"], b.Metrics["alerts.7d.red"] = 2, 5
	a.Metrics["gaps.total"], b.Metrics["gaps.total"] = 8, 8
	a.Texts["regime.macro_quadrant"], b.Texts["regime.macro_quadrant"] = "Q4", "Q1"
	a.Texts["regime.jordi"], b.Texts["regime.jordi"] = "stable", "stable"
	a.Sets["watchlist.in_zone"], b.Sets["watchlist.in_zone"] = []string{"AAPL"}, []string{"AAPL", "NVDA"}
	a.Sets["gaps.open"], b.Sets["gaps.open"] = []string{"watchlist:DJT", "watchlist:NEM"}, []string{"watchlist:DJT"}
	a.Sets["theses.locked"], b.Sets["theses.locked"] = []string{"RHM.DE"}, []string{"RHM.DE", "TSLA"}
	return
}

func TestCompareDeltasTrendsAndTopChangesAreExact(t *testing.T) {
	d, c, a, b := weeks()
	r := Compare(b, []*Payload{a, c, d})
	if r.First || r.PrevWeek != "2026-10-05" {
		t.Fatalf("first=%v prev=%s", r.First, r.PrevWeek)
	}
	get := func(k string) Series {
		for _, s := range r.Metrics {
			if s.Key == k {
				return s
			}
		}
		t.Fatalf("no series %s", k)
		return Series{}
	}
	tv := get("portfolio.total_value_usd")
	if *tv.Prev != 22000 || *tv.Delta != 1100 || fmt.Sprint(tv.Trend) != "[20000 21000 22000 23100]" {
		t.Fatalf("total value: %+v", tv)
	}
	al := get("alerts.7d.red")
	if *al.Delta != 3 || fmt.Sprint(al.Trend) != "[2 5]" {
		t.Fatalf("alerts: %+v", al)
	}
	if g := get("gaps.total"); *g.Delta != 0 {
		t.Fatalf("gaps: %+v", g)
	}
	want := []string{
		"Macro quadrant: Q4 → Q1",
		"Thesis RHM.DE score: 14 → 12",
		"Gap report: watchlist:NEM gap closed",
		"Thesis: TSLA newly locked",
		"Watchlist: NVDA entered the entry zone",
		"Alerts last 7 days (red): 2 → 5 (+3)",
		"Total value (stocks + crypto): $22,000.00 → $23,100.00 (+$1,100.00)",
	}
	if strings.Join(r.Top, "\n") != strings.Join(want, "\n") {
		t.Fatalf("top changes:\n%s\nwant:\n%s", strings.Join(r.Top, "\n"), strings.Join(want, "\n"))
	}
	if r.ChangeCount != 7 {
		t.Fatalf("change count %d", r.ChangeCount)
	}
	if len(r.TextChanges) != 1 || r.TextChanges[0].From != "Q4" {
		t.Fatalf("text changes: %+v", r.TextChanges)
	}
	// Deterministic.
	r2 := Compare(b, []*Payload{a, c, d})
	if strings.Join(r.Top, "|") != strings.Join(r2.Top, "|") {
		t.Fatal("not deterministic")
	}
}

func TestFirstSnapshotShowsNoDeltasOrNaN(t *testing.T) {
	_, _, _, b := weeks()
	r := Compare(b, nil)
	if !r.First || len(r.Top) != 0 || len(r.TextChanges) != 0 || len(r.SetChanges) != 0 {
		t.Fatalf("%+v", r)
	}
	out := Render(b, r)
	if !strings.Contains(out, "First snapshot") || !strings.Contains(out, "first snapshot |") {
		t.Fatalf("missing first-snapshot wording:\n%s", out)
	}
	for _, bad := range []string{"NaN", "Inf", "+0.00", "$0.00"} {
		if strings.Contains(out, bad) {
			t.Fatalf("unexpected %q in first snapshot:\n%s", bad, out)
		}
	}
}

// A metric absent from history is new, not a change from zero.
func TestNewKeyIsNotAChangeFromZero(t *testing.T) {
	_, _, a, b := weeks()
	b.Metrics["performance.ytd_net_usd"] = 647.05
	r := Compare(b, []*Payload{a})
	for _, s := range r.Metrics {
		if s.Key == "performance.ytd_net_usd" && (s.Prev != nil || s.Delta != nil) {
			t.Fatalf("new key must have no previous value: %+v", s)
		}
	}
	for _, l := range r.Top {
		if strings.Contains(l, "ytd_net") || strings.Contains(l, "YTD net") {
			t.Fatalf("new key leaked into top changes: %s", l)
		}
	}
}

func TestTopChangesCappedAtEight(t *testing.T) {
	a, b := payload("2026-10-05"), payload("2026-10-12")
	var before, after []string
	for i := 0; i < 12; i++ {
		after = append(after, fmt.Sprintf("T%02d", i))
	}
	a.Sets["watchlist.in_zone"], b.Sets["watchlist.in_zone"] = before, after
	r := Compare(b, []*Payload{a})
	if len(r.Top) != TopMax || r.ChangeCount != 12 {
		t.Fatalf("top %d count %d", len(r.Top), r.ChangeCount)
	}
	if !strings.Contains(Render(b, r), "12 changes in total") {
		t.Fatal("render must say how many changes there were")
	}
}

func TestWeekStartIsTheMondayInDublin(t *testing.T) {
	dub, err := time.LoadLocation("Europe/Dublin")
	if err != nil {
		t.Skip("no tzdata")
	}
	for in, want := range map[string]string{
		"2026-10-11T18:00:00Z": "2026-10-05", // Sunday evening: still the week of 10-05
		"2026-10-12T00:30:00Z": "2026-10-12", // Monday
		"2026-10-18T10:00:00Z": "2026-10-12", // Sunday
		"2026-10-18T23:30:00Z": "2026-10-19", // Dublin is UTC+1 until 25 Oct: already Monday 00:30
	} {
		ts, _ := time.Parse(time.RFC3339, in)
		if got := WeekStart(ts, dub); got != want {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestRenderHasHeaderEndMarkerAndStaysShort(t *testing.T) {
	_, _, a, b := weeks()
	for i := 0; i < 53; i++ { // every thesis: only changes are listed
		b.Metrics[fmt.Sprintf("thesis.score.T%02d", i)] = 12
		a.Metrics[fmt.Sprintf("thesis.score.T%02d", i)] = 12
	}
	for i := 0; i < 11; i++ {
		b.Metrics[fmt.Sprintf("crypto_ind.ind_%d", i)] = float64(i)
		b.Texts[fmt.Sprintf("crypto_ind.ind_%d.as_of", i)] = "2026-10-12"
	}
	b.Lines["data_health"] = []string{"Farside: 3 days old"}
	b.Notes = []string{"Conviction bands are not stored in FT."}
	out := Render(b, Compare(b, []*Payload{a}))
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(out, "ft_snapshot_version: 2\ngenerated_at: ") || !strings.Contains(out, "\nweek_start: 2026-10-12\n") {
		t.Fatalf("header:\n%s", out[:200])
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "<!-- END ft-snapshot v2 -->") {
		t.Fatal("end marker missing")
	}
	if len(lines) > 250 {
		t.Fatalf("%d lines", len(lines))
	}
	if strings.Index(out, "## Top changes") > strings.Index(out, "## Portfolio") {
		t.Fatal("Top changes must come first")
	}
	if strings.Contains(out, "thesis.score.T05") || strings.Contains(out, "T05 score") {
		t.Fatal("unchanged per-thesis scores must not be listed")
	}
	if !strings.Contains(out, "(as of 2026-10-12)") || !strings.Contains(out, "Conviction bands are not stored") {
		t.Fatal("as-of dates and notes must render")
	}
}

func TestSaveReplacesTheSameWeekAndKeepsOthers(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE ft_snapshots (id INTEGER PRIMARY KEY, week_start TEXT NOT NULL UNIQUE, generated_at INTEGER NOT NULL, schema_version INTEGER NOT NULL, payload_json TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	a, b := payload("2026-10-05"), payload("2026-10-12")
	b.Metrics["x"] = 1
	for _, p := range []*Payload{a, b} {
		if err := Save(ctx, db, p, now); err != nil {
			t.Fatal(err)
		}
	}
	b.Metrics["x"] = 2
	if err := Save(ctx, db, b, now); err != nil {
		t.Fatal(err)
	}
	if n, _ := Count(ctx, db); n != 2 {
		t.Fatalf("count %d", n)
	}
	got, _ := Get(ctx, db, "2026-10-12")
	if got.Metrics["x"] != 2 {
		t.Fatalf("replace failed: %+v", got.Metrics)
	}
	prev, _ := Before(ctx, db, "2026-10-12", 3)
	if len(prev) != 1 || prev[0].WeekStart != "2026-10-05" {
		t.Fatalf("before: %+v", prev)
	}
}

// A1: an unset or unclassified state is never a regime change; a real change still ranks first.
func TestUnsetAndUnclassifiedAreNotRegimeChanges(t *testing.T) {
	for _, c := range []struct{ prev, cur string }{
		{"defensive", "unset"}, {"defensive", "unclassified"}, {"unset", "stable"}, {"unclassified", "defensive"}, {"unset", "unset"},
	} {
		a, b := payload("2026-10-05"), payload("2026-10-12")
		a.Texts["regime.cowen"], b.Texts["regime.cowen"] = c.prev, c.cur
		r := Compare(b, []*Payload{a})
		if len(r.TextChanges) != 0 || len(r.Top) != 0 {
			t.Fatalf("%s → %s must not be a change: %+v %v", c.prev, c.cur, r.TextChanges, r.Top)
		}
	}
	a, b := payload("2026-10-05"), payload("2026-10-12")
	a.Texts["regime.jordi"], b.Texts["regime.jordi"] = "stable", "defensive"
	a.Metrics["portfolio.total_value_usd"], b.Metrics["portfolio.total_value_usd"] = 100, 150
	r := Compare(b, []*Payload{a})
	if len(r.Top) != 2 || r.Top[0] != "Regime (Jordi): stable → defensive" {
		t.Fatalf("a real change must rank first: %v", r.Top)
	}
}

// A1: the file shows the regime with its age/stale detail, and an unset regime reads "unset since".
func TestRegimeDetailIsRendered(t *testing.T) {
	b := payload("2026-10-12")
	b.Texts["regime.jordi"] = "stable"
	b.Texts["regime.jordi.detail"] = "set 2026-07-03 (101 days ago, manual) — STALE (older than 14 days)"
	b.Texts["regime.cowen"] = "unset"
	b.Texts["regime.cowen.detail"] = "unset since 2026-05-16 (149 days)"
	b.Lines["data_health"] = []string{"Monitored external sources: all ok.", "Jordi regime: set 2026-07-03 (101 days ago, manual) — STALE (older than 14 days)"}
	out := Render(b, Compare(b, nil))
	for _, want := range []string{"Regime (Jordi): stable — set 2026-07-03 (101 days ago, manual) — STALE", "Regime (Cowen): unset — unset since 2026-05-16 (149 days)", "Jordi regime: set 2026-07-03"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "All monitored sources are ok") {
		t.Fatal("the health section must not claim everything is ok")
	}
}

// A2: value changes carry the holdings-change annotation; the first snapshot is unchanged.
func TestValueNotesAppearNextToTheChange(t *testing.T) {
	a, b := payload("2026-10-05"), payload("2026-10-12")
	a.Metrics["portfolio.total_value_usd"], b.Metrics["portfolio.total_value_usd"] = 28675, 25368
	notes := map[string]ValueNote{"portfolio.total_value_usd": {Short: "holdings changes: 6 removals, 0 additions, 0 adjustments; capital flows not recorded", Long: "holdings changes recorded since the last snapshot: 6 removals ($2,000.00 invested: MSTR, NOW); deposits and withdrawals are not recorded in FT, so they are not separable from price movement."}}
	r := CompareWith(b, []*Payload{a}, notes)
	if len(r.Top) != 1 || !strings.Contains(r.Top[0], "— holdings changes: 6 removals") {
		t.Fatalf("top: %v", r.Top)
	}
	out := Render(b, r)
	if !strings.Contains(out, "Of the change in total value (stocks + crypto) (-$3,307.00): holdings changes recorded since the last snapshot: 6 removals") {
		t.Fatalf("long note missing:\n%s", out)
	}
	// Unchanged value: no note. First snapshot: no note, no change.
	b.Metrics["portfolio.total_value_usd"] = 28675
	if out := Render(b, CompareWith(b, []*Payload{a}, notes)); strings.Contains(out, "Of the change in") {
		t.Fatal("no note when the value did not move")
	}
	if out := Render(b, Compare(b, nil)); strings.Contains(out, "Of the change in") || !strings.Contains(out, "First snapshot") {
		t.Fatal("first snapshot must be unchanged")
	}
}

// A3: entering "in the entry zone without a thesis" is a Top change; leaving it is not.
func TestInZoneWithoutThesisTopChange(t *testing.T) {
	a, b := payload("2026-10-05"), payload("2026-10-12")
	a.Sets["watchlist.in_zone_no_thesis"], b.Sets["watchlist.in_zone_no_thesis"] = nil, []string{"RTX"}
	r := Compare(b, []*Payload{a})
	if len(r.Top) != 1 || r.Top[0] != "Watchlist: RTX is in the entry zone without a thesis" {
		t.Fatalf("top: %v", r.Top)
	}
	r = Compare(a, []*Payload{b})
	if len(r.Top) != 0 || r.ChangeCount != 1 {
		t.Fatalf("leaving the state is visible but not a Top change: %v / %d", r.Top, r.ChangeCount)
	}
	if out := Render(b, Compare(b, nil)); !strings.Contains(out, "In entry zone without a thesis: RTX") {
		t.Fatalf("the section must show RTX:\n%s", out)
	}
}
