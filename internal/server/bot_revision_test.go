package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"ft/internal/config"
)

// SC-56b: the per-holding thesis_revision_needed bot alert is gone (the daily summary replaces it).
func TestBotAlerts_NoPerHoldingRevisionNudge(t *testing.T) {
	_, st := snapServer(t)
	// a configured thesis engine, as in production (the old code only emitted the nudge when s.theses != nil)
	s := New(&config.Config{GitHubToken: "t", ThesisRepoOwner: "o", ThesisRepoName: "n", ThesisRepoDir: t.TempDir()}, st, nil)
	mustExec(t, st, `INSERT INTO stock_holdings (user_id, name, ticker, invested_usd, updated_at) VALUES (1, 'Acme', 'ACME', 100, 0)`)
	mustExec(t, st, `INSERT INTO theses_index (ticker, adapter, version, status, score, max_score, locked_date, github_path, github_url, markdown_content, rendered_html, file_sha, earnings_urgency, created_at, updated_at)
		VALUES ('ACME', 'pharma', 1, 'locked', 11, 16, '2026-05-18', 'theses/pharma/x.md', 'u', 'body', '<p>x</p>', 'sha', 'revision_needed', 0, 0)`)
	for _, q := range []string{"/api/bot/alerts", "/api/bot/alerts?only_unnotified=0"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", q, nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1)))
		s.handleBotAlerts(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), "thesis_revision_needed") {
			t.Fatalf("%s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
}
