// Package news has adapters for NewsAPI (market headlines) and CryptoPanic
// (crypto headlines), plus a small JSON-serialised cache layer in the
// news_cache SQLite table.
package news

import "time"

// Article is the normalized news item the frontend renders. Source-agnostic
// fields chosen to fit both NewsAPI's `Article` and CryptoPanic's `Post`.
type Article struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
	Summary     string    `json:"summary,omitempty"`
	Sentiment   string    `json:"sentiment,omitempty"` // "positive" / "negative" / "neutral"
	Image       string    `json:"image,omitempty"`
}

// Feed is what /api/news/{market,crypto} returns.
type Feed struct {
	Articles  []Article `json:"articles"`
	Source    string    `json:"source"`    // "newsapi" / "cryptopanic" / "seed"
	FetchedAt time.Time `json:"fetchedAt"`
	Cached    bool      `json:"cached"`
}
