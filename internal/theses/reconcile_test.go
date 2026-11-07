package theses

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func (e *testEnv) lastRun() (status string, mismatches int, detail string) {
	e.t.Helper()
	if err := e.db.QueryRow(`SELECT status, mismatches, detail FROM thesis_reconcile_runs ORDER BY id DESC LIMIT 1`).Scan(&status, &mismatches, &detail); err != nil {
		e.t.Fatalf("no reconcile run: %v", err)
	}
	return
}

func (e *testEnv) takeAlerts() []string {
	a := e.alerts
	e.alerts = nil
	return a
}

func TestReconcile_CleanRunIsSilent(t *testing.T) {
	e := newEnv(t)
	seed(e)
	if st, n, d := e.lastRun(); st != "ok" || n != 0 {
		t.Fatalf("clean run: status %s mismatches %d detail %s", st, n, d)
	}
	for i := 0; i < 3; i++ {
		if err := e.sync(); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.alerts) != 0 {
		t.Fatalf("clean runs alerted: %v", e.alerts)
	}
}

func TestReconcile_MalformedHeaderAlertsOnceThenRecovers(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/pharma/AAA_v1_locked.md", "garbage, no header\n")
	e.push("break AAA")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	// One fault, one message: the failed-ingest check reports it; the reconciliation does not repeat it.
	a := e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "FT thesis library: Thesis ingest failures looks down") || !strings.Contains(a[0], "AAA_v1_locked.md") {
		t.Fatalf("alerts after the break: %q", a)
	}
	if st, n, d := e.lastRun(); st != "ok" || n != 0 {
		t.Fatalf("reconcile should not repeat the header fault: %s %d %s", st, n, d)
	}
	e.want("AAA", 1, "archived")
	// Same fault on the next syncs: deduped, no second message.
	for i := 0; i < 2; i++ {
		if err := e.sync(); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.alerts) != 0 {
		t.Fatalf("repeat alerts: %q", e.alerts)
	}
	// Fixed: one recovery message, then silence.
	e.write("theses/pharma/AAA_v1_locked.md", thesisMD("AAA", 1, 12))
	e.push("fix AAA")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	a = e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "is back") {
		t.Fatalf("recovery alerts: %q", a)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if len(e.alerts) != 0 {
		t.Fatalf("alerts after recovery: %q", e.alerts)
	}
}

