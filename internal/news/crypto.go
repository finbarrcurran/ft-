package news

import (
	"context"
	"encoding/xml"
	"fmt"
	"ft/internal/health"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// FetchCryptoNews aggregates crypto news directly from publisher RSS feeds
// (v1.9.3 — replaces both the deprecated CryptoPanic and the now-gated
// CryptoCompare APIs). Direct RSS removes every third-party dependency:
// no API keys, no rate limits, no "free tier discontinued 2026-04-01"
// rugs. The publishers themselves publish RSS and want it crawled.
//
// Sources (RSS 2.0):
//   - CoinDesk:      https://www.coindesk.com/arc/outboundfeeds/rss/
//   - Cointelegraph: https://cointelegraph.com/rss
//   - The Block:     https://www.theblock.co/rss.xml
//   - Decrypt:       https://decrypt.co/feed
//
// Failures on a single feed are logged but don't abort the aggregate —
// the others' articles still flow. Deduped by URL across all feeds.
// Sorted newest-first. Capped at 50 articles per request.
//
// Sentiment is "neutral" for every article — RSS doesn't carry sentiment
// data and the UI already handles neutral as the default.

// cryptoFeeds is the source list. Adding a feed = appending a line.
// Removing a noisy publisher = deleting a line. Format / structure is
// generic RSS 2.0 so no per-publisher parsing.
var cryptoFeeds = []struct {
	Name string
	URL  string
}{
	{"CoinDesk", "https://www.coindesk.com/arc/outboundfeeds/rss/"},
	{"Cointelegraph", "https://cointelegraph.com/rss"},
	{"The Block", "https://www.theblock.co/rss.xml"},
	{"Decrypt", "https://decrypt.co/feed"},
}

// rssFeed mirrors enough of the RSS 2.0 schema to extract item titles,
// links, and pubDates. Other fields (description, guid, media:content,
// etc.) are ignored.
type rssFeed struct {
	XMLName xml.Name  `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}
type rssChannel struct {
	Items []rssItem `xml:"item"`
}
type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
}

// pubDateFormats covers the variants RSS feeds actually emit.
var pubDateFormats = []string{
	time.RFC1123Z,                          // "Mon, 02 Jan 2006 15:04:05 -0700"
	time.RFC1123,                           // "Mon, 02 Jan 2006 15:04:05 MST"
	"Mon, 2 Jan 2006 15:04:05 -0700",       // non-zero-padded day
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2006-01-02T15:04:05Z07:00",            // RFC 3339 variants some feeds use
	time.RFC3339,
}

func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, f := range pubDateFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{} // zero — sorts to end
}

// FetchCryptoNews returns the merged feed. Always returns a non-nil
// slice (possibly empty) — never nil — so the handler can distinguish
// "providers returned zero items" from "missing API key" (the latter
// triggers the UI's `unconfigured` banner). RSS providers never go
// unconfigured.
func FetchCryptoNews(ctx context.Context) (articles []Article, retErr error) {
	defer func() {
		health.Record(ctx, "rss_crypto", retErr)
	}()

	client := &http.Client{
		Timeout: 12 * time.Second,
		// Default redirect policy follows up to 10 hops — fine for
		// CoinDesk's outboundfeeds redirect.
	}

	// Always return a non-nil slice (see godoc note above).
	out := []Article{}
	seen := map[string]bool{}
	var firstErr error

	for _, feed := range cryptoFeeds {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		items, err := fetchRSSItems(ctx, client, feed.URL)
		if err != nil {
			// One bad feed doesn't kill the aggregate.
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", feed.Name, err)
			}
			continue
		}
		for _, it := range items {
			url := strings.TrimSpace(it.Link)
			title := strings.TrimSpace(it.Title)
			if url == "" || title == "" || seen[url] {
				continue
			}
			seen[url] = true
			out = append(out, Article{
				Title:       title,
				URL:         url,
				Source:      feed.Name,
				PublishedAt: parsePubDate(it.PubDate),
				Sentiment:   "neutral",
			})
		}
	}

	// Sort newest-first; cap at 50.
	sort.Slice(out, func(i, j int) bool {
		return out[i].PublishedAt.After(out[j].PublishedAt)
	})
	if len(out) > 50 {
		out = out[:50]
	}

	// If we got at least one article, swallow the partial error — we have
	// something to show. Surface the first error only when nothing came
	// back, so the cache logic in the handler still treats us as "tried
	// and failed" rather than "unconfigured".
	if len(out) > 0 {
		return out, nil
	}
	if firstErr != nil {
		return out, firstErr
	}
	return out, nil
}

func fetchRSSItems(ctx context.Context, client *http.Client, url string) ([]rssItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Some publisher CDNs (CoinDesk's Arc) reject default Go UA / serve
	// 308 redirects. A real-browser UA + redirect-follow handles both.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; FT-Dashboard/1.0; +https://ft.curranhouse.dev)")
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse rss: %w", err)
	}
	return feed.Channel.Items, nil
}
