package server

import (
	"context"
	"encoding/json"
	"errors"
	"ft/internal/market"
	"ft/internal/news"
	"ft/internal/store"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const newsTTL = 60 * time.Minute

// GET /api/news/market
// GET /api/news/crypto
//
// Strategy:
//   1. If we have a cache row younger than TTL, return it.
//   2. Otherwise try the live adapter. If it returns articles, persist + return.
//   3. If the live call fails AND we have a stale cache, return the stale cache.
//   4. If everything fails, return an empty feed with source="unconfigured".
func (s *Server) handleMarketNews(w http.ResponseWriter, r *http.Request) {
	s.handleNews(w, r, "market", "newsapi", news.FetchMarketNews)
}
func (s *Server) handleCryptoNews(w http.ResponseWriter, r *http.Request) {
	s.handleNews(w, r, "crypto", "cryptopanic", news.FetchCryptoNews)
}

func (s *Server) handleNews(
	w http.ResponseWriter,
	r *http.Request,
	scope, sourceTag string,
	fetcher func(ctx context.Context) ([]news.Article, error),
) {
	// Cache check.
	if payload, fetchedAt, err := s.store.GetNewsCache(r.Context(), scope); err == nil {
		if time.Since(fetchedAt) < newsTTL {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		slog.Warn("news cache read", "scope", scope, "err", err)
	}

	articles, err := fetcher(r.Context())
	if err != nil {
		slog.Warn("news fetch failed", "scope", scope, "err", err)
		// Stale fallback if we have one.
		if payload, _, cerr := s.store.GetNewsCache(r.Context(), scope); cerr == nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
	}

	feed := news.Feed{
		Articles:  articles,
		FetchedAt: time.Now().UTC(),
		Source:    sourceTag,
	}
	if articles == nil {
		// API key missing — nil signals "unconfigured" so the UI can show a hint.
		feed.Source = "unconfigured"
	}
	body, _ := json.Marshal(feed)
	if len(articles) > 0 {
		_ = s.store.SetNewsCache(r.Context(), scope, string(body))
	}
	// Spec 9b D10: optional ?filter=mine narrows the feed to articles
	// mentioning the user's holdings or watchlist tickers/names.
	if r.URL.Query().Get("filter") == "mine" {
		body = s.applyNewsFilter(r.Context(), body)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// applyNewsFilter narrows the feed JSON to articles whose title or summary
// matches a ticker / company-name token from the user's portfolio. Naive
// case-insensitive substring match; the spec accepts false +/- for v1.
func (s *Server) applyNewsFilter(ctx context.Context, raw []byte) []byte {
	var feed news.Feed
	if err := json.Unmarshal(raw, &feed); err != nil {
		return raw
	}
	needles := s.portfolioNewsNeedles(ctx)
	if len(needles) == 0 {
		return raw
	}
	out := make([]news.Article, 0, len(feed.Articles))
	for _, a := range feed.Articles {
		hay := strings.ToLower(a.Title + " " + a.Summary)
		for _, n := range needles {
			if strings.Contains(hay, n) {
				out = append(out, a)
				break
			}
		}
	}
	feed.Articles = out
	b, _ := json.Marshal(feed)
	return b
}

// portfolioNewsNeedles builds a deduped, lowercased list of tokens to match
// news articles against: every ticker symbol, plus the first word of each
// company name (long enough to be unique).
func (s *Server) portfolioNewsNeedles(ctx context.Context) []string {
	seen := map[string]bool{}
	var add = func(raw string) {
		t := strings.ToLower(strings.TrimSpace(raw))
		if len(t) < 3 { // skip 1-2 char tokens — too noisy
			return
		}
		seen[t] = true
	}
	// stock_holdings: ticker + first-word(name)
	uid, _ := userIDFromContext(ctx)
	if stocks, err := s.store.ListStockHoldings(ctx, uid); err == nil {
		for _, h := range stocks {
			if h.Ticker != nil {
				add(*h.Ticker)
			}
			add(firstWord(h.Name))
		}
	}
	// crypto_holdings: symbol + first-word(name)
	if cryptos, err := s.store.ListCryptoHoldings(ctx, uid); err == nil {
		for _, c := range cryptos {
			add(c.Symbol)
			add(firstWord(c.Name))
		}
	}
	// watchlist (both kinds)
	if wl, err := s.store.ListWatchlist(ctx, uid); err == nil {
		for _, e := range wl {
			add(e.Ticker)
			if e.CompanyName != nil {
				add(firstWord(*e.CompanyName))
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// firstWord returns the first whitespace-delimited token from s (often the
// recognisable brand: "Apple" from "Apple Inc.", "Nvidia" from "NVIDIA Corp").
func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n"); i > 0 {
		return s[:i]
	}
	return s
}

// GET /api/feargreed         — crypto (alternative.me)
// GET /api/feargreed/stocks  — stocks (CNN)
//
// Both 0–100 + classification. Cached in news_cache for 1h, scoped per source.

func (s *Server) handleFearGreed(w http.ResponseWriter, r *http.Request) {
	s.handleFearGreedScope(w, r, "feargreed", market.FetchFearGreed)
}

func (s *Server) handleFearGreedStocks(w http.ResponseWriter, r *http.Request) {
	s.handleFearGreedScope(w, r, "feargreed_stocks", market.FetchStockFearGreed)
}

func (s *Server) handleFearGreedScope(
	w http.ResponseWriter,
	r *http.Request,
	scope string,
	fetcher func(ctx context.Context) (*market.FearGreed, error),
) {
	if payload, fetchedAt, err := s.store.GetNewsCache(r.Context(), scope); err == nil {
		if time.Since(fetchedAt) < newsTTL {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
	}

	fg, err := fetcher(r.Context())
	if err != nil {
		slog.Warn("fear/greed fetch", "scope", scope, "err", err)
		// 6h stale fallback per Spec 2 §D6 risk note.
		if payload, fetchedAt, cerr := s.store.GetNewsCache(r.Context(), scope); cerr == nil &&
			time.Since(fetchedAt) < 6*time.Hour {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(payload))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"value":          nil,
			"classification": "",
			"error":          err.Error(),
		})
		return
	}
	body, _ := json.Marshal(map[string]any{
		"value":          fg.Value,
		"classification": fg.Classification,
		"fetchedAt":      fg.FetchedAt.Format(time.RFC3339),
	})
	_ = s.store.SetNewsCache(r.Context(), scope, string(body))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
