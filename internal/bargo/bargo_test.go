package bargo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ft/internal/store"
)

func openDB(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	return st
}

func trade(slug, ticker, txn, disc string) Trade {
	return Trade{Member: slug, MemberSlug: slug, Chamber: "house", State: "OH06", Ticker: ticker,
		Asset: ticker + " Inc", Type: "purchase", AmountRange: "$1,001 - $15,000",
		TransactionDate: txn, DisclosureDate: disc, FilingPortal: "https://example.test/f"}
}

// A fake Bargo that serves the given trades and enforces the bearer key.
func fake(t *testing.T, all []Trade, quotaAfter int, wantKey string) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantKey != "" && r.Header.Get("Authorization") != "Bearer "+wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("key") != "" || r.URL.Query().Get("api_key") != "" {
			t.Error("key must never be sent in the URL")
		}
		calls++
		if quotaAfter > 0 && calls > quotaAfter {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
		var out []Trade
		for _, tr := range all {
			if tr.TransactionDate >= from && tr.TransactionDate <= to {
				out = append(out, tr)
			}
		}
		_ = json.NewEncoder(w).Encode(tradePage{Trades: out, Count: len(out)})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSyncUpsertAndHistory(t *testing.T) {
	ctx := context.Background()
	st := openDB(t)
	now := time.Date(2026, 10, 11, 23, 10, 0, 0, time.UTC)
	all := []Trade{
		trade("a-one", "MSFT", "2026-10-01", "2026-10-05"),
		trade("b-two", "NVDA", "2026-09-20", "2026-10-02"),
		trade("c-three", "AAPL", "2026-08-15", "2026-09-01"),
	}
	srv, _ := fake(t, all, 0, "k")
	c := &Client{BaseURL: srv.URL, Key: "k", HTTP: srv.Client()}

	res, err := Sync(ctx, st.DB, c, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fetched != 3 || res.New != 3 {
		t.Fatalf("first sync: %+v", res)
	}
	// Second run: nothing new, nothing duplicated.
	res, err = Sync(ctx, st.DB, c, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 0 {
		t.Fatalf("re-sync should add nothing, got %+v", res)
	}
	var n int
	st.DB.QueryRow(`SELECT COUNT(*) FROM congress_trades_raw`).Scan(&n)
	if n != 3 {
		t.Fatalf("rows = %d, want 3", n)
	}
	// History survives the source dropping a trade (aged out of Bargo's window).
	srv2, _ := fake(t, all[:1], 0, "k")
	c2 := &Client{BaseURL: srv2.URL, Key: "k", HTTP: srv2.Client()}
	if _, err := Sync(ctx, st.DB, c2, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	st.DB.QueryRow(`SELECT COUNT(*) FROM congress_trades_raw`).Scan(&n)
	if n != 3 {
		t.Fatalf("history lost: rows = %d, want 3", n)
	}
	var status string
	st.DB.QueryRow(`SELECT status FROM bargo_sync_runs ORDER BY id DESC LIMIT 1`).Scan(&status)
	if status != "ok" {
		t.Fatalf("run status %q", status)
	}
}

func TestSyncAuthFailureAndQuota(t *testing.T) {
	ctx := context.Background()
	st := openDB(t)
	now := time.Date(2026, 10, 11, 23, 10, 0, 0, time.UTC)

	// Wrong key → failed run, no data.
	srv, _ := fake(t, nil, 0, "right")
	bad := &Client{BaseURL: srv.URL, Key: "wrong", HTTP: srv.Client()}
	if _, err := Sync(ctx, st.DB, bad, now); err != ErrAuth {
		t.Fatalf("want ErrAuth, got %v", err)
	}
	var status string
	st.DB.QueryRow(`SELECT status FROM bargo_sync_runs ORDER BY id DESC LIMIT 1`).Scan(&status)
	if status != "failed" {
		t.Fatalf("status %q, want failed", status)
	}

	// Quota after the first request, with data stored → partial, and the
	// unfinished backfill cursor is kept so the next run resumes.
	var all []Trade
	for i := 0; i < 5; i++ {
		all = append(all, trade("m", "T"+string(rune('A'+i)), now.AddDate(0, 0, -80+i).Format("2006-01-02"), "2026-10-01"))
	}
	srv2, _ := fake(t, all, 1, "")
	c := &Client{BaseURL: srv2.URL, HTTP: srv2.Client()}
	res, err := Sync(ctx, st.DB, c, now)
	if err != nil {
		t.Fatalf("quota after data should not be an error: %v", err)
	}
	if !res.Partial {
		t.Fatalf("expected partial, got %+v", res)
	}
	var cur string
	st.DB.QueryRow(`SELECT value FROM meta WHERE key='bargo_cursor'`).Scan(&cur)
	if cur == "" {
		t.Fatal("cursor not saved after partial run")
	}
}

func TestSourceKeyStable(t *testing.T) {
	a := trade("x", "msft", "2026-10-01", "2026-10-05")
	b := trade("x", "MSFT", "2026-10-01", "2026-10-05")
	if SourceKey(a) != SourceKey(b) {
		t.Fatal("ticker case must not change the key")
	}
	b.AmountRange = "$15,001 - $50,000"
	if SourceKey(a) == SourceKey(b) {
		t.Fatal("different amount bands must differ")
	}
}
