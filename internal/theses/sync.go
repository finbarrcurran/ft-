package theses

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"ft/internal/monitor"
	"ft/internal/scorecards"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Engine owns the local clone of cross_sector_research and the
// theses_index cache. One instance lives on the Server; the periodic cron
// invokes (*Engine).Sync.
type Engine struct {
	DB          *sql.DB
	RepoDir     string // absolute path to local clone, e.g. /var/lib/ft/research
	RepoOwner   string // "finbarrcurran"
	RepoName    string // "cross_sector_research"
	GitHubToken string // PAT used for git pull/push (HTTPS basic-auth with x-access-token user)

	// Alerts evaluates the Thesis Library checks (reconciliation, SC-55) right after a sync or a nightly
	// run. Built by New with the Telegram notifier; tests replace Notify.
	Alerts *monitor.Monitor

	// passFn replaces syncPass in tests (to make the sync that follows an upload fail).
	passFn func(ctx context.Context) (*syncPass, error)
}

// New constructs an Engine. RepoDir is created (and the repo cloned) lazily
// on first sync if missing.
func New(db *sql.DB, repoDir, owner, name, token string) *Engine {
	return &Engine{
		DB: db, RepoDir: repoDir,
		RepoOwner: owner, RepoName: name,
		GitHubToken: token,
		Alerts:      &monitor.Monitor{DB: db, Checks: monitor.ThesisChecks(db), Notify: &monitor.Telegram{}},
	}
}

// Configured reports whether the engine has enough to operate. When the
// token isn't set (dev environment), the engine no-ops gracefully.
func (e *Engine) Configured() bool {
	return e != nil && e.RepoDir != "" && e.GitHubToken != "" && e.RepoOwner != "" && e.RepoName != ""
}

// EnsureClone idempotently makes sure RepoDir is a valid checkout of the
// configured GitHub repo. If RepoDir doesn't exist or isn't a git checkout,
// it shells out to `git clone`.
func (e *Engine) EnsureClone(ctx context.Context) error {
	if !e.Configured() {
		return fmt.Errorf("theses engine not configured (missing repo dir, owner, name, or token)")
	}
	if _, err := os.Stat(filepath.Join(e.RepoDir, ".git")); err == nil {
		return nil // already a checkout
	}
	if err := os.MkdirAll(filepath.Dir(e.RepoDir), 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	url := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git",
		e.GitHubToken, e.RepoOwner, e.RepoName)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", url, e.RepoDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %s: %w", scrub(string(out), e.GitHubToken), err)
	}
	slog.Info("theses: cloned repo", "dir", e.RepoDir)
	// Make sure the remote URL we save doesn't bake in the token; we re-use
	// the env var on push.
	clean := fmt.Sprintf("https://github.com/%s/%s.git", e.RepoOwner, e.RepoName)
	_ = exec.CommandContext(ctx, "git", "-C", e.RepoDir, "remote", "set-url", "origin", clean).Run()
	return nil
}

// ErrThesesDirMissing is returned when `theses/` does not exist after a good fetch. It is an error, never
// "no files": treating it as empty would archive every thesis (SC-55 P1).
var ErrThesesDirMissing = errors.New("theses: theses/ directory missing after fetch; nothing changed")

// Archive reasons stored in theses_index.archive_reason.
const (
	ArchiveFileMissing   = "file_missing"
	ArchiveHeaderInvalid = "header_invalid"
)

// syncFile is one valid *_locked.md file seen in a pass.
type syncFile struct {
	Key      string // TICKER_vN
	Header   Header
	RelPath  string
	FileSHA  string
	Declared string // status the header declares (locked | draft | superseded)
	body     string
}

// syncFailure is one *_locked.md file that could not be ingested.
type syncFailure struct {
	RelPath string
	Error   string
	Kind    string // FailHeader | FailDuplicate | FailRead | FailDatabase
}

// syncPass is everything one Sync saw; later hooks (reconciliation, failure records) read it.
type syncPass struct {
	Files      map[string]*syncFile // valid files by key
	Failures   []syncFailure        // invalid or conflicting files
	Archived   []archivedRow        // rows archived in this pass
	SafetyStop []archivedRow        // rows the pass WOULD have archived; non-empty = it archived nothing (SC-55 P2)
}

type archivedRow struct {
	ID      int64
	Ticker  string
	Version int
	Reason  string
}

