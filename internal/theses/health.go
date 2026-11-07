package theses

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"
)

// Failure kinds stored in thesis_ingest_failures.kind.
const (
	FailHeader    = "header"    // header does not parse or validate
	FailDuplicate = "duplicate" // another file already provides this ticker and version
	FailRead      = "read"      // the file could not be read
	FailDatabase  = "database"  // the row could not be written
)

// IngestFailure is one open entry of the "failed" list on the Theses tab.
type IngestFailure struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	Error     string `json:"error"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
}

// SyncState is the shared sync-tick counter.
type SyncState struct {
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	FirstFailureAt      *int64 `json:"firstFailureAt,omitempty"`
	LastError           string `json:"lastError,omitempty"`
	LastAttemptAt       *int64 `json:"lastAttemptAt,omitempty"`
	LastOKAt            *int64 `json:"lastOkAt,omitempty"`
}

// ReconcileSummary is the latest reconciliation run, for the Theses tab banner.
type ReconcileSummary struct {
	RanAt      int64  `json:"ranAt"`
	Trigger    string `json:"trigger"`
	Status     string `json:"status"`
	Mismatches int    `json:"mismatches"`
	Files      int    `json:"files"`
	Rows       int    `json:"rows"`
	Archived   int    `json:"archived"`
}

// Health is what GET /api/theses/health returns.
type Health struct {
	Configured bool              `json:"configured"`
	Failures   []IngestFailure   `json:"failures"`
	Sync       SyncState         `json:"sync"`
	Reconcile  *ReconcileSummary `json:"reconcile,omitempty"`
}

// Health reads the failed-ingest list, the sync-tick state and the latest reconciliation.
func (e *Engine) Health(ctx context.Context) (*Health, error) {
	h := &Health{Configured: e.Configured(), Failures: []IngestFailure{}}
	rows, err := e.DB.QueryContext(ctx, `SELECT path, kind, error, first_seen, last_seen FROM thesis_ingest_failures WHERE resolved_at IS NULL ORDER BY first_seen, path`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f IngestFailure
		if err := rows.Scan(&f.Path, &f.Kind, &f.Error, &f.FirstSeen, &f.LastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		h.Failures = append(h.Failures, f)
	}
	rows.Close()
	var first, attempt, ok sql.NullInt64
	var lastErr sql.NullString
	err = e.DB.QueryRowContext(ctx, `SELECT consecutive_failures, first_failure_at, last_error, last_attempt_at, last_ok_at FROM thesis_sync_state WHERE id = 1`).
		Scan(&h.Sync.ConsecutiveFailures, &first, &lastErr, &attempt, &ok)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if first.Valid {
		h.Sync.FirstFailureAt = &first.Int64
	}
	if attempt.Valid {
		h.Sync.LastAttemptAt = &attempt.Int64
	}
	if ok.Valid {
		h.Sync.LastOKAt = &ok.Int64
	}
	h.Sync.LastError = lastErr.String
	var rs ReconcileSummary
	err = e.DB.QueryRowContext(ctx, `SELECT ran_at, trigger, status, mismatches, files, rows, archived FROM thesis_reconcile_runs ORDER BY id DESC LIMIT 1`).
		Scan(&rs.RanAt, &rs.Trigger, &rs.Status, &rs.Mismatches, &rs.Files, &rs.Rows, &rs.Archived)
	if err == nil {
		h.Reconcile = &rs
	}
	return h, nil
}

// recordIngestFailures upserts every failure of this pass as open and resolves the ones that are gone.
func (e *Engine) recordIngestFailures(ctx context.Context, failures []syncFailure) {
	now := time.Now().Unix()
	open := make([]any, 0, len(failures))
	for _, f := range failures {
		kind := f.Kind
		if kind == "" {
			kind = FailHeader
		}
		_, err := e.DB.ExecContext(ctx, `
			INSERT INTO thesis_ingest_failures (path, kind, error, first_seen, last_seen, resolved_at)
			VALUES (?, ?, ?, ?, ?, NULL)
			ON CONFLICT (path) DO UPDATE SET
			  kind = excluded.kind,
			  error = excluded.error,
			  last_seen = excluded.last_seen,
			  first_seen = CASE WHEN thesis_ingest_failures.resolved_at IS NULL THEN thesis_ingest_failures.first_seen ELSE excluded.first_seen END,
			  resolved_at = NULL`,
			f.RelPath, kind, f.Error, now, now)
		if err != nil {
			slog.Error("theses: record ingest failure", "path", f.RelPath, "err", err)
		}
		open = append(open, f.RelPath)
	}
	q := `UPDATE thesis_ingest_failures SET resolved_at = ? WHERE resolved_at IS NULL`
	args := []any{now}
	if len(open) > 0 {
		q += ` AND path NOT IN (?` + strings.Repeat(",?", len(open)-1) + `)`
		args = append(args, open...)
	}
	if _, err := e.DB.ExecContext(ctx, q, args...); err != nil {
		slog.Error("theses: resolve ingest failures", "err", err)
	}
}

// recordTick stores the outcome of one sync pass in the shared counter.
func (e *Engine) recordTick(ctx context.Context, syncErr error) {
	now := time.Now().Unix()
	var err error
	if syncErr == nil {
		_, err = e.DB.ExecContext(ctx, `UPDATE thesis_sync_state SET consecutive_failures = 0, first_failure_at = NULL, last_error = NULL, last_attempt_at = ?, last_ok_at = ? WHERE id = 1`, now, now)
	} else {
		msg := scrub(syncErr.Error(), e.GitHubToken)
		if len(msg) > 400 {
			msg = msg[:400]
		}
		_, err = e.DB.ExecContext(ctx, `
			UPDATE thesis_sync_state SET
			  consecutive_failures = consecutive_failures + 1,
			  first_failure_at = COALESCE(first_failure_at, ?),
			  last_error = ?, last_attempt_at = ?
			WHERE id = 1`, now, msg, now)
	}
	if err != nil {
		slog.Error("theses: record sync tick", "err", err)
	}
}
