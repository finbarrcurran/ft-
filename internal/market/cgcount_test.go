package market

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ft/internal/health"
)

type countRec struct {
	mu    sync.Mutex
	calls map[string]int
	fails map[string]int
}

func (c *countRec) RecordProviderHealth(_ context.Context, p string, ok bool, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[p]++
	if !ok {
		c.fails[p]++
	}
	return nil
}

func TestCGGetJSONCountsKeyedAndKeylessAndRetries(t *testing.T) {
	rec := &countRec{calls: map[string]int{}, fails: map[string]int{}}
	health.Init(rec)
	defer health.Init(nil)
	cgBackoffUnit = time.Millisecond
	var hits int
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotKey = r.Header.Get("x-cg-demo-api-key")
		if hits == 1 {
			http.Error(w, "rate", 429)
			return
		}
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()
	var out map[string]int

	// keyless: one call, counted under coingecko, no header
	hits = 0
	if err := cgGetJSON(context.Background(), srv.URL, false, 0, &out); err == nil {
		t.Fatal("first hit is a 429 and there are no retries: want error")
	}
	if rec.calls[ProviderCoinGecko] != 1 || rec.fails[ProviderCoinGecko] != 1 || gotKey != "" {
		t.Fatalf("keyless: %v key=%q", rec.calls, gotKey)
	}

	// keyed with one retry: two calls (429 then 200), both under coingecko_key, header sent
	t.Setenv("FT_COINGECKO_API_KEY", "demo-key")
	hits = 0
	if err := cgGetJSON(context.Background(), srv.URL, true, 1, &out); err != nil || out["a"] != 1 {
		t.Fatalf("keyed retry: %v %v", err, out)
	}
	if rec.calls[ProviderCoinGeckoKey] != 2 || rec.fails[ProviderCoinGeckoKey] != 1 || gotKey != "demo-key" {
		t.Fatalf("keyed: %v key=%q", rec.calls, gotKey)
	}

	// keyed requested but no key configured: falls back to keyless counting
	t.Setenv("FT_COINGECKO_API_KEY", "")
	hits = 1
	_ = cgGetJSON(context.Background(), srv.URL, true, 0, &out)
	if rec.calls[ProviderCoinGeckoKey] != 2 || rec.calls[ProviderCoinGecko] != 2 {
		t.Fatalf("no key: %v", rec.calls)
	}
}