// Sync runs one full reconciliation pass:
//  1. `git fetch && git reset --hard origin/main` to pull latest
//  2. Walk theses/<adapter>/*_locked.md (a missing theses/ directory is an error, nothing changes)
//  3. Parse, render, upsert into theses_index keyed by (ticker, version); an archived row whose
//     valid file is back is restored
//  4. ARCHIVE (never delete) rows whose file is gone (file_missing) or no longer parses (header_invalid)
//  5. Recompute supersede from scratch: per ticker the highest non-archived version is live
//  6. Recompute earnings_urgency from stock_holdings.earnings_date
//
// Safe to call concurrently with reads — uses a transaction per upsert.
func (e *Engine) Sync(ctx context.Context) error {
	run := e.syncPass
	if e.passFn != nil {
		run = e.passFn
	}
	pass, err := run(ctx)
	if e.Configured() {
		e.recordTick(ctx, err)
		if pass != nil {
			e.recordIngestFailures(ctx, pass.Failures)
		}
	}
	e.afterSync(ctx, pass, err)
	return err
}

// Archive safety stop (SC-55 P2): one pass that would archive more than this many rows, or more than this
// share of all rows, archives nothing and raises an alert instead. Variables so tests can tighten them.
var (
	maxArchivePerRun   = 2
	maxArchiveFraction = 0.05
	// The percentage rule only applies from this many rows: in a tiny table a single row is always
	// more than 5%. With 63 rows the absolute limit (2) is the binding one until the table passes 40.
	minRowsForFraction = 20
)

// scanRepo reads every theses/**/*_locked.md in the local clone (no database access): the valid files by
// key and the files that failed to parse or duplicate another file's (ticker, version). The first path in
// walk order wins a duplicate key.
func (e *Engine) scanRepo() (map[string]*syncFile, []syncFailure, error) {
	thesesRoot := filepath.Join(e.RepoDir, "theses")
	if st, err := os.Stat(thesesRoot); err != nil || !st.IsDir() {
		return nil, nil, ErrThesesDirMissing
	}
	files := map[string]*syncFile{}
	var failures []syncFailure
	err := filepath.WalkDir(thesesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, "_locked.md") {
			return nil
		}
		rel, _ := filepath.Rel(e.RepoDir, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			slog.Error("theses: read failed", "path", rel, "err", err)
			failures = append(failures, syncFailure{RelPath: rel, Error: "read: " + err.Error(), Kind: FailRead})
			return nil
		}
		h := ParseHeader(string(raw))
		if err := h.Validate(); err != nil {
			slog.Warn("theses: skip malformed", "path", rel, "err", err)
			failures = append(failures, syncFailure{RelPath: rel, Error: err.Error(), Kind: FailHeader})
			return nil
		}
		key := fmt.Sprintf("%s_v%d", h.Ticker, h.Version)
		if first, dup := files[key]; dup {
			msg := fmt.Sprintf("duplicate %s: %s already provides this ticker and version", key, first.RelPath)
			slog.Warn("theses: duplicate key", "path", rel, "first", first.RelPath)
			failures = append(failures, syncFailure{RelPath: rel, Error: msg, Kind: FailDuplicate})
			return nil
		}
		sum := sha1.Sum(raw)
		files[key] = &syncFile{Key: key, Header: h, RelPath: rel, FileSHA: hex.EncodeToString(sum[:]),
			Declared: h.Status, body: string(raw)}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("walk theses dir: %w", err)
	}
	return files, failures, nil
}

func (e *Engine) syncPass(ctx context.Context) (*syncPass, error) {
	if !e.Configured() {
		return nil, nil // graceful no-op when token missing (dev)
	}
	if err := e.EnsureClone(ctx); err != nil {
		return nil, err
	}
	if err := e.gitFetchReset(ctx); err != nil {
		return nil, err
	}
	files, failures, err := e.scanRepo()
	if err != nil {
		return nil, err
	}
	pass := &syncPass{Files: files, Failures: failures}

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := files[k]
		if err := e.upsert(ctx, f.Header, f.body, f.RelPath, f.FileSHA); err != nil {
			slog.Error("theses: upsert", "path", f.RelPath, "err", err)
			pass.Failures = append(pass.Failures, syncFailure{RelPath: f.RelPath, Error: "database: " + err.Error(), Kind: FailDatabase})
		}
	}

	// Archive rows whose source file is gone or no longer parses. Never deletes.
	if err := e.archiveMissing(ctx, pass); err != nil {
		slog.Error("theses: archive", "err", err)
	}
	// Supersede is recomputed every sync, so removing v2 brings v1 back.
	if err := e.recomputeStatus(ctx, pass); err != nil {
		slog.Error("theses: recompute status", "err", err)
	}
	// Recompute earnings urgency for every row (cheap — handful of rows).
	if err := e.refreshEarningsUrgency(ctx); err != nil {
		slog.Error("theses: refresh earnings", "err", err)
	}
	return pass, nil
}

