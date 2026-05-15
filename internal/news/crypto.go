package news

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// FetchCryptoNews pulls public posts from CryptoPanic. Returns (nil, nil) if
// the API key is missing.
//
// CryptoPanic's votes block gives us native sentiment: more positives than
// negatives → positive; opposite → negative; otherwise neutral.
func FetchCryptoNews(ctx context.Context) ([]Article, error) {
	key := os.Getenv("CRYPTOPANIC_API_KEY")
	if key == "" {
		return nil, nil
	}

	u, _ := url.Parse("https://cryptopanic.com/api/v1/posts/")
	q := u.Query()
	q.Set("auth_token", key)
	q.Set("public", "true")
	q.Set("kind", "news")
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return nil, fmt.Errorf("cryptopanic http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Results []struct {
			Title       string `json:"title"`
			PublishedAt string `json:"published_at"`
			URL         string `json:"url"`
			Domain      string `json:"domain"`
			Source      struct {
				Title  string `json:"title"`
				Domain string `json:"domain"`
			} `json:"source"`
			Votes struct {
				Negative   int `json:"negative"`
				Positive   int `json:"positive"`
				Important  int `json:"important"`
				Liked      int `json:"liked"`
				Disliked   int `json:"disliked"`
				LoL        int `json:"lol"`
				Toxic      int `json:"toxic"`
				Saved      int `json:"saved"`
				Comments   int `json:"comments"`
			} `json:"votes"`
		} `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := make([]Article, 0, len(payload.Results))
	for _, p := range payload.Results {
		if p.Title == "" || p.URL == "" {
			continue
		}
		t, _ := time.Parse(time.RFC3339, p.PublishedAt)
		sentiment := "neutral"
		if p.Votes.Positive > p.Votes.Negative {
			sentiment = "positive"
		} else if p.Votes.Negative > p.Votes.Positive {
			sentiment = "negative"
		}
		src := p.Source.Title
		if src == "" {
			src = p.Source.Domain
		}
		if src == "" {
			src = p.Domain
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
