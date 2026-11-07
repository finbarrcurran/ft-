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
