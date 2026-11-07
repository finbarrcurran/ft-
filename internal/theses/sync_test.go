package theses

// SC-55 — thesis sync safeguards. Every test runs against a scratch SQLite DB and a scratch git repo
// (a local bare "origin" plus a working clone that plays the part of GitHub); nothing touches the live
// DB or cross_sector_research.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ft/internal/store"
)

// fakeNotifier records alert texts instead of sending Telegram messages.
type fakeNotifier struct{ msgs *[]string }

func (f fakeNotifier) Send(_ context.Context, text string) error {
	*f.msgs = append(*f.msgs, text)
	return nil
}

type testEnv struct {
	alerts []string
	t      *testing.T
	eng    *Engine
	db     *sql.DB
	origin string // bare repo
	work   string // clone used to author commits and push to origin
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "init.defaultBranch=main"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	clone := filepath.Join(root, "clone")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, origin, "init", "--bare", "-b", "main")
	git(t, root, "clone", origin, work)
	git(t, work, "checkout", "-B", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "-A")
	git(t, work, "commit", "-m", "init")
	git(t, work, "push", "-u", "origin", "main")
	git(t, root, "clone", origin, clone)

	st, err := store.Open(filepath.Join(root, "ft.db"))
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
	env := &testEnv{t: t, db: st.DB, origin: origin, work: work,
		eng: New(st.DB, clone, "owner", "repo", "token")}
	env.eng.Alerts.Notify = fakeNotifier{msgs: &env.alerts}
	return env
}

// thesisMD builds a minimal valid locked thesis.
func thesisMD(ticker string, version, score int) string {
	return fmt.Sprintf("# %s — %s Inc — Locked Thesis v%d\n\n> **Ticker:** %s (NYSE)\n> **Adapter:** Pharma (`test-sub` sub-type)\n> **Status:** Locked — 2026-05-18\n> **Final Score:** %d / 16 — test\n> **Personal use only. Not investment advice.**\n\n---\n\nbody for %s v%d\n",
		ticker, ticker, version, ticker, score, ticker, version)
}

func (e *testEnv) write(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.work, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) remove(rel string) {
	e.t.Helper()
	if err := os.RemoveAll(filepath.Join(e.work, rel)); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) push(msg string) {
	e.t.Helper()
	git(e.t, e.work, "add", "-A")
	// allow empty so a no-op step still produces a commit
	git(e.t, e.work, "commit", "--allow-empty", "-m", msg)
	git(e.t, e.work, "push", "origin", "main")
}

func (e *testEnv) sync() error {
	e.t.Helper()
	return e.eng.Sync(context.Background())
}

type row struct {
	status, reason string
	archivedAt     sql.NullInt64
	path           string
	hasBody        bool
}

func (e *testEnv) row(ticker string, version int) (row, bool) {
	e.t.Helper()
	var r row
	var reason sql.NullString
	var body string
	err := e.db.QueryRow(`SELECT status, archive_reason, archived_at, github_path, markdown_content FROM theses_index WHERE ticker=? AND version=?`, ticker, version).
		Scan(&r.status, &reason, &r.archivedAt, &r.path, &body)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		e.t.Fatal(err)
	}
	r.reason = reason.String
	r.hasBody = body != ""
	return r, true
}

