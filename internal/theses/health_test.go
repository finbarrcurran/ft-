package theses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHealth_FailedIngestListOpensAndResolves(t *testing.T) {
	e := newEnv(t)
	seed(e)
	h, err := e.eng.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Failures) != 0 || h.Sync.ConsecutiveFailures != 0 || h.Reconcile == nil || h.Reconcile.Status != "ok" {
		t.Fatalf("healthy state: %+v", h)
	}
	e.write("theses/pharma/AAA_v1_locked.md", "no header\n")
	e.write("theses/zzz/BBB_v1_locked.md", thesisMD("BBB", 1, 3)) // duplicate key of pharma/BBB (first path wins)
	e.push("two bad files")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	h, _ = e.eng.Health(context.Background())
	if len(h.Failures) != 2 {
		t.Fatalf("failures = %+v", h.Failures)
	}
	kinds := map[string]string{}
	for _, f := range h.Failures {
		kinds[f.Path] = f.Kind
		if f.FirstSeen == 0 || f.Error == "" {
			t.Fatalf("incomplete failure record %+v", f)
		}
	}
	if kinds["theses/pharma/AAA_v1_locked.md"] != FailHeader || kinds["theses/zzz/BBB_v1_locked.md"] != FailDuplicate {
		t.Fatalf("kinds = %v", kinds)
	}
	first := h.Failures[0].FirstSeen
	// A second sync keeps first_seen and clears nothing.
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	h, _ = e.eng.Health(context.Background())
	if len(h.Failures) != 2 || h.Failures[0].FirstSeen != first {
		t.Fatalf("after second sync: %+v", h.Failures)
	}
	// Fix one, delete the other: both resolve.
	e.write("theses/pharma/AAA_v1_locked.md", thesisMD("AAA", 1, 12))
	e.remove("theses/zzz")
	e.push("fix")
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	h, _ = e.eng.Health(context.Background())
	if len(h.Failures) != 0 {
		t.Fatalf("failures not cleared: %+v", h.Failures)
	}
	var resolved int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM thesis_ingest_failures WHERE resolved_at IS NOT NULL`).Scan(&resolved); err != nil || resolved != 2 {
		t.Fatalf("resolved rows %d err %v", resolved, err)
	}
}

func TestHealth_SixConsecutiveSyncFailuresAlertOnceAndRecover(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.takeAlerts()
	// Break the remote: every fetch fails.
	broken := e.origin + ".gone"
	if err := os.Rename(e.origin, broken); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if err := e.sync(); err == nil {
			t.Fatal("sync unexpectedly succeeded")
		}
		if len(e.alerts) != 0 {
			t.Fatalf("alert after only %d failure(s): %q", i, e.alerts)
		}
	}
	if err := e.sync(); err == nil {
		t.Fatal("sync unexpectedly succeeded")
	}
	a := e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "Thesis library sync looks down") || !strings.Contains(a[0], "6 consecutive failed syncs") {
		t.Fatalf("alerts after 6 failures: %q", a)
	}
	for i := 0; i < 3; i++ {
		_ = e.sync()
	}
	if len(e.alerts) != 0 {
		t.Fatalf("repeat alerts while still failing: %q", e.alerts)
	}
	h, _ := e.eng.Health(context.Background())
	if h.Sync.ConsecutiveFailures != 9 || h.Sync.LastError == "" {
		t.Fatalf("sync state: %+v", h.Sync)
	}
	// The data is untouched while the remote is down.
	for _, tk := range []string{"AAA", "BBB", "CCC"} {
		e.want(tk, 1, "locked")
	}
	// Remote back: one recovery message, counter reset.
	if err := os.Rename(broken, e.origin); err != nil {
		t.Fatal(err)
	}
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	a = e.takeAlerts()
	if len(a) != 1 || !strings.Contains(a[0], "is back") {
		t.Fatalf("recovery: %q", a)
	}
	h, _ = e.eng.Health(context.Background())
	if h.Sync.ConsecutiveFailures != 0 {
		t.Fatalf("counter not reset: %+v", h.Sync)
	}
}

func TestHealth_FiveFailuresThenSuccessNeverAlerts(t *testing.T) {
	e := newEnv(t)
	seed(e)
	broken := e.origin + ".gone"
	_ = os.Rename(e.origin, broken)
	for i := 0; i < 5; i++ {
		_ = e.sync()
	}
	_ = os.Rename(broken, e.origin)
	if err := e.sync(); err != nil {
		t.Fatal(err)
	}
	if len(e.alerts) != 0 {
		t.Fatalf("alerts: %q", e.alerts)
	}
}

func TestUpload_SyncFailureIsInTheResponse(t *testing.T) {
	e := newEnv(t)
	seed(e)
	e.eng.passFn = func(ctx context.Context) (*syncPass, error) { return nil, errors.New("boom: fetch failed") }
	res, err := e.eng.Upload(context.Background(), UploadOpts{ThesisFilename: "NEW_v1_locked.md", ThesisContent: []byte(thesisMD("NEW", 1, 7))})
	if err != nil {
		t.Fatalf("upload itself must succeed once the file is pushed: %v", err)
	}
	if res.SyncError == "" || !strings.Contains(res.SyncError, "boom") {
		t.Fatalf("SyncError = %q", res.SyncError)
	}
	// the file really is in the repo
	if out := git(t, e.eng.RepoDir, "log", "--oneline", "-1"); !strings.Contains(out, "NEW") {
		t.Fatalf("no commit for NEW: %s", out)
	}
	// a bad header is refused up front, with the parser's message
	if _, err := e.eng.Upload(context.Background(), UploadOpts{ThesisFilename: "bad.md", ThesisContent: []byte("nope")}); err == nil || !strings.Contains(err.Error(), "invalid thesis MD") {
		t.Fatalf("bad header upload: %v", err)
	}
}

// SC-55 Task A — an expired or revoked GitHub token (HTTP 401/403 on fetch) is a failed sync pass and
// counts toward the six that raise the alert. The remote is a local HTTP server that answers like GitHub does.
func TestHealth_RejectedTokenCountsTowardTheAlert(t *testing.T) {
	for _, code := range []int{401, 403} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Setenv("GIT_TERMINAL_PROMPT", "0")
			e := newEnv(t)
			seed(e)
			e.takeAlerts()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
				http.Error(w, "remote: Invalid username or token. Password authentication is not supported for Git operations.", code)
			}))
			defer srv.Close()
			git(t, e.eng.RepoDir, "remote", "set-url", "origin", srv.URL+"/finbarrcurran/cross_sector_research.git")
			e.eng.GitHubToken = "ghp_SECRET_TOKEN_VALUE"
			for i := 1; i <= 5; i++ {
				if err := e.sync(); err == nil {
					t.Fatalf("pass %d unexpectedly succeeded against a %d", i, code)
				}
				if len(e.alerts) != 0 {
					t.Fatalf("alert after only %d rejected pass(es): %q", i, e.alerts)
				}
			}
			err := e.sync()
			if err == nil {
				t.Fatal("sixth pass succeeded")
			}
			if strings.Contains(err.Error(), "ghp_SECRET_TOKEN_VALUE") {
				t.Fatalf("token leaked into the error: %v", err)
			}
			a := e.takeAlerts()
			if len(a) != 1 || !strings.Contains(a[0], "Thesis library sync looks down") || !strings.Contains(a[0], "6 consecutive failed syncs") {
				t.Fatalf("alerts after six %d answers: %q", code, a)
			}
			for i := 0; i < 3; i++ {
				_ = e.sync()
			}
			if len(e.alerts) != 0 {
				t.Fatalf("repeat alerts: %q", e.alerts)
			}
			h, _ := e.eng.Health(context.Background())
			if h.Sync.ConsecutiveFailures != 9 || h.Sync.LastError == "" || strings.Contains(h.Sync.LastError, "ghp_SECRET") {
				t.Fatalf("sync state: %+v", h.Sync)
			}
			// data untouched; a fresh valid token (remote back) recovers with one message
			for _, tk := range []string{"AAA", "BBB", "CCC"} {
				e.want(tk, 1, "locked")
			}
			git(t, e.eng.RepoDir, "remote", "set-url", "origin", e.origin)
			if err := e.sync(); err != nil {
				t.Fatal(err)
			}
			if a := e.takeAlerts(); len(a) != 1 || !strings.Contains(a[0], "is back") {
				t.Fatalf("recovery: %q", a)
			}
		})
	}
}
