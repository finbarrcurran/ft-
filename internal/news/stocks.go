package news

import (
	"context"
	"encoding/json"
	"fmt"
	"ft/internal/health"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Heuristic sentiment word lists. Mirrors the prototype's word-list approach
// in lib/news-feeds/stocks.ts.
var (
	positiveWords = []string{
		"gain", "gains", "rally", "rallies", "surge", "surges", "soar", "rises",
		"jumps", "boost", "strong", "beat", "beats", "record", "high", "highs",
		"breakthrough", "upgrade", "growth", "expansion", "approved",
	}
	negativeWords = []string{
		"loss", "losses", "fall", "falls", "drop", "drops", "plunge", "plunges",
		"sink", "tumble", "tumbles", "weak", "miss", "misses", "low", "lows",
		"warning", "downgrade", "decline", "concerns", "rejected", "lawsuit",
	}
)

// FetchMarketNews pulls top business headlines from NewsAPI. If the API key
// is missing the function returns (nil, nil) — the caller decides what to do
// (fall back to seed / cache).
func FetchMarketNews(ctx context.Context) (articles []Article, retErr error) {
	defer func() {
		// Only record if we actually attempted a call (key present).
		if os.Getenv("NEWSAPI_API_KEY") != "" {
			health.Record(ctx, "newsapi", retErr)
		}
	}()
	key := os.Getenv("NEWSAPI_API_KEY")
	if key == "" {
		return nil, nil
	}

	u, _ := url.Parse("https://newsapi.org/v2/top-headlines")
	q := u.Query()
	q.Set("category", "business")
	q.Set("country", "us")
	q.Set("pageSize", "20")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("X-Api-Key", key)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return nil, fmt.Errorf("newsapi http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Status   string `json:"status"`
		Articles []struct {
			Source      struct{ Name string } `json:"source"`
			Title       string                `json:"title"`
			Description string                `json:"description"`
			URL         string                `json:"url"`
			URLToImage  string                `json:"urlToImage"`
			PublishedAt string                `json:"publishedAt"`
		} `json:"articles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]Article, 0, len(payload.Articles))
	for _, a := range payload.Articles {
		if a.Title == "" || a.URL == "" {
			continue
		}
		t, _ := time.Parse(time.RFC3339, a.PublishedAt)
		out = append(out, Article{
			Title:       a.Title,
			URL:         a.URL,
			Source:      a.Source.Name,
			PublishedAt: t,
			Summary:     a.Description,
			Sentiment:   heuristicSentiment(a.Title + " " + a.Description),
			Image:       a.URLToImage,
		})
	}
	return out, nil
}

// heuristicSentiment classifies a string by counting positive/negative words.
func heuristicSentiment(s string) string {
	s = strings.ToLower(s)
	pos, neg := 0, 0
	for _, w := range positiveWords {
		if strings.Contains(s, w) {
			pos++
		}
	}
	for _, w := range negativeWords {
		if strings.Contains(s, w) {
			neg++
		}
	}
	switch {
	case pos > neg:
		return "positive"
	case neg > pos:
		return "negative"
	default:
		return "neutral"
	}
}
