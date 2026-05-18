package theses

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"ft/internal/scorecards"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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
}

// New constructs an Engine. RepoDir is created (and the repo cloned) lazily
// on first sync if missing.
func New(db *sql.DB, repoDir, owner, name, token string) *Engine {
	return &Engine{
		DB: db, RepoDir: repoDir,
		RepoOwner: owner, RepoName: name,
		GitHubToken: token,
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

// Sync runs one full reconciliation pass:
//  1. `git fetch && git reset --hard origin/main` to pull latest
//  2. Walk theses/<adapter>/*.md
//  3. Parse, render, upsert into theses_index keyed by (ticker, version)
//  4. Recompute earnings_urgency from stock_holdings.earnings_date
//  5. Delete rows whose source file is gone
//
// Safe to call concurrently with reads — uses a transaction per upsert.
func (e *Engine) Sync(ctx context.Context) error {
	if !e.Configured() {
		return nil // graceful no-op when token missing (dev)
	}
	if err := e.EnsureClone(ctx); err != nil {
		return err
	}
	if err := e.gitFetchReset(ctx); err != nil {
		return err
	}

	thesesRoot := filepath.Join(e.RepoDir, "theses")
	seen := map[string]bool{} // ticker_v<n>

	err := filepath.WalkDir(thesesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, "_locked.md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		h := ParseHeader(string(raw))
		if err := h.Validate(); err != nil {
			slog.Warn("theses: skip malformed", "path", path, "err", err)
			return nil
		}
		rel, _ := filepath.Rel(e.RepoDir, path)
		key := fmt.Sprintf("%s_v%d", h.Ticker, h.Version)
		seen[key] = true

		sum := sha1.Sum(raw)
		fileSHA := hex.EncodeToString(sum[:])

		if err := e.upsert(ctx, h, string(raw), rel, fileSHA); err != nil {
			slog.Error("theses: upsert", "path", path, "err", err)
			return nil // keep walking
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("walk theses dir: %w", err)
	}

	// Purge rows whose source MD no longer exists.
	if err := e.purgeMissing(ctx, seen); err != nil {
		slog.Error("theses: purge", "err", err)
	}
	// Recompute earnings urgency for every row (cheap — handful of rows).
	if err := e.refreshEarningsUrgency(ctx); err != nil {
		slog.Error("theses: refresh earnings", "err", err)
	}
	return nil
}

func (e *Engine) gitFetchReset(ctx context.Context) error {
	// Inject token via -c http.extraHeader for the lifetime of this command.
	bearer := "x-access-token:" + e.GitHubToken
	cmd := exec.CommandContext(ctx, "git", "-C", e.RepoDir,
		"-c", "http.https://github.com/.extraheader=Authorization: Basic "+basicAuth(bearer),
		"fetch", "origin", "main")
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

	// Check if row exists with same SHA — skip if unchanged.
	var existingSHA string
	row := e.DB.QueryRowContext(ctx,
		`SELECT file_sha FROM theses_index WHERE ticker = ? AND version = ?`,
		h.Ticker, h.Version)
	if err := row.Scan(&existingSHA); err == nil && existingSHA == fileSHA {
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
		   updated_at       = excluded.updated_at`,
		h.Ticker, nullStr(h.CompanyName), h.Adapter, nullStr(h.SubType),
		scoreVal, h.MaxScore, h.Version,
		h.Status, nullStr(h.LockedDate), relPath, url, body, html, fileSHA,
		now, now)
	return err
}

func (e *Engine) purgeMissing(ctx context.Context, seen map[string]bool) error {
	rows, err := e.DB.QueryContext(ctx, `SELECT id, ticker, version FROM theses_index`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type del struct{ id int64 }
	var deletes []del
	for rows.Next() {
		var id int64
		var tk string
		var ver int
		if err := rows.Scan(&id, &tk, &ver); err != nil {
			return err
		}
		if !seen[fmt.Sprintf("%s_v%d", tk, ver)] {
			deletes = append(deletes, del{id})
		}
	}
	for _, d := range deletes {
		_, _ = e.DB.ExecContext(ctx, `DELETE FROM theses_index WHERE id = ?`, d.id)
	}
	return nil
}

// refreshEarningsUrgency joins to stock_holdings.earnings_date and computes
// the urgency band per Spec 15 Phase 2:
//
//	none             — no upcoming earnings within 14 days
//	amber            — earnings within (3d, 14d] from today
//	red              — earnings within [0, 3d]
//	revision_needed  — earnings has passed AND was after the locked_date
//	                   (i.e. a print happened after the thesis was locked,
//	                    so the thesis needs a fresh re-score)
func (e *Engine) refreshEarningsUrgency(ctx context.Context) error {
	today := time.Now().UTC().Format("2006-01-02")
	// Pull all (ticker, locked_date) pairs from index, join to stocks.
	rows, err := e.DB.QueryContext(ctx, `
		SELECT t.id, t.ticker, COALESCE(t.locked_date, ''),
		       COALESCE(s.earnings_date, '')
		  FROM theses_index t
		  LEFT JOIN stock_holdings s
		    ON s.ticker = t.ticker AND s.deleted_at IS NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type up struct {
		id        int64
		earnings  string
		urgency   string
	}
	var updates []up
	for rows.Next() {
		var id int64
		var tk, locked, earn string
		if err := rows.Scan(&id, &tk, &locked, &earn); err != nil {
			return err
		}
		urgency := "none"
		if earn != "" {
			daysOut, ok := daysBetween(today, earn)
			if ok {
				switch {
				case daysOut < 0 && locked != "" && earn > locked:
					urgency = "revision_needed"
				case daysOut >= 0 && daysOut <= 3:
					urgency = "red"
				case daysOut > 3 && daysOut <= 14:
					urgency = "amber"
				}
			}
		}
		updates = append(updates, up{id, earn, urgency})
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
