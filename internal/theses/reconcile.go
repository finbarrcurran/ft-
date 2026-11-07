package theses

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"ft/internal/monitor"
)

// Reconcile issue types written to thesis_reconcile_runs.detail.
const (
	IssueFileNotIndexed = "file_not_indexed" // valid file in the repo, no row
	IssueRowWithoutFile = "row_without_file" // live row, no valid file
	IssueArchived       = "archived"         // row archived (file missing or unreadable) and not yet resolved
	IssueFileInvalid    = "file_invalid"     // file present but its header does not parse (or duplicates another)
	IssueSHAMismatch    = "sha_mismatch"     // row's stored SHA differs from the file
	IssuePathMismatch   = "path_mismatch"    // row's github_path differs from where the file is
	IssueStatusSanity   = "status_sanity"    // no live row, or more than one, for a ticker
	IssueSafetyStop     = "safety_stop"      // the pass would have archived too many rows and archived none
	IssueDirMissing     = "dir_missing"      // theses/ missing after a good fetch; nothing changed
	IssueCloneStale     = "clone_stale"      // the local clone is behind origin/main (nightly check)
	IssueSyncFailed     = "sync_failed"      // the sync itself failed before reconciling
)

// afterSync records the reconciliation for a finished pass and lets the thesis checks alert.
// pass is nil when the engine is not configured or the sync failed before scanning.
func (e *Engine) afterSync(ctx context.Context, pass *syncPass, syncErr error) {
	if !e.Configured() {
		return
	}
	switch {
	case syncErr == ErrThesesDirMissing:
		e.recordRun(ctx, "sync", "aborted", 0, []monitor.ReconcileIssue{{Type: IssueDirMissing, Note: "theses/ is missing after a good fetch; no row was changed"}})
	case syncErr != nil:
		// Fetch and clone failures are handled by the sync-tick check (P4); they do not touch the DB, so
		// the last reconciliation result stands.
	case pass != nil:
		e.reconcile(ctx, pass.Files, pass.Failures, pass.SafetyStop, "sync")
	}
	e.alertTick(ctx)
}

func (e *Engine) alertTick(ctx context.Context) {
	if e.Alerts != nil {
		e.Alerts.Tick(ctx)
	}
}

// ReconcileNow is the nightly run: it reads the local clone as it is (no database write besides the run
// record), compares it with the table, checks that the clone is not behind origin/main, and lets the
// thesis checks alert.
func (e *Engine) ReconcileNow(ctx context.Context) error {
	if !e.Configured() {
		return nil
	}
	if err := e.EnsureClone(ctx); err != nil {
		return err
	}
	files, failures, err := e.scanRepo()
	if err == ErrThesesDirMissing {
		e.recordRun(ctx, "nightly", "aborted", 0, []monitor.ReconcileIssue{{Type: IssueDirMissing, Note: "theses/ is missing in the local clone"}})
		e.alertTick(ctx)
		return err
	}
	if err != nil {
		return err
	}
	extra := e.cloneStaleIssues(ctx)
	e.reconcileWith(ctx, files, failures, nil, "nightly", extra)
	e.alertTick(ctx)
	return nil
}

// cloneStaleIssues compares the clone's HEAD with origin/main (ls-remote). A network failure is not an
// issue here; repeated sync failures are the P4 check's job.
func (e *Engine) cloneStaleIssues(ctx context.Context) []monitor.ReconcileIssue {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	remote, err := exec.CommandContext(cctx, "git", "-C", e.RepoDir,
		"-c", "http.https://github.com/.extraheader=Authorization: Basic "+basicAuth("x-access-token:"+e.GitHubToken),
		"ls-remote", "origin", "refs/heads/main").Output()
	if err != nil {
		return nil
	}
	head, err := exec.CommandContext(cctx, "git", "-C", e.RepoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil
	}
	r := strings.Fields(string(remote))
	h := strings.TrimSpace(string(head))
	if len(r) > 0 && h != "" && r[0] != h {
		return []monitor.ReconcileIssue{{Type: IssueCloneStale, Note: fmt.Sprintf("clone at %.7s, origin/main at %.7s", h, r[0])}}
	}
	return nil
}

func (e *Engine) reconcile(ctx context.Context, files map[string]*syncFile, failures []syncFailure, stopped []archivedRow, trigger string) {
	e.reconcileWith(ctx, files, failures, stopped, trigger, nil)
}

