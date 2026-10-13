package news

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"ft/internal/health"
)

// Market-news RSS (SC-48 / work order 2026-10-13). Free publisher feeds, no key or account.
//
//   - Tier A are always displayed. Tier B are fetched on every run (so each one's health is
//     measured and the daily warm-up exercises it) but only DISPLAYED when fewer than 2 tier-A
//     feeds are healthy, or fewer than 10 tier-A items are under 24 h old.
//   - One failing feed never blanks the tab: each feed is fetched, recorded and judged alone.
//   - FT shows headline, source name, time and a link out, plus the feed's own summary snippet
//     when the feed supplies one (HTML stripped, cut at ~200 characters). No article body is
//     fetched or stored.
type marketFeed struct {
	Key  string // provider_health suffix: rss_market_<key>
	Name string // attribution shown on every headline
	URL  string
	Tier string // "A" or "B"
}

var marketFeeds = []marketFeed{
	{"cnbc", "CNBC", "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114", "A"},
	{"bbc", "BBC Business", "https://feeds.bbci.co.uk/news/business/rss.xml", "A"},
	{"nasdaq", "Nasdaq", "https://www.nasdaq.com/feed/rssoutbound?category=Markets", "A"},
	{"marketwatch", "MarketWatch", "https://feeds.content.dowjones.io/public/rss/mw_topstories", "B"},
	{"guardian", "The Guardian", "https://www.theguardian.com/uk/business/rss", "B"},
}

// MarketFeedKeys lists the feed keys (for the monitor and the disable switch).
func MarketFeedKeys() []string {
	out := make([]string, 0, len(marketFeeds))
	for _, f := range marketFeeds {
		out = append(out, f.Key)
	}
	return out
}

// MarketFeedInfo describes one feed for the monitor.
type MarketFeedInfo struct{ Key, Name, Tier string }

// MarketFeedList returns key/name/tier for every market feed.
func MarketFeedList() []MarketFeedInfo {
	out := make([]MarketFeedInfo, 0, len(marketFeeds))
	for _, f := range marketFeeds {
		out = append(out, MarketFeedInfo{f.Key, f.Name, f.Tier})
	}
	return out
}

// FetchMarketNewsRSS fetches all enabled market feeds and returns the displayed articles.
// `disabled` holds feed keys switched off (Fin can drop a publisher without a deploy).
func FetchMarketNewsRSS(ctx context.Context, disabled map[string]bool) ([]Article, error) {
	return fetchMarketRSS(ctx, marketFeeds, disabled, time.Now())
}

var tagRe = regexp.MustCompile(`(?s)<[^>]*>`)

func snippet(desc string) string {
	s := html.UnescapeString(tagRe.ReplaceAllString(desc, " "))
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	const max = 200
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)[:max]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > 100 {
		cut = cut[:i]
	}
	return cut + "…"
}

func titleKey(t string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(t) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fetchMarketRSS(ctx context.Context, feeds []marketFeed, disabled map[string]bool, now time.Time) ([]Article, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	type result struct {
		feed  marketFeed
		items []Article
		err   error
	}
	var results []result
	for _, f := range feeds {
		if disabled[f.Key] {
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		items, err := fetchRSSItems(ctx, client, f.URL)
		health.Record(ctx, "rss_market_"+f.Key, err) // per-feed health: one alert per outage names the feed
		r := result{feed: f, err: err}
		if err == nil {
			for _, it := range items {
				title, link := strings.TrimSpace(it.Title), strings.TrimSpace(it.Link)
				if title == "" || link == "" {
					continue
				}
				r.items = append(r.items, Article{
					Title: title, URL: link, Source: f.Name,
					PublishedAt: parsePubDate(it.PubDate),
					Summary:     snippet(it.Description),
					Sentiment:   heuristicSentiment(title),
				})
			}
		}
		results = append(results, r)
	}

	// Decide whether tier B is displayed.
	healthyA, freshA := 0, 0
	cutoff := now.Add(-24 * time.Hour)
	for _, r := range results {
		if r.feed.Tier == "A" && r.err == nil && len(r.items) > 0 {
			healthyA++
			for _, a := range r.items {
				if a.PublishedAt.After(cutoff) {
					freshA++
				}
			}
		}
	}
	showB := healthyA < 2 || freshA < 10

	out := []Article{}
	seenURL, seenTitle := map[string]bool{}, map[string]bool{}
	var firstErr error
	for _, r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", r.feed.Name, r.err)
		}
		if r.feed.Tier == "B" && !showB {
			continue
		}
		for _, a := range r.items {
			k := titleKey(a.Title)
			if seenURL[a.URL] || (k != "" && seenTitle[k]) {
				continue
			}
			seenURL[a.URL] = true
			seenTitle[k] = true
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
	if len(out) > 50 {
		out = out[:50]
	}
	var retErr error
	if len(out) == 0 && firstErr != nil {
		retErr = firstErr
	}
	health.Record(ctx, "rss_market", retErr)
	return out, retErr
}