// recomputeStatus marks, per ticker, every non-archived version below the highest one 'superseded', and
// returns the highest one to the status its header declares if it had been superseded. It does not
// depend on which files changed, so a vanished v2 returns v1 to live on the next sync. A live row
// whose status is already locked or draft is left alone (the live DB holds one such row, 373220.KS,
// locked while its header says Draft; the sync must not silently change that).
func (e *Engine) recomputeStatus(ctx context.Context, pass *syncPass) error {
	rows, err := e.DB.QueryContext(ctx, `SELECT id, ticker, version, status FROM theses_index WHERE status != 'archived'`)
	if err != nil {
		return err
	}
	type r struct {
		id      int64
		ticker  string
		version int
		status  string
	}
	byTicker := map[string][]r{}
	for rows.Next() {
		var x r
		if err := rows.Scan(&x.id, &x.ticker, &x.version, &x.status); err != nil {
			rows.Close()
			return err
		}
		byTicker[x.ticker] = append(byTicker[x.ticker], x)
	}
	rows.Close()
	for tk, list := range byTicker {
		top := 0
		for _, x := range list {
			if x.version > top {
				top = x.version
			}
		}
		for _, x := range list {
			want := "superseded"
			if x.version == top {
				// The live row keeps whatever status it has (set from the header at ingest; a hand edit
				// is preserved, as before). Only a row that was superseded returns to the status its
				// header declares.
				want = x.status
				if x.status == "superseded" {
					f, ok := pass.Files[fmt.Sprintf("%s_v%d", tk, x.version)]
					if !ok || f.Declared == "" || f.Declared == "superseded" {
						continue // nothing says what it should be
					}
					want = f.Declared
				}
			}
			if want != x.status {
				if _, err := e.DB.ExecContext(ctx, `UPDATE theses_index SET status = ? WHERE id = ?`, want, x.id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *Engine) gitFetchReset(ctx context.Context) error {
	// Inject token via -c http.extraHeader for the lifetime of this command.
	bearer := "x-access-token:" + e.GitHubToken
	cmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "http.https://github.com/.extraheader=Authorization: Basic "+basicAuth(bearer),
		"fetch", "origin", "main")
	// A rejected token (401/403) must fail the pass, never wait for a credential prompt.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %s: %w", scrub(string(out), e.GitHubToken), err)
	}
	cmd = exec.CommandContext(ctx, "git", "-C", e.RepoDir, "reset", "--hard", "origin/main")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset: %s: %w", scrub(string(out), e.GitHubToken), err)
	}
	return nil
}

func (e *Engine) upsert(ctx context.Context, h Header, body, relPath, fileSHA string) error {
	url := h.CanonicalGitHubURL(e.RepoOwner, e.RepoName)
	html := scorecards.Render(body)
	now := time.Now().Unix()

	// Skip if the row exists with the same SHA at the same path — but never skip an archived row:
	// its file is back and it must be restored. A moved file (same content, new folder) is updated so
	// github_path always names the file that exists.
	var existingSHA, existingStatus, existingPath string
	row := e.DB.QueryRowContext(ctx,
		`SELECT file_sha, status, github_path FROM theses_index WHERE ticker = ? AND version = ?`,
		h.Ticker, h.Version)
	if err := row.Scan(&existingSHA, &existingStatus, &existingPath); err == nil &&
		existingSHA == fileSHA && existingStatus != "archived" && existingPath == relPath {
		return nil // unchanged
	}

	var scoreVal sql.NullInt64
	if h.Score != nil {
		scoreVal = sql.NullInt64{Int64: int64(*h.Score), Valid: true}
	}

	_, err := e.DB.ExecContext(ctx, `
		INSERT INTO theses_index
		  (ticker, company_name, adapter, sub_type, score, max_score, version,
		   status, locked_date, github_path, github_url, markdown_content,
		   rendered_html, file_sha, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT (ticker, version) DO UPDATE SET
		   company_name     = excluded.company_name,
		   adapter          = excluded.adapter,
		   sub_type         = excluded.sub_type,
		   score            = excluded.score,
		   max_score        = excluded.max_score,
		   status           = excluded.status,
		   locked_date      = excluded.locked_date,
		   github_path      = excluded.github_path,
		   github_url       = excluded.github_url,
		   markdown_content = excluded.markdown_content,
		   rendered_html    = excluded.rendered_html,
		   file_sha         = excluded.file_sha,
		   archived_at      = NULL,
		   archive_reason   = NULL,
		   updated_at       = excluded.updated_at`,
		h.Ticker, nullStr(h.CompanyName), h.Adapter, nullStr(h.SubType),
		scoreVal, h.MaxScore, h.Version,
		h.Status, nullStr(h.LockedDate), relPath, url, body, html, fileSHA,
		now, now)
	return err
}

// archiveMissing archives (never deletes) every live row whose (ticker, version) has no valid file
// in this pass. The reason is header_invalid when a file at the row's stored path exists but failed
// to parse, file_missing otherwise.
func (e *Engine) archiveMissing(ctx context.Context, pass *syncPass) error {
	rows, err := e.DB.QueryContext(ctx, `SELECT id, ticker, version, github_path FROM theses_index WHERE status != 'archived'`)
	if err != nil {
		return err
	}
	invalidPath := map[string]bool{}
	for _, f := range pass.Failures {
		invalidPath[f.RelPath] = true
	}
	var todo []archivedRow
	for rows.Next() {
		var a archivedRow
		var path string
		if err := rows.Scan(&a.ID, &a.Ticker, &a.Version, &path); err != nil {
			rows.Close()
			return err
		}
		if _, ok := pass.Files[fmt.Sprintf("%s_v%d", a.Ticker, a.Version)]; ok {
			continue
		}
		a.Reason = ArchiveFileMissing
		if invalidPath[path] {
			a.Reason = ArchiveHeaderInvalid
		}
		todo = append(todo, a)
	}
	rows.Close()
	if len(todo) > 0 {
		var total int
		_ = e.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM theses_index`).Scan(&total)
		if len(todo) > maxArchivePerRun || (total >= minRowsForFraction && float64(len(todo)) > maxArchiveFraction*float64(total)) {
			slog.Error("theses: SAFETY STOP — archive batch too large, nothing archived",
				"would_archive", len(todo), "rows", total)
			pass.SafetyStop = todo
			return nil
		}
	}
	now := time.Now().Unix()
	for _, a := range todo {
		if _, err := e.DB.ExecContext(ctx,
			`UPDATE theses_index SET status = 'archived', archived_at = ?, archive_reason = ? WHERE id = ? AND status != 'archived'`,
			now, a.Reason, a.ID); err != nil {
			return err
		}
		slog.Warn("theses: archived", "ticker", a.Ticker, "version", a.Version, "reason", a.Reason)
		pass.Archived = append(pass.Archived, a)
	}
	return nil
}

// refreshEarningsUrgency joins to ticker_calendar (SC-56; every live thesis ticker has a row once the
// calendar job has run) and computes the urgency band:
//
//	none             — no date, or none within 14 days
//	amber            — next report within (3d, 14d] from today
//	red              — next report within [0, 3d]
//	revision_needed  — the LAST REPORTED date is later than the thesis lock date, i.e. a print happened
//	                   after the thesis was locked and it has not been re-scored (a re-lock after the
//	                   print clears it). It takes precedence over amber/red, and it keeps working after
//	                   the next date has rolled forward, because it reads last_reported_date, not the
//	                   (now future) next date.
func (e *Engine) refreshEarningsUrgency(ctx context.Context) error {
	today := time.Now().UTC().Format("2006-01-02")
	rows, err := e.DB.QueryContext(ctx, `
		SELECT t.id, COALESCE(t.locked_date, ''), COALESCE(c.earnings_date, ''), COALESCE(c.last_reported_date, '')
		  FROM theses_index t
		  LEFT JOIN ticker_calendar c ON c.ticker = t.ticker`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type up struct {
		id       int64
		earnings string
		urgency  string
	}
	var updates []up
	for rows.Next() {
		var id int64
		var locked, next, reported string
		if err := rows.Scan(&id, &locked, &next, &reported); err != nil {
			return err
		}
		if next != "" && next < today && next > reported { // a next date that has itself just passed
			reported = next
			next = ""
		}
		urgency := "none"
		switch {
		case reported != "" && locked != "" && reported > locked:
			urgency = "revision_needed"
		case next != "":
			if daysOut, ok := daysBetween(today, next); ok {
				switch {
				case daysOut >= 0 && daysOut <= 3:
					urgency = "red"
				case daysOut > 3 && daysOut <= 14:
					urgency = "amber"
				}
			}
		}
		updates = append(updates, up{id, next, urgency})
	}
	for _, u := range updates {
		_, _ = e.DB.ExecContext(ctx,
			`UPDATE theses_index SET next_earnings_date = NULLIF(?, ''), earnings_urgency = ? WHERE id = ?`,
			u.earnings, u.urgency, u.id)
	}
	return nil
}

// ----- helpers -----------------------------------------------------------

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// daysBetween returns (b - a) in calendar days. Returns (0, false) if either
// fails to parse. Negative means b is in the past.
func daysBetween(a, b string) (int, bool) {
	ta, err := time.Parse("2006-01-02", a)
	if err != nil {
		return 0, false
	}
	tb, err := time.Parse("2006-01-02", b)
	if err != nil {
		return 0, false
	}
	return int(tb.Sub(ta).Hours() / 24), true
}

// scrub redacts a token from a string before logging.
func scrub(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "[REDACTED]")
}

// basicAuth encodes "user:pass" for HTTP Basic.
func basicAuth(userpass string) string {
	return base64.StdEncoding.EncodeToString([]byte(userpass))
}
