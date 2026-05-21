package news

import (
	"context"
	"encoding/json"
	"fmt"
	"ft/internal/health"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FetchCryptoNews pulls aggregated crypto news from CryptoCompare's free
// News API (v1.9.2 — replaces the discontinued CryptoPanic free tier).
//
// Endpoint: https://min-api.cryptocompare.com/data/v2/news/?lang=EN
//
// No API key required for basic use; free tier is generous (~100k req/month
// — FT's news cache hits this once per refresh cycle, well under limit).
// Returns aggregated headlines from CoinDesk, Cointelegraph, The Block,
// Decrypt, BeInCrypto, Bitcoin Magazine, etc. — the same publisher set
// CryptoPanic was previously sourcing from.
//
// Sentiment: CryptoCompare exposes upvotes/downvotes per article. When
// neither is set (most articles in the free feed), sentiment falls back
// to "neutral". UI already handles the neutral case, so this is a clean
// downgrade from CryptoPanic's richer sentiment data.
func FetchCryptoNews(ctx context.Context) (articles []Article, retErr error) {
	defer func() {
		health.Record(ctx, "cryptocompare", retErr)
	}()

	const endpoint = "https://min-api.cryptocompare.com/data/v2/news/?lang=EN"

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "application/json")
	// CryptoCompare blocks generic Go default UAs; identify politely.
	req.Header.Set("User-Agent", "FT-Dashboard fin@curranhouse.dev")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return nil, fmt.Errorf("cryptocompare http %d: %s",
			res.StatusCode, strings.TrimSpace(string(body)))
	}

	// CryptoCompare wraps the array in {Type, Message, Data}.
	var payload struct {
		Type    int    `json:"Type"`
		Message string `json:"Message"`
		Data    []struct {
			ID          string `json:"id"`
			PublishedOn int64  `json:"published_on"` // unix seconds
			Title       string `json:"title"`
			URL         string `json:"url"`
			Body        string `json:"body"`
			Tags        string `json:"tags"`
			Categories  string `json:"categories"`
			Upvotes     string `json:"upvotes"`   // strings in their JSON
			Downvotes   string `json:"downvotes"`
			Source      string `json:"source"`    // slug, e.g. "coindesk"
			SourceInfo  struct {
				Name string `json:"name"` // display name, e.g. "CoinDesk"
				Lang string `json:"lang"`
			} `json:"source_info"`
		} `json:"Data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]Article, 0, len(payload.Data))
	for _, p := range payload.Data {
		if p.Title == "" || p.URL == "" {
			continue
		}
		t := time.Unix(p.PublishedOn, 0).UTC()
		// Derive sentiment from upvotes vs downvotes. Most articles in the
		// free feed have 0/0 → neutral. Strings in the JSON because their
		// API is quirky.
		up, _ := strconv.Atoi(p.Upvotes)
		down, _ := strconv.Atoi(p.Downvotes)
		sentiment := "neutral"
		if up > down {
			sentiment = "positive"
		} else if down > up {
			sentiment = "negative"
		}
		src := p.SourceInfo.Name
		if src == "" {
			src = p.Source
		}
		out = append(out, Article{
			Title:       p.Title,
			URL:         p.URL,
			Source:      src,
			PublishedAt: t,
			Sentiment:   sentiment,
		})
	}
	return out, nil
}
