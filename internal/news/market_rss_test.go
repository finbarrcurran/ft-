package news

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

// feedXML builds an RSS document with n items (distinct titles/URLs per prefix),
// each published ageH hours before testNow.
func feedXML(prefix string, n int, ageH float64, desc string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><rss version="2.0"><channel>`)
	for i := 0; i < n; i++ {
		pub := testNow.Add(-time.Duration(ageH*float64(time.Hour)) - time.Duration(i)*time.Minute).Format(time.RFC1123Z)
		d := ""
		if desc != "" {
			d = "<description><![CDATA[" + desc + "]]></description>"
		}
		fmt.Fprintf(&b, `<item><title>%s story %d</title><link>https://%s.example/%d</link><pubDate>%s</pubDate>%s</item>`, prefix, i, prefix, i, pub, d)
	}
	b.WriteString(`</channel></rss>`)
	return b.String()
}

func srv(t *testing.T, body string, code int) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func sources(arts []Article) map[string]int {
	m := map[string]int{}
	for _, a := range arts {
		m[a.Source]++
	}
	return m
}

func TestTierBHiddenWhenTierAHealthyAndFresh(t *testing.T) {
	a1, a2, a3 := srv(t, feedXML("a1", 8, 1, ""), 200), srv(t, feedXML("a2", 8, 2, ""), 200), srv(t, feedXML("a3", 8, 3, ""), 200)
	b1 := srv(t, feedXML("b1", 8, 1, ""), 200)
	feeds := []marketFeed{{"a1", "A1", a1.URL, "A"}, {"a2", "A2", a2.URL, "A"}, {"a3", "A3", a3.URL, "A"}, {"b1", "B1", b1.URL, "B"}}
	arts, err := fetchMarketRSS(context.Background(), feeds, nil, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := sources(arts); got["B1"] != 0 || got["A1"] == 0 || got["A2"] == 0 || got["A3"] == 0 {
		t.Fatalf("tier B must be fetched for health but not displayed when tier A is healthy and fresh: %v", got)
	}
}

func TestDeadFeedNeverBlanksTheTabAndTierBStepsIn(t *testing.T) {
	a1 := srv(t, feedXML("a1", 12, 1, ""), 200)
	dead1, dead2 := srv(t, "boom", 500), srv(t, "boom", 503)
	b1 := srv(t, feedXML("b1", 6, 1, ""), 200)
	feeds := []marketFeed{{"a1", "A1", a1.URL, "A"}, {"a2", "A2", dead1.URL, "A"}, {"a3", "A3", dead2.URL, "A"}, {"b1", "B1", b1.URL, "B"}}
	arts, err := fetchMarketRSS(context.Background(), feeds, nil, testNow)
	if err != nil {
		t.Fatalf("dead feeds must not fail the tab: %v", err)
	}
	got := sources(arts)
	if got["A1"] == 0 {
		t.Fatalf("the healthy feed must still render: %v", got)
	}
	if got["B1"] == 0 {
		t.Fatalf("with only one healthy tier-A feed, tier B must be displayed: %v", got)
	}
}

func TestTierBShownWhenTooFewFreshItems(t *testing.T) {
	// three healthy tier-A feeds, but only 5 items under 24 h in total -> tier B steps in
	a1, a2, a3 := srv(t, feedXML("a1", 2, 30, ""), 200), srv(t, feedXML("a2", 3, 1, ""), 200), srv(t, feedXML("a3", 2, 1, ""), 200)
	b1 := srv(t, feedXML("b1", 4, 1, ""), 200)
	feeds := []marketFeed{{"a1", "A1", a1.URL, "A"}, {"a2", "A2", a2.URL, "A"}, {"a3", "A3", a3.URL, "A"}, {"b1", "B1", b1.URL, "B"}}
	arts, _ := fetchMarketRSS(context.Background(), feeds, nil, testNow)
	if sources(arts)["B1"] == 0 {
		t.Fatalf("fewer than 10 fresh tier-A items must bring tier B in: %v", sources(arts))
	}
}

func TestDedupeByTitleAcrossFeedsAndDisabled(t *testing.T) {
	same := `<?xml version="1.0"?><rss version="2.0"><channel><item><title>Fed holds rates steady!</title><link>https://one.example/x</link><pubDate>` + testNow.Add(-time.Hour).Format(time.RFC1123Z) + `</pubDate></item></channel></rss>`
	same2 := strings.ReplaceAll(strings.ReplaceAll(same, "one.example", "two.example"), "Fed holds rates steady!", "FED HOLDS RATES STEADY")
	a1, a2 := srv(t, same, 200), srv(t, same2, 200)
	feeds := []marketFeed{{"a1", "A1", a1.URL, "A"}, {"a2", "A2", a2.URL, "A"}}
	arts, _ := fetchMarketRSS(context.Background(), feeds, nil, testNow)
	if len(arts) != 1 {
		t.Fatalf("same headline from two publishers (different URLs, different case/punctuation) must dedupe, got %d", len(arts))
	}
	arts, _ = fetchMarketRSS(context.Background(), feeds, map[string]bool{"a1": true}, testNow)
	if len(arts) != 1 || arts[0].Source != "A2" {
		t.Fatalf("a disabled feed must be skipped: %+v", arts)
	}
}

func TestSnippetOnlyWhenFeedSuppliesOne(t *testing.T) {
	long := "<p>" + strings.Repeat("Markets moved sharply on the news. ", 20) + "</p>"
	a1, a2 := srv(t, feedXML("a1", 3, 1, long), 200), srv(t, feedXML("a2", 3, 1, ""), 200)
	arts, _ := fetchMarketRSS(context.Background(), []marketFeed{{"a1", "A1", a1.URL, "A"}, {"a2", "A2", a2.URL, "A"}}, nil, testNow)
	for _, a := range arts {
		switch a.Source {
		case "A1":
			if a.Summary == "" || strings.Contains(a.Summary, "<p>") || len([]rune(a.Summary)) > 205 {
				t.Fatalf("snippet must be plain text, ≤ ~200 chars: %q", a.Summary)
			}
		case "A2":
			if a.Summary != "" {
				t.Fatalf("no summary must be invented when the feed has none: %q", a.Summary)
			}
		}
	}
}

func TestAllFeedsDeadSurfacesError(t *testing.T) {
	d := srv(t, "boom", 500)
	if _, err := fetchMarketRSS(context.Background(), []marketFeed{{"a1", "A1", d.URL, "A"}}, nil, testNow); err == nil {
		t.Fatal("when every feed fails the error must surface (the handler then serves the stale cache)")
	}
}
