package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/marketdata"
)

func writeFarside(t *testing.T, fetchedAt string, dates ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "etf-flow.json")
	rows := ""
	for i, d := range dates {
		if i > 0 {
			rows += ","
		}
		rows += `{"date":"` + d + `","totalM":1}`
	}
	if err := os.WriteFile(p, []byte(`{"fetchedAt":"`+fetchedAt+`","rolling7dM":1,"rows":[`+rows+`]}`), 0o644); err != nil { // mtime = now
		t.Fatal(err)
	}
	return p
}

func TestETFFlowEvalRules(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC) // Wednesday

	// Monday morning style: Friday data viewed on Monday = age 1, no alert.
	mon := time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC)
	p := writeFarside(t, "2026-10-12T00:25:00Z", "2026-10-09")
	if down, _, _, _, err := etfFlowEval(p)(ctx, mon); err != nil || down {
		t.Fatalf("Friday data on Monday must be healthy: down=%v err=%v", down, err)
	}

	// 3 trading days old, scraper fine -> alert, cause = SOURCE.
	p = writeFarside(t, "2026-10-14T00:25:00Z", "2026-10-09")
	down, _, found, detail, err := etfFlowEval(p)(ctx, now)
	if err != nil || !down || !found || !strings.Contains(detail, "SOURCE not updated") || !strings.Contains(detail, "3 US trading days") {
		t.Fatalf("source-stale case wrong: down=%v found=%v %q err=%v", down, found, detail, err)
	}

	// 3 trading days old and the scraper has not succeeded for days -> cause = SCRAPER.
	p = writeFarside(t, "2026-10-09T00:25:00Z", "2026-10-09")
	down, _, _, detail, _ = etfFlowEval(p)(ctx, now)
	if !down || !strings.Contains(detail, "SCRAPER failed") {
		t.Fatalf("scraper-failed case wrong: %v %q", down, detail)
	}

	// mtime is fresh (the file was just written) but rows are old -> still stale.
	p = writeFarside(t, "2026-10-14T00:25:00Z", "2026-10-02") // 8 trading days old
	if down, _, _, _, _ := etfFlowEval(p)(ctx, now); !down {
		t.Fatal("a fresh file with old rows must be treated as stale")
	}

	// missing file -> alert with a clear cause.
	down, _, found, detail, err = etfFlowEval(filepath.Join(t.TempDir(), "nope.json"))(ctx, now)
	if err != nil || !down || found || !strings.Contains(detail, "not found") {
		t.Fatalf("missing file: %v %v %q %v", down, found, detail, err)
	}
}

// End to end through the monitor: one alert at age >= 3, no repeat, then a recovery note.
func TestETFFlowAlertsOnceAndRecovers(t *testing.T) {
	if err := marketdata.LoadHolidays(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC) // Tuesday: Friday data = 2 trading days
	dir := t.TempDir()
	path := filepath.Join(dir, "etf-flow.json")
	write := func(fetched string, dates ...string) {
		rows := ""
		for i, d := range dates {
			if i > 0 {
				rows += ","
			}
			rows += `{"date":"` + d + `","totalM":1}`
		}
		_ = os.WriteFile(path, []byte(`{"fetchedAt":"`+fetched+`","rolling7dM":1,"rows":[`+rows+`]}`), 0o644)
	}
	write("2026-10-13T00:25:00Z", "2026-10-09")
	n := &fakeNotifier{}
	m := newMon(t, new(time.Time), &now, n)
	m.Checks = []Check{{Source: "farside", Label: "Farside ETF-flow data", MaxAge: 48 * time.Hour, Eval: etfFlowEval(path)}}
	m.Tick(ctx) // age 2: healthy
	if len(n.msgs) != 0 {
		t.Fatalf("age 2 must not alert: %v", n.msgs)
	}
	now = now.Add(24 * time.Hour) // Wednesday: age 3; the scraper ran fine overnight, Farside simply has no newer row
	write("2026-10-14T00:25:00Z", "2026-10-09")
	m.Tick(ctx)
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "SOURCE not updated") {
		t.Fatalf("expected one alert naming the cause, got %v", n.msgs)
	}
	now = now.Add(time.Hour)
	m.Tick(ctx)
	if len(n.msgs) != 1 {
		t.Fatalf("no repeat within 24 h: %v", n.msgs)
	}
	write("2026-10-14T00:25:00Z", "2026-10-13") // new data arrives
	m.Tick(ctx)
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "is back") {
		t.Fatalf("expected a recovery note, got %v", n.msgs)
	}
}