// reconcileWith compares the repo's files with theses_index and stores one run.
func (e *Engine) reconcileWith(ctx context.Context, files map[string]*syncFile, failures []syncFailure, stopped []archivedRow, trigger string, extra []monitor.ReconcileIssue) {
	type dbrow struct {
		id                int64
		ticker            string
		version           int
		status, sha, path string
		archiveReason     string
	}
	rows, err := e.DB.QueryContext(ctx, `SELECT id, ticker, version, status, file_sha, github_path, COALESCE(archive_reason, '') FROM theses_index`)
	if err != nil {
		slog.Error("theses: reconcile read", "err", err)
		return
	}
	var all []dbrow
	for rows.Next() {
		var r dbrow
		if err := rows.Scan(&r.id, &r.ticker, &r.version, &r.status, &r.sha, &r.path, &r.archiveReason); err != nil {
			rows.Close()
			slog.Error("theses: reconcile scan", "err", err)
			return
		}
		all = append(all, r)
	}
	rows.Close()

	var issues []monitor.ReconcileIssue
	issues = append(issues, extra...)
	byKey := map[string]dbrow{}
	var live, sup, arch int
	liveByTicker := map[string]int{}
	nonArchived := map[string]int{}
	for _, r := range all {
		key := fmt.Sprintf("%s_v%d", r.ticker, r.version)
		byKey[key] = r
		switch r.status {
		case "archived":
			arch++
		case "superseded":
			sup++
			nonArchived[r.ticker]++
		default:
			live++
			liveByTicker[r.ticker]++
			nonArchived[r.ticker]++
		}
		_, has := files[key]
		switch {
		case r.status == "archived":
			note := r.archiveReason
			issues = append(issues, monitor.ReconcileIssue{Type: IssueArchived, Ticker: r.ticker, Version: r.version, Path: r.path, Note: note})
		case !has:
			issues = append(issues, monitor.ReconcileIssue{Type: IssueRowWithoutFile, Ticker: r.ticker, Version: r.version, Path: r.path})
		}
	}
	for key, f := range files {
		r, ok := byKey[key]
		switch {
		case !ok:
			issues = append(issues, monitor.ReconcileIssue{Type: IssueFileNotIndexed, Ticker: f.Header.Ticker, Version: f.Header.Version, Path: f.RelPath})
		case r.status == "archived":
			// the file is back but the row is still archived: the sync restores it; reported under archived
		default:
			if r.sha != f.FileSHA {
				issues = append(issues, monitor.ReconcileIssue{Type: IssueSHAMismatch, Ticker: r.ticker, Version: r.version, Path: f.RelPath})
			}
			if r.path != f.RelPath {
				issues = append(issues, monitor.ReconcileIssue{Type: IssuePathMismatch, Ticker: r.ticker, Version: r.version, Path: f.RelPath, Note: "row says " + r.path})
			}
		}
	}
	for _, f := range failures {
		issues = append(issues, monitor.ReconcileIssue{Type: IssueFileInvalid, Path: f.RelPath, Note: f.Error})
	}
	for tk, n := range nonArchived {
		if liveByTicker[tk] != 1 {
			issues = append(issues, monitor.ReconcileIssue{Type: IssueStatusSanity, Ticker: tk, Note: fmt.Sprintf("%d live rows among %d non-archived", liveByTicker[tk], n)})
		}
	}
	status := "ok"
	if len(issues) > 0 {
		status = "mismatch"
	}
	if len(stopped) > 0 {
		status = "aborted"
		for _, a := range stopped {
			issues = append(issues, monitor.ReconcileIssue{Type: IssueSafetyStop, Ticker: a.Ticker, Version: a.Version, Note: "would archive: " + a.Reason})
		}
	}
	sortIssues(issues)
	e.recordRunCounts(ctx, trigger, status, len(files), len(all), live, sup, arch, issues)
}

func sortIssues(is []monitor.ReconcileIssue) {
	// stable, readable order: by type then ticker then path
	for i := 1; i < len(is); i++ {
		for j := i; j > 0 && less(is[j], is[j-1]); j-- {
			is[j], is[j-1] = is[j-1], is[j]
		}
	}
}

func less(a, b monitor.ReconcileIssue) bool {
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if a.Ticker != b.Ticker {
		return a.Ticker < b.Ticker
	}
	if a.Version != b.Version {
		return a.Version < b.Version
	}
	return a.Path < b.Path
}

func (e *Engine) recordRun(ctx context.Context, trigger, status string, files int, issues []monitor.ReconcileIssue) {
	var rows, live, sup, arch int
	_ = e.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(status NOT IN ('superseded','archived')),0), COALESCE(SUM(status='superseded'),0), COALESCE(SUM(status='archived'),0) FROM theses_index`).Scan(&rows, &live, &sup, &arch)
	e.recordRunCounts(ctx, trigger, status, files, rows, live, sup, arch, issues)
}

func (e *Engine) recordRunCounts(ctx context.Context, trigger, status string, files, rows, live, sup, arch int, issues []monitor.ReconcileIssue) {
	detail, _ := json.Marshal(issues)
	if issues == nil {
		detail = []byte("[]")
	}
	if _, err := e.DB.ExecContext(ctx, `
		INSERT INTO thesis_reconcile_runs (ran_at, trigger, status, files, rows, live, superseded, archived, mismatches, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().Unix(), trigger, status, files, rows, live, sup, arch, len(issues), string(detail)); err != nil {
		slog.Error("theses: reconcile record", "err", err)
		return
	}
	// keep a short history
	_, _ = e.DB.ExecContext(ctx, `DELETE FROM thesis_reconcile_runs WHERE id NOT IN (SELECT id FROM thesis_reconcile_runs ORDER BY id DESC LIMIT 90)`)
	if status != "ok" {
		slog.Warn("theses: reconcile found issues", "trigger", trigger, "status", status, "issues", len(issues))
	}
}
