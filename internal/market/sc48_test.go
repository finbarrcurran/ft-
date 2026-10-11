package market

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func resetFinnhub() {
	finnhubCacheMu.Lock()
	finnhubCache = map[string]finnhubCacheEntry{}
	finnhubCacheMu.Unlock()
	finnhubGate.mu.Lock()
	finnhubGate.next, finnhubGate.blocked = time.Time{}, time.Time{}
	finnhubGate.mu.Unlock()
}

func TestFinnhubSkipsNonUSAndCaches(t *testing.T) {
	resetFinnhub()
	t.Setenv("FT_FINNHUB_API_KEY", "test-key")
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Ratelimit-Limit", "60")
		w.Header().Set("X-Ratelimit-Remaining", "50")
		_, _ = w.Write([]byte(`{"c":123.45,"dp":1.5}`))
	}))
	defer srv.Close()
	old := finnhubBase
	finnhubBase = srv.URL
	defer func() { finnhubBase = old }()
	finnhubGate.minGap = time.Millisecond
	defer func() { finnhubGate.minGap = finnhubMinGap }()

	ctx := context.Background()
	if _, err := fetchFinnhub(ctx, "RR.L"); err != errFinnhubSkipped {
		t.Fatalf("non-US ticker must be skipped, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("a skipped ticker must not call Finnhub")
	}
	q, err := fetchFinnhub(ctx, "AAPL")
	if err != nil || q.Price != 123.45 {
		t.Fatalf("quote: %v %v", q, err)
	}
	if _, err := fetchFinnhub(ctx, "AAPL"); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("second request within the TTL must come from the cache, calls=%d", calls)
	}
	if hr := FinnhubHeadroom(); hr == nil || hr.Limit != 60 || hr.Remaining != 50 {
		t.Fatalf("headroom not recorded: %+v", hr)
	}
}

func TestFinnhub429BacksOff(t *testing.T) {
	resetFinnhub()
	t.Setenv("FT_FINNHUB_API_KEY", "test-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	old := finnhubBase
	finnhubBase = srv.URL
	defer func() { finnhubBase = old }()
	finnhubGate.minGap = time.Millisecond
	defer func() { finnhubGate.minGap = finnhubMinGap }()

	_, err := fetchFinnhub(context.Background(), "MSFT")
	if err == nil {
		t.Fatal("expected a 429 error")
	}
	finnhubGate.mu.Lock()
	blockedFor := time.Until(finnhubGate.blocked)
	finnhubGate.mu.Unlock()
	if blockedFor < 20*time.Second {
		t.Fatalf("a 429 must hold the gate (backoff), blocked for only %v", blockedFor)
	}
}

func TestRateGateSpacesCalls(t *testing.T) {
	g := &rateGate{minGap: 60 * time.Millisecond}
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 4; i++ {
		if err := g.wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 170*time.Millisecond {
		t.Fatalf("4 calls through a 60 ms gate took only %v", el)
	}
	ctx2, cancel := context.WithCancel(ctx)
	cancel()
	g.penalize(time.Hour)
	if err := g.wait(ctx2); err == nil {
		t.Fatal("a cancelled context must abort the wait")
	}
}

func TestCNNStyleHeadersAreSent(t *testing.T) {
	var gotRef, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRef, gotOrigin = r.Header.Get("Referer"), r.Header.Get("Origin")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	var out map[string]any
	if err := httpGetJSONHeaders(context.Background(), srv.URL, map[string]string{"Referer": "https://edition.cnn.com/", "Origin": "https://edition.cnn.com"}, &out); err != nil {
		t.Fatal(err)
	}
	if gotRef != "https://edition.cnn.com/" || gotOrigin != "https://edition.cnn.com" {
		t.Fatalf("headers not sent: %q %q", gotRef, gotOrigin)
	}
}