func (e *testEnv) count() int {
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM theses_index`).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *testEnv) want(ticker string, version int, status string) row {
	e.t.Helper()
	r, ok := e.row(ticker, version)
	if !ok {
		e.t.Fatalf("%s v%d: no row", ticker, version)
	}
	if r.status != status {
		e.t.Fatalf("%s v%d: status %q, want %q (reason %q)", ticker, version, r.status, status, r.reason)
	}
	return r
}

func seed(e *testEnv) {
	e.write("theses/pharma/AAA_v1_locked.md", thesisMD("AAA", 1, 12))
	e.write("theses/pharma/BBB_v1_locked.md", thesisMD("BBB", 1, 11))
	e.write("theses/pharma/CCC_v1_locked.md", thesisMD("CCC", 1, 10))
	e.push("seed")
	if err := e.sync(); err != nil {
		e.t.Fatal(err)
	}
}

func TestSync_IngestAndIdempotent(t *testing.T) {
	e := newEnv(t)
	seed(e)
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.want(tk, 1, "locked")
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if e.count() != 3 {
		t.Fatalf("rows %d, want 3", e.count())
	}
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.want(tk, 1, "locked")
	}
}

func TestSync_FileDeletedIsArchivedNotDeleted_AndRestored(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.remove("theses/pharma/BBB_v1_locked.md")
	e.push("delete BBB")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	r := e.want("BBB", 1, "archived")
	if r.reason != ArchiveFileMissing || !r.archivedAt.Valid || !r.hasBody {
		t.Fatalf("archived row: reason %q archivedAt %v body %v", r.reason, r.archivedAt, r.hasBody)
	}
	if e.count() != 3 {
		t.Fatalf("rows %d, want 3: nothing may be deleted", e.count())
	}
	// A second sync changes nothing (idempotent), archive timestamp stays.
	first := r.archivedAt.Int64
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if r2 := e.want("BBB", 1, "archived"); r2.archivedAt.Int64 != first {
		t.Fatalf("archived_at moved on a repeat sync")
	}
	// File restored with identical content -> un-archived even though the SHA is unchanged.
	e.write("theses/pharma/BBB_v1_locked.md", thesisMD("BBB", 1, 11))
	e.push("restore BBB")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	r = e.want("BBB", 1, "locked")
	if r.reason != "" || r.archivedAt.Valid {
		t.Fatalf("restored row still carries archive fields: %+v", r)
	}
}

func TestSync_RenamedFileWithSameHeaderKeepsRow(t *testing.T) {
	e := newEnv(t)
	seed(e)
	if err := os.MkdirAll(filepath.Join(e.work, "theses/other"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, e.work, "mv", "theses/pharma/CCC_v1_locked.md", "theses/other/CCC_v1_locked.md")
	e.push("move CCC")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	r := e.want("CCC", 1, "locked")
	if r.path != "theses/other/CCC_v1_locked.md" {
		t.Fatalf("github_path %q not updated", r.path)
	}
}

func TestSync_RenamedOutOfPatternIsArchivedFileMissing(t *testing.T) {
	e := newEnv(t)
	seed(e)
	git(t, e.work, "mv", "theses/pharma/CCC_v1_locked.md", "theses/pharma/CCC_v1_locked.md.bak")
	e.push("rename CCC away from the pattern")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if r := e.want("CCC", 1, "archived"); r.reason != ArchiveFileMissing {
		t.Fatalf("reason %q", r.reason)
	}
}

func TestSync_BrokenHeaderArchivesThenFixRestores(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/pharma/AAA_v1_locked.md", "no header here at all\n")
	e.push("break AAA")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	r := e.want("AAA", 1, "archived")
	if r.reason != ArchiveHeaderInvalid || !r.hasBody {
		t.Fatalf("reason %q body %v", r.reason, r.hasBody)
	}
	e.write("theses/pharma/AAA_v1_locked.md", thesisMD("AAA", 1, 12))
	e.push("fix AAA")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("AAA", 1, "locked")
}

func TestSync_DirectoryMissingAbortsWithoutChanges(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.remove("theses")
	e.push("remove theses/")
	err := e.sync()
	if err != ErrThesesDirMissing {
		t.Fatalf("err = %v, want ErrThesesDirMissing", err)
	}
	if e.count() != 3 {
		t.Fatalf("rows %d, want 3", e.count())
	}
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.want(tk, 1, "locked")
	}
}

func TestSync_RemovedV2ReturnsV1ToLive(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/pharma/AAA_v2_locked.md", thesisMD("AAA", 2, 13))
	e.push("add AAA v2")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("AAA", 1, "superseded")
	e.want("AAA", 2, "locked")
	e.remove("theses/pharma/AAA_v2_locked.md")
	e.push("remove AAA v2")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("AAA", 2, "archived")
	e.want("AAA", 1, "locked") // the old one-way supersede left this superseded forever
	// ...and v2 coming back flips them again.
	e.write("theses/pharma/AAA_v2_locked.md", thesisMD("AAA", 2, 13))
	e.push("v2 back")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("AAA", 2, "locked")
	e.want("AAA", 1, "superseded")
}

func TestSync_DuplicateKeyDoesNotOverwrite(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/zzz/AAA_v1_locked.md", thesisMD("AAA", 1, 5)) // same ticker+version, other folder, lower score
	e.push("duplicate AAA v1")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	r := e.want("AAA", 1, "locked")
	if r.path != "theses/pharma/AAA_v1_locked.md" {
		t.Fatalf("duplicate took over the row: path %q", r.path)
	}
}

func TestNoDeleteTrigger(t *testing.T) {
	e := newEnv(t)
	seed(e)
	if _, err := e.db.Exec(`DELETE FROM theses_index WHERE ticker='AAA'`); err == nil {
		t.Fatal("DELETE FROM theses_index succeeded; the guard trigger is missing")
	} else if !strings.Contains(err.Error(), "never deleted") {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.count() != 3 {
		t.Fatalf("rows %d, want 3", e.count())
	}
	// Documented admin path.
	if _, err := e.db.Exec(`INSERT INTO thesis_delete_unlock (reason, created_at) VALUES ('test', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`DELETE FROM theses_index WHERE ticker='AAA'`); err != nil {
		t.Fatalf("admin delete failed: %v", err)
	}
	if e.count() != 2 {
		t.Fatalf("rows %d, want 2", e.count())
	}
}

