package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Nasdaq's CDN resets connections whose User-Agent contains a URL; the RSS fetcher
// must send a plain self-identifying UA (no URL, still names FT).
func TestRSSUserAgentIsPlainAndIdentifiesFT(t *testing.T) {
	var got string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(feedXML("ua", 1, 1, "")))
	}))
	defer s.Close()
	if _, err := fetchRSSItems(context.Background(), s.Client(), s.URL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "FT-Dashboard") || strings.Contains(got, "http") {
		t.Fatalf("unexpected User-Agent %q", got)
	}
}
