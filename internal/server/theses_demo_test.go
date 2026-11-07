package server

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ft/internal/config"
)

// SC-55 close: with demo mode on, the Thesis Library must not reveal what is held or watched.
func TestThesisLibraryDemoMode(t *testing.T) {
	s, st := snapServer(t)
	s2 := New(&config.Config{GitHubToken: "t", ThesisRepoOwner: "o", ThesisRepoName: "n", ThesisRepoDir: t.TempDir()}, st, nil)
	_ = s
	mustExec(t, st, `INSERT INTO stock_holdings (user_id, name, ticker, invested_usd, updated_at) VALUES (1, 'Secret Corp', 'SECRETCO', 12345, 0)`)
	mustExec(t, st, `INSERT INTO theses_index (ticker, adapter, version, status, score, max_score, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, created_at, updated_at)
		VALUES ('SECRETCO', 'pharma', 1, 'locked', 11, 16, '2026-05-18', 'theses/pharma/x.md', 'u', 'body', '<p>x</p>', 'sha', 0, 0)`)
	list := func() string {
		rec := httptest.NewRecorder()
		s2.handleListTheses(rec, httptest.NewRequest("GET", "/api/theses", nil))
		return rec.Body.String()
	}
	if !strings.Contains(list(), `"ownership":"owned"`) {
		t.Fatalf("demo off should tag the holding as owned: %s", list())
	}
	mustExec(t, st, `INSERT OR REPLACE INTO user_preferences (key, value, updated_at) VALUES ('demo_mode','on',0)`)
	if b := list(); strings.Contains(b, `"owned"`) || strings.Contains(b, `"watchlist"`) {
		t.Fatalf("demo on leaks ownership: %s", b)
	}
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"GET /api/theses/gaps", s.requireUser(s.emptyInDemo(`) {
		t.Fatal("/api/theses/gaps must be wrapped in emptyInDemo")
	}
}
