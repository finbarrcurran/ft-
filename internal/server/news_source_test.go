package server

import "testing"

// SC-52 P6: NewsAPI is a fallback that runs only when explicitly selected; the default is RSS.
func TestMarketNewsSourceDefaultsToRSS(t *testing.T) {
	for in, want := range map[string]string{"": "rss", "rss": "rss", "RSS": "rss", "garbage": "rss", "newsapi": "newsapi", " NewsAPI ": "newsapi"} {
		if got := marketNewsSource(in); got != want {
			t.Errorf("setting %q: got %s, want %s", in, got, want)
		}
	}
}
