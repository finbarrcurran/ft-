package monitor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// thesisDomain is the alert family for the Thesis Library checks ("FT thesis library: … looks down").
const thesisDomain = "thesis library"

// ThesisChecks are the SC-55 checks on the Thesis Library sync. They read state the sync writes to the
// database (never the repo or the network), so they cost nothing and can run in the main monitor as well
// as right after every sync. Alerts follow the monitor's rules: one message when a check goes down, a
// reminder every 24 h while it stays down, one when it recovers, and nothing while green.
func ThesisChecks(db *sql.DB) []Check {
	return []Check{
		{Source: "thesis_reconcile", Label: "Thesis library vs repo", Domain: thesisDomain,
			MaxAge: 36 * time.Hour, Eval: thesisReconcileEval(db)},
		// SC-55 P4: a file that cannot be ingested is loud — one message when it first fails.
		{Source: "thesis_ingest", Label: "Thesis ingest failures", Domain: thesisDomain,
			MaxAge: 36 * time.Hour, Eval: thesisIngestEval(db)},
		// SC-55 P4: the sync itself failing N times in a row (fetch / clone), about 30 minutes at the
		// 5-minute default. Over 6 months of history this fires once (a six-hour DNS outage, 2026-09-23);
		// the 9 isolated single failures stay quiet.
		{Source: "thesis_sync", Label: "Thesis library sync", Domain: thesisDomain,
			MaxAge: 36 * time.Hour, Eval: thesisSyncEval(db)},
	}
}

// ThesisSyncFailThreshold is the number of consecutive failed sync passes that raises the alert.
const ThesisSyncFailThreshold = 6

func thesisIngestEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		rows, err := db.QueryContext(ctx, `SELECT path, error FROM thesis_ingest_failures WHERE resolved_at IS NULL ORDER BY first_seen, path`)
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		defer rows.Close()
		var items []string
		n := 0
		for rows.Next() {
			var path, msg string
			if err := rows.Scan(&path, &msg); err != nil {
				return false, time.Time{}, false, "", err
			}
			n++
			if len(items) < 5 {
				if len(msg) > 140 {
					msg = msg[:140] + "…"
				}
				items = append(items, fmt.Sprintf("%s: %s", path, msg))
			}
		}
		if err := rows.Err(); err != nil {
			return false, time.Time{}, false, "", err
		}
		if n == 0 {
			return false, now, true, "no failed files", nil
		}
		more := ""
		if n > len(items) {
			more = fmt.Sprintf(" (+%d more)", n-len(items))
		}
		return true, now, true, fmt.Sprintf("%d file(s) cannot be ingested: %s%s", n, strings.Join(items, " | "), more), nil
	}
}

func thesisSyncEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		var n int
		var first, lastOK sql.NullInt64
		var lastErr sql.NullString
		err := db.QueryRowContext(ctx, `SELECT consecutive_failures, first_failure_at, last_error, last_ok_at FROM thesis_sync_state WHERE id = 1`).
			Scan(&n, &first, &lastErr, &lastOK)
		if err == sql.ErrNoRows {
			return false, time.Time{}, false, "no sync recorded yet", nil
		}
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		at := now
		if lastOK.Valid {
			at = time.Unix(lastOK.Int64, 0).UTC()
		}
		if n < ThesisSyncFailThreshold {
			return false, at, true, fmt.Sprintf("%d consecutive failed sync(s)", n), nil
		}
		since := ""
		if first.Valid {
			since = " since " + time.Unix(first.Int64, 0).UTC().Format("2006-01-02 15:04 UTC")
		}
		msg := lastErr.String
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return true, at, true, fmt.Sprintf("%d consecutive failed syncs%s; last error: %s", n, since, msg), nil
	}
}

// ReconcileIssue is one line of a reconciliation run's detail (stored as JSON in thesis_reconcile_runs).
type ReconcileIssue struct {
	Type    string `json:"type"`
	Ticker  string `json:"ticker,omitempty"`
	Version int    `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Note    string `json:"note,omitempty"`
}

func thesisReconcileEval(db *sql.DB) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		var ran int64
		var status, detail string
		var mismatches int
		err := db.QueryRowContext(ctx,
			`SELECT ran_at, status, mismatches, detail FROM thesis_reconcile_runs ORDER BY id DESC LIMIT 1`).
			Scan(&ran, &status, &mismatches, &detail)
		if err == sql.ErrNoRows {
			return false, time.Time{}, false, "no reconciliation run yet", nil
		}
		if err != nil {
			return false, time.Time{}, false, "", err
		}
		at := time.Unix(ran, 0).UTC()
		if status == "ok" {
			return false, at, true, "last run clean", nil
		}
		return true, at, true, SummariseReconcile(status, detail), nil
	}
}

// SummariseReconcile turns a run's JSON detail into one alert paragraph naming each mismatch type and
// its tickers (at most six per type).
func SummariseReconcile(status, detail string) string {
	var issues []ReconcileIssue
	if err := json.Unmarshal([]byte(detail), &issues); err != nil || len(issues) == 0 {
		return "reconciliation " + status
	}
	order := []string{}
	byType := map[string][]string{}
	for _, is := range issues {
		if _, ok := byType[is.Type]; !ok {
			order = append(order, is.Type)
		}
		name := is.Ticker
		if name == "" {
			name = is.Path
		}
		if is.Version > 0 {
			name = fmt.Sprintf("%s v%d", name, is.Version)
		}
		if is.Note != "" {
			name += " (" + is.Note + ")"
		}
		byType[is.Type] = append(byType[is.Type], name)
	}
	var parts []string
	for _, t := range order {
		names := byType[t]
		more := ""
		if len(names) > 6 {
			more = fmt.Sprintf(" +%d more", len(names)-6)
			names = names[:6]
		}
		parts = append(parts, fmt.Sprintf("%s: %s%s", strings.ReplaceAll(t, "_", " "), strings.Join(names, ", "), more))
	}
	prefix := ""
	if status == "aborted" {
		prefix = "SYNC ABORTED, nothing changed. "
	}
	return prefix + strings.Join(parts, "; ")
}
