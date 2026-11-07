package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ft/internal/store"
)

// SC-55 Task A — the token-missing branch of the thesis_sync check.
func newThesisMon(t *testing.T, n *fakeNotifier, now *time.Time) *Monitor {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return &Monitor{DB: st.DB, Checks: []Check{ThesisTokenCheck()}, Notify: n, Now: func() time.Time { return *now }}
}

func withToken(t *testing.T, present *bool, dir string) {
	t.Helper()
	oldP, oldD := thesisTokenPresent, thesisRepoDir
	thesisTokenPresent = func() bool { return *present }
	thesisRepoDir = func() string { return dir }
	t.Cleanup(func() { thesisTokenPresent, thesisRepoDir = oldP, oldD })
}

func TestThesisToken_AbsentWithCloneAlertsOnce_RestoredRecoversOnce(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := newThesisMon(t, n, &now)
	present := true
	withToken(t, &present, t.TempDir()) // the clone directory exists
	for i := 0; i < 3; i++ {            // token present: silent
		m.Tick(context.Background())
		now = now.Add(10 * time.Minute)
	}
	if len(n.msgs) != 0 {
		t.Fatalf("token present alerted: %q", n.msgs)
	}
	present = false // the token disappears from the environment
	m.Tick(context.Background())
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "FT thesis library: Thesis library sync looks down") ||
		!strings.Contains(n.msgs[0], "NOT running because FT_GITHUB_TOKEN is missing") {
		t.Fatalf("alerts: %q", n.msgs)
	}
	for i := 0; i < 5; i++ { // deduped while it lasts
		now = now.Add(10 * time.Minute)
		m.Tick(context.Background())
	}
	if len(n.msgs) != 1 {
		t.Fatalf("repeat alerts: %q", n.msgs)
	}
	now = now.Add(25 * time.Hour) // daily reminder
	m.Tick(context.Background())
	if len(n.msgs) != 2 || !strings.Contains(n.msgs[1], "STILL down") {
		t.Fatalf("reminder: %q", n.msgs)
	}
	present = true // restored
	now = now.Add(10 * time.Minute)
	m.Tick(context.Background())
	if len(n.msgs) != 3 || !strings.Contains(n.msgs[2], "is back") {
		t.Fatalf("recovery: %q", n.msgs)
	}
	now = now.Add(10 * time.Minute)
	m.Tick(context.Background())
	if len(n.msgs) != 3 {
		t.Fatalf("alert after recovery: %q", n.msgs)
	}
}

func TestThesisToken_AbsentWithoutCloneIsSilent(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := newThesisMon(t, n, &now)
	present := false
	withToken(t, &present, filepath.Join(t.TempDir(), "no-such-clone")) // dev / demo copy: no directory
	for i := 0; i < 3; i++ {
		m.Tick(context.Background())
		now = now.Add(time.Hour)
	}
	if len(n.msgs) != 0 {
		t.Fatalf("dev/demo without a clone alerted: %q", n.msgs)
	}
	// a plain file where the directory should be is not a clone either
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	thesisRepoDir = func() string { return f }
	m.Tick(context.Background())
	if len(n.msgs) != 0 {
		t.Fatalf("file instead of directory alerted: %q", n.msgs)
	}
}

func TestThesisToken_FirstEverTickWithoutTokenSaysSo(t *testing.T) {
	// A server that boots without a token (no prior monitor state) gets the "already looks stale" summary.
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	n := &fakeNotifier{}
	m := newThesisMon(t, n, &now)
	present := false
	withToken(t, &present, t.TempDir())
	m.Tick(context.Background())
	if len(n.msgs) != 1 || !strings.Contains(n.msgs[0], "FT_GITHUB_TOKEN is missing") {
		t.Fatalf("first tick: %q", n.msgs)
	}
}

func TestDefaultChecksCarryAllThesisChecks(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	have := map[string]string{}
	for _, c := range DefaultChecks(st.DB) {
		have[c.Source] = c.Domain
	}
	for _, src := range []string{"thesis_reconcile", "thesis_ingest", "thesis_sync", "thesis_token"} {
		if have[src] != "thesis library" {
			t.Fatalf("DefaultChecks lacks %s (domain %q): the main monitor and Diagnostics would not show it", src, have[src])
		}
	}
}
