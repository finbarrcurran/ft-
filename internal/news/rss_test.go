package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const feedA = `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>Stocks rally as earnings beat</title><link>https://a.example/1</link><pubDate>Mon, 12 Oct 2026 10:00:00 +0000</pubDate></item>
<item><title>Shared story</title><link>https://shared.example/x</link><pubDate>Mon, 12 Oct 2026 09:00:00 +0000</pubDate></item></channel></rss>`
const feedB = `<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>Shared story</title><link>https://shared.example/x</link><pubDate>Mon, 12 Oct 2026 09:00:00 +0000</pubDate></item>
<item><title>Oil falls on demand concerns</title><link>https://b.example/2</link><pubDate>Mon, 12 Oct 2026 11:00:00 +0000</pubDate></item></channel></rss>`

func TestRSSAggregateDedupeIsolationAttribution(t *testing.T) {
	mk := func(body string, code int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
	}
	a, b, bad := mk(feedA, 200), mk(feedB, 200), mk("nope", 500)
	defer a.Close()
	defer b.Close()
	defer bad.Close()
	feeds := []rssSource{{"A", a.URL}, {"Broken", bad.URL}, {"B", b.URL}}
	arts, err := fetchRSSAggregate(context.Background(), "rss_test", feeds, true)
	if err != nil {
		t.Fatalf("one broken feed must not fail the aggregate: %v", err)
	}
	if len(arts) != 3 {
		t.Fatalf("want 3 articles after dedupe by URL, got %d: %+v", len(arts), arts)
	}
	if arts[0].Title != "Oil falls on demand concerns" || arts[0].Source != "B" {
		t.Fatalf("newest first with source attribution expected, got %+v", arts[0])
	}
	var rally *Article
	for i := range arts {
		if arts[i].Title == "Stocks rally as earnings beat" {
			rally = &arts[i]
		}
	}
	if rally == nil || rally.Source != "A" || rally.Sentiment == "neutral" {
		t.Fatalf("headline sentiment/attribution wrong: %+v", rally)
	}
	// All feeds broken -> an error, not an empty success.
	if _, err := fetchRSSAggregate(context.Background(), "rss_test", []rssSource{{"Broken", bad.URL}}, true); err == nil {
		t.Fatal("when nothing comes back the error must surface")
	}
}
