package server

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ft/internal/config"
)

// SC-56 demo mode: the calendar must not reveal the watchlist, the Theses-tab calendar fields follow the
// same masks as the rest of the Thesis Library, and Diagnostics must not name watchlist tickers from the
// report-date checks. Holdings keep their calendar (public market data on tickers the demo book already shows).
func TestCalendarFieldsInDemoMode(t *testing.T) {
	s, st := snapServer(t)
	now := time.Now().Unix()
	mustExec(t, st, `DELETE FROM watchlist`)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, company_name, added_at, updated_at) VALUES (1, 'SECRETW', 'stock', 'Secret Watch', 0, 0)`)
	mustExec(t, st, `INSERT INTO ticker_calendar (ticker, earnings_status, last_error, error_count, error_permanent, updated_at)
		VALUES ('SECRETW', 'FETCH_ERROR', 'http 404: Quote not found for symbol: SECRETW', 1, 1, ?)`, now)
	mustExec(t, st, `INSERT INTO theses_index (id, ticker, adapter, version, status, score, max_score, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, created_at, updated_at)
		VALUES (7, 'SECRETW', 'pharma', 1, 'locked', 11, 16, '2026-05-18', 'theses/pharma/x.md', 'u', 'body', '<p>x</p>', 'sha', 0, 0)`)

	mustExec(t, st, `INSERT INTO monitor_events (ts, source, kind, detail, delivered) VALUES (?, 'report_dates', 'down', 'fetch error: SECRETE (http 404)', 1)`, now)
	diag := func() string {
		rec := httptest.NewRecorder()
		s.handleDiagnostics(rec, httptest.NewRequest("GET", "/api/diagnostics", nil).WithContext(context.Background()))
		return rec.Body.String()
	}
	if b := diag(); !strings.Contains(b, "SECRETW") || !strings.Contains(b, "SECRETE") {
		t.Fatalf("demo off: the report_dates check should name the failing ticker: %.600s", b)
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	if b := diag(); strings.Contains(b, "SECRETW") || strings.Contains(b, "SECRETE") {
		t.Fatalf("demo on: Diagnostics names a watchlist ticker: %.600s", b)
	}

	// Theses-tab calendar fields: shown with demo off, absent with demo on.
	mustExec(t, st, `UPDATE ticker_calendar SET earnings_status = 'REPORTED', earnings_source = 'yahoo_calendar', last_reported_date = '2026-11-06',
		last_error = NULL, error_count = 0, error_permanent = 0, earnings_fetched_at = ? WHERE ticker = 'SECRETW'`, now)
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','off',0)`)
	s2 := New(&config.Config{GitHubToken: "t", ThesisRepoOwner: "o", ThesisRepoName: "n", ThesisRepoDir: t.TempDir()}, st, nil)
	list := func() string {
		rec := httptest.NewRecorder()
		s2.handleListTheses(rec, httptest.NewRequest("GET", "/api/theses", nil))
		return rec.Body.String()
	}
	if b := list(); !strings.Contains(b, "2026-11-06") || !strings.Contains(b, "yahoo_calendar") {
		t.Fatalf("demo off: the Theses row should carry the calendar fields: %s", b)
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	if b := list(); strings.Contains(b, "2026-11-06") || strings.Contains(b, "yahoo_calendar") || strings.Contains(b, "REPORTED") || strings.Contains(b, "earningsFetchedAt") {
		t.Fatalf("demo on: the Theses row leaks calendar fields: %s", b)
	}
	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routes), `"GET /api/watchlist", s.requireUser(s.emptyInDemo(`) {
		t.Fatal("/api/watchlist (which carries the calendar column) must be wrapped in emptyInDemo")
	}
}
