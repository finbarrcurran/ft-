package server

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ft/internal/config"
)

// SC-55 close: with demo mode on, no Thesis Library endpoint may reveal what is held or watched
// (owned/watchlist tag, earnings date/urgency that only held tickers carry, the gap list), and the
// endpoints that write to the real research repo are refused.
func TestThesisLibraryDemoMode(t *testing.T) {
	_, st := snapServer(t)
	s := New(&config.Config{GitHubToken: "t", ThesisRepoOwner: "o", ThesisRepoName: "n", ThesisRepoDir: t.TempDir()}, st, nil)
	mustExec(t, st, `INSERT INTO stock_holdings (user_id, name, ticker, invested_usd, updated_at) VALUES (1, 'Secret Corp', 'SECRETCO', 12345, 0)`)
	mustExec(t, st, `INSERT INTO theses_index (id, ticker, adapter, version, status, score, max_score, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, next_earnings_date, earnings_urgency, created_at, updated_at)
		VALUES (7, 'SECRETCO', 'pharma', 1, 'locked', 11, 16, '2026-05-18', 'theses/pharma/x.md', 'u', 'body', '<p>x</p>', 'sha', '2026-12-01', 'revision_needed', 0, 0)`)

	get := func(name string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/theses/7", nil)
		req.SetPathValue("id", "7")
		switch name {
		case "list":
			s.handleListTheses(rec, req)
		case "byid":
			s.handleGetThesisLibrary(rec, req)
		case "prompt":
			s.handleThesisRevisionPrompt(rec, req)
		}
		return rec.Body.String()
	}
	for _, n := range []string{"list", "byid"} {
		b := get(n)
		if !strings.Contains(b, `"ownership":"owned"`) && n == "list" || !strings.Contains(b, "2026-12-01") || !strings.Contains(b, "revision_needed") {
			t.Fatalf("%s with demo off should show the real values: %s", n, b)
		}
	}
	if !strings.Contains(get("prompt"), "2026-12-01") {
		t.Fatal("prompt with demo off should carry the earnings date")
	}

	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	for _, n := range []string{"list", "byid", "prompt"} {
		b := get(n)
		if strings.Contains(b, `"owned"`) || strings.Contains(b, `"watchlist"`) || strings.Contains(b, "2026-12-01") || strings.Contains(b, "revision_needed") {
			t.Fatalf("%s leaks portfolio-derived fields in demo mode: %s", n, b)
		}
	}

	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"GET /api/theses/gaps", s.requireUser(s.emptyInDemo(`,
		`"POST /api/theses/upload", s.requireUser(s.denyInDemo(`,
		`"POST /api/theses/scoring-log", s.requireUser(s.denyInDemo(`,
		`"POST /api/theses/sync", s.requireUser(s.denyInDemo(`,
	} {
		if !strings.Contains(string(src), want) {
			t.Fatalf("route wrapper missing: %s", want)
		}
	}
}