func TestReconcile_DeletedFileAlerts(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.remove("theses/pharma/BBB_v1_locked.md")
	e.push("delete BBB")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	a := e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "BBB") || !strings.Contains(a[0], "file_missing") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestReconcile_ExtraRowWithNoFile_Nightly(t *testing.T) {
	e := newEnv(t)
	seed(e)
	if _, err := e.db.Exec(`INSERT INTO theses_index (ticker, adapter, version, status, github_path, github_url, markdown_content, rendered_html, file_sha, created_at, updated_at)
		VALUES ('GHOST', 'pharma', 1, 'locked', 'theses/pharma/GHOST_v1_locked.md', 'u', 'x', 'x', 'abc', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.ReconcileNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _, d := e.lastRun()
	if st != "mismatch" || !strings.Contains(d, `"type":"row_without_file"`) || !strings.Contains(d, "GHOST") {
		t.Fatalf("run: %s %s", st, d)
	}
	a := e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "GHOST") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestReconcile_StaleCloneNightly(t *testing.T) {
	e := newEnv(t)
	seed(e)
	// origin moves on, the clone does not (no sync): the nightly run must notice.
	e.write("theses/pharma/DDD_v1_locked.md", thesisMD("DDD", 1, 9))
	e.push("add DDD upstream")
	if err := e.eng.ReconcileNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _, d := e.lastRun()
	if st != "mismatch" || !strings.Contains(d, `"type":"clone_stale"`) {
		t.Fatalf("run: %s %s", st, d)
	}
	if a := e.takeAlerts(); len(a) != 1 || !strings.Contains(a[0], "clone stale") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestReconcile_FileNotIndexedWhenUpsertFails(t *testing.T) {
	e := newEnv(t)
	seed(e)
	// Simulate a failed ingest of a valid new file: scan sees it, the row never appears.
	e.write("theses/pharma/EEE_v1_locked.md", thesisMD("EEE", 1, 8))
	e.push("add EEE")
	if _, err := e.db.Exec(`CREATE TRIGGER block_eee BEFORE INSERT ON theses_index WHEN NEW.ticker = 'EEE' BEGIN SELECT RAISE(ABORT, 'blocked for test'); END`); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	st, _, d := e.lastRun()
	if st != "mismatch" || !strings.Contains(d, `"type":"file_not_indexed"`) || !strings.Contains(d, "EEE") {
		t.Fatalf("run: %s %s", st, d)
	}
	// Two real faults, two messages: the file is not indexed, and the ingest failed with a database error.
	a := e.takeAlerts()
	if len(a) != 2 || !strings.Contains(strings.Join(a, "\n"), "Thesis library vs repo") || !strings.Contains(strings.Join(a, "\n"), "Thesis ingest failures") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestReconcile_SafetyStopArchivesNothing(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/pharma/DDD_v1_locked.md", thesisMD("DDD", 1, 9))
	e.write("theses/pharma/EEE_v1_locked.md", thesisMD("EEE", 1, 8))
	e.push("add DDD and EEE")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.takeAlerts()
	// three files vanish at once (more than 2 rows): the pass must change nothing
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.remove("theses/pharma/" + tk + "_v1_locked.md")
	}
	e.push("remove three")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.want(tk, 1, "locked") // NOT archived
	}
	st, _, d := e.lastRun()
	if st != "aborted" || strings.Count(d, `"type":"safety_stop"`) != 3 {
		t.Fatalf("run: %s %s", st, d)
	}
	a := e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "SYNC ABORTED") {
		t.Fatalf("alerts: %q", a)
	}
	// two vanishing files are within the limit and are archived as usual
	e2 := newEnv(t)
	seed(e2)
	e2.write("theses/pharma/DDD_v1_locked.md", thesisMD("DDD", 1, 9))
	e2.push("add DDD")
	if err := e2.sync(); err != nil {
		t.Fatal(err)
	}
	e2.remove("theses/pharma/AAA_v1_locked.md")
	e2.remove("theses/pharma/BBB_v1_locked.md")
	e2.push("remove two")
	if err := e2.sync(); err != nil {
		t.Fatal(err)
	}
	e2.want("AAA", 1, "archived")
	e2.want("BBB", 1, "archived")
}

func TestReconcile_DirectoryMissingAlertsAbort(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.remove("theses")
	e.push("remove theses/")
	if err := e.sync(); err != ErrThesesDirMissing {
		t.Fatalf("err = %v", err)
	}
	st, _, d := e.lastRun()
	if st != "aborted" || !strings.Contains(d, `"type":"dir_missing"`) {
		t.Fatalf("run: %s %s", st, d)
	}
	if a := e.takeAlerts(); len(a) != 1 || !strings.Contains(a[0], "SYNC ABORTED") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestReconcile_SafetyStopFractionRule(t *testing.T) {
	old := maxArchivePerRun
	maxArchivePerRun = 100 // isolate the percentage rule
	defer func() { maxArchivePerRun = old }()
	e := newEnv(t)
	for i := 0; i < 25; i++ {
		e.write(fmt.Sprintf("theses/pharma/T%02d_v1_locked.md", i), thesisMD(fmt.Sprintf("T%02d", i), 1, 10))
	}
	e.push("25 theses")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.remove("theses/pharma/T00_v1_locked.md") // 1 of 25 = 4%: allowed
	e.push("remove one")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("T00", 1, "archived")
	e.remove("theses/pharma/T01_v1_locked.md") // 1 more is 4% again: allowed
	e.remove("theses/pharma/T02_v1_locked.md") // together 2 of 25 = 8%: stopped
	e.push("remove two more")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("T01", 1, "locked")
	e.want("T02", 1, "locked")
	if st, _, _ := e.lastRun(); st != "aborted" {
		t.Fatalf("status %s, want aborted", st)
	}
}

func TestReconcile_ShaAndPathMismatch(t *testing.T) {
	e := newEnv(t)
	seed(e)
	if _, err := e.db.Exec(`UPDATE theses_index SET file_sha = 'deadbeef' WHERE ticker = 'AAA'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE theses_index SET github_path = 'theses/old/BBB_v1_locked.md' WHERE ticker = 'BBB'`); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.ReconcileNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, d := e.lastRun()
	if !strings.Contains(d, `"type":"sha_mismatch"`) || !strings.Contains(d, `"type":"path_mismatch"`) {
		t.Fatalf("detail: %s", d)
	}
	if a := e.takeAlerts(); len(a) != 1 || !strings.Contains(a[0], "AAA") || !strings.Contains(a[0], "BBB") {
		t.Fatalf("alerts: %q", a)
	}
}

func TestGaps_FileNotIndexedFlag(t *testing.T) {
	e := newEnv(t)
	seed(e)
	// A valid file is in the repo but its row is gone (admin delete): the nightly run flags it.
	if _, err := e.db.Exec(`INSERT INTO thesis_delete_unlock (reason, created_at) VALUES ('test', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM theses_index WHERE ticker = 'CCC'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM thesis_delete_unlock`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO watchlist (user_id, ticker, kind, company_name, added_at, updated_at) VALUES (1, 'BBB', 'stock', 'Bee Inc', 0, 0)`); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.ReconcileNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	gaps, err := e.eng.Gaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]GapRow{}
	for _, g := range gaps {
		got[g.Ticker] = g
	}
	if g, ok := got["CCC"]; !ok || g.Reason != GapFileNotIndexed || g.Kind != "other" {
		t.Fatalf("CCC gap = %+v (present %v), want reason file_not_indexed kind other", g, ok)
	}
	for _, covered := range []string{"AAA", "BBB"} {
		if _, ok := got[covered]; ok {
			t.Fatalf("%s is covered but listed: %+v", covered, got[covered])
		}
	}
	// A held or watched ticker keeps its own kind.
	if _, err := e.db.Exec(`INSERT INTO thesis_delete_unlock (reason, created_at) VALUES ('test', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM theses_index WHERE ticker = 'BBB'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM thesis_delete_unlock`); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.ReconcileNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	gaps, _ = e.eng.Gaps(context.Background())
	found := false
	for _, g := range gaps {
		if g.Ticker == "BBB" {
			found = true
			// BBB is on the watchlist and has no row at all now: the plain gap (no reason) takes precedence
			// over the file flag, and keeps kind=watchlist.
			if g.Kind != "watchlist" {
				t.Fatalf("BBB kind %q", g.Kind)
			}
		}
	}
	if !found {
		t.Fatal("BBB missing from gaps")
	}
	// Once the sync re-indexes the files the flag disappears.
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	gaps, _ = e.eng.Gaps(context.Background())
	for _, g := range gaps {
		if g.Ticker == "CCC" || g.Ticker == "BBB" {
			t.Fatalf("%s still a gap after re-index: %+v", g.Ticker, g)
		}
	}
}