func TestNoDeleteStatementsInCode(t *testing.T) {
	// P1 acceptance: no `DELETE FROM theses_index` remains in the Go code of this package.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		if !strings.HasSuffix(en.Name(), ".go") || strings.HasSuffix(en.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(en.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToUpper(string(b)), "DELETE FROM THESES_INDEX") {
			t.Fatalf("%s still contains DELETE FROM theses_index", en.Name())
		}
	}
}

func TestGaps_ArchivedAndNoLiveThesis(t *testing.T) {
	e := newEnv(t)
	seed(e)
	exec := func(q string, a ...any) {
		if _, err := e.db.Exec(q, a...); err != nil {
			t.Fatal(err)
		}
	}
	for _, tk := range []string{"AAA", "BBB", "ZZZ"} {
		exec(`INSERT INTO watchlist (user_id, ticker, kind, added_at, updated_at) VALUES (1, ?, 'stock', 0, 0)`, tk)
	}
	// BBB: archived (file gone). AAA: covered. ZZZ: no row at all.
	e.remove("theses/pharma/BBB_v1_locked.md")
	e.push("delete BBB")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	gaps, err := e.eng.Gaps(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, g := range gaps {
		got[g.Ticker] = g.Reason
	}
	if r, ok := got["BBB"]; !ok || r != GapArchived {
		t.Fatalf("BBB: reason %q present=%v, want %q (gaps %v)", r, ok, GapArchived, got)
	}
	if r, ok := got["ZZZ"]; !ok || r != "" {
		t.Fatalf("ZZZ: reason %q present=%v, want present with empty reason", r, ok)
	}
	if _, ok := got["AAA"]; ok {
		t.Fatalf("AAA is covered but listed as a gap")
	}
	// A ticker whose only rows are superseded (no live one) -> no_live_thesis.
	exec(`UPDATE theses_index SET status='superseded' WHERE ticker='AAA'`)
	gaps, _ = e.eng.Gaps(context.Background())
	for _, g := range gaps {
		if g.Ticker == "AAA" && g.Reason != GapNoLiveThesis {
			t.Fatalf("AAA reason %q, want %q", g.Reason, GapNoLiveThesis)
		}
	}
}

func TestList_HidesArchivedAndSuperseded(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.write("theses/pharma/AAA_v2_locked.md", thesisMD("AAA", 2, 13))
	e.remove("theses/pharma/BBB_v1_locked.md")
	e.push("v2 + delete BBB")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	rows, err := e.eng.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s_v%d", r.Ticker, r.Version))
	}
	if len(got) != 2 {
		t.Fatalf("List = %v, want AAA_v2 and CCC_v1 only", got)
	}
}

func TestSync_LiveRowStatusIsNotRewrittenFromHeader(t *testing.T) {
	// The live DB has a row (373220.KS) whose status is 'locked' while its header says Draft. A sync must
	// leave a live row's status alone; only a superseded row returns to the header's declared status.
	e := newEnv(t)
	seed(e)
	if _, err := e.db.Exec(`UPDATE theses_index SET status = 'draft' WHERE ticker = 'CCC'`); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	e.want("CCC", 1, "draft")
	e.want("AAA", 1, "locked")
}
