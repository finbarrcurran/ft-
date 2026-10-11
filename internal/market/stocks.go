package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/health"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// ----- HTTP client --------------------------------------------------------

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

func httpGetJSON(ctx context.Context, rawURL string, out any) error {
	return httpGetJSONHeaders(ctx, rawURL, nil, out)
}

// httpGetJSONHeaders is httpGetJSON with extra request headers (CNN's endpoint
// answers HTTP 418 "You're a bot" unless a Referer/Origin from cnn.com is present).
func httpGetJSONHeaders(ctx context.Context, rawURL string, extra map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// ----- Stock quote fetch with provider chain ------------------------------
//
// Chain (SC-48, v1.80.0): Finnhub (US tickers, free tier 60 calls/min) -> Yahoo.
// Twelve Data was retired: its free plan is US-only and capped at 800 credits a
// day, FT was spending ~3,400 a day on it, and every call failed (Yahoo supplied
// the data). Callers still get one StockQuote per successful ticker.

// FetchStockQuote returns a quote for one ticker. Tries Finnhub (US tickers only,
// rate-limited and cached) then Yahoo. Returns nil + err if both fail.
func FetchStockQuote(ctx context.Context, ticker string) (*StockQuote, error) {
	if q, err := fetchFinnhub(ctx, ticker); err == nil {
		return q, nil
	} else if !errors.Is(err, errFinnhubSkipped) {
		slog.Debug("finnhub miss", "ticker", ticker, "err", err)
	}
	q, err := fetchYahoo(ctx, ticker)
	if err != nil {
		return nil, fmt.Errorf("all providers failed: %w", err)
	}
	return q, nil
}

// FetchStockQuotes batch-fetches multiple tickers sequentially. Failures are
// logged + skipped; the returned slice contains only successful quotes. The
// Finnhub rate gate spaces the calls, so no extra sleep is needed here.
func FetchStockQuotes(ctx context.Context, tickers []string) []*StockQuote {
	out := make([]*StockQuote, 0, len(tickers))
	for _, t := range tickers {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		q, err := FetchStockQuote(ctx, t)
		if err != nil {
			slog.Warn("stock quote failed (all providers)", "ticker", t, "err", err)
			continue
		}
		out = append(out, q)
	}
	return out
}

// FetchStockQuotesYahoo fetches quotes straight from Yahoo, sequentially, for
// tickers that do not need Finnhub (the heatmap tiles). Guardrails so moving ~80
// tiles here does not just shift the rate-limit problem: one request at a time,
// 150 ms apart, and the batch stops after 5 consecutive failures (the heatmap
// keeps its previous live values; nothing is cleared).
func FetchStockQuotesYahoo(ctx context.Context, tickers []string) []*StockQuote {
	out := make([]*StockQuote, 0, len(tickers))
	consecutive := 0
	for _, t := range tickers {
		select {
		case <-ctx.Done():
			return out
		default:
		}
		q, err := fetchYahoo(ctx, t)
		if err != nil {
			consecutive++
			slog.Debug("yahoo quote miss", "ticker", t, "err", err)
			if consecutive >= 5 {
				slog.Warn("yahoo quote batch stopped after 5 consecutive failures", "fetched", len(out), "of", len(tickers))
				return out
			}
			time.Sleep(time.Duration(consecutive) * 500 * time.Millisecond) // back off
			continue
		}
		consecutive = 0
		out = append(out, q)
		time.Sleep(150 * time.Millisecond)
	}
	return out
}

// ----- Finnhub (primary, US tickers) -------------------------------------

var finnhubBase = "https://finnhub.io/api/v1" // var so tests can point it at a fake server

// errFinnhubSkipped marks a ticker Finnhub's free tier cannot serve (non-US
// listings carry an exchange suffix such as .L, .PA, .DE, .T). It is not a call,
// so it is neither counted against the provider nor recorded as a failure.
var errFinnhubSkipped = errors.New("finnhub: non-US ticker skipped")

const (
	finnhubMinGap   = 1100 * time.Millisecond // <= ~54 calls/min, under the 60/min limit
	finnhubCacheTTL = 5 * time.Minute
)

// rateGate serialises calls and enforces a minimum gap (plus jitter) between
// them; penalize pushes the next allowed call out (used on 429).
type rateGate struct {
	mu      sync.Mutex
	next    time.Time
	blocked time.Time
	minGap  time.Duration
}

func (g *rateGate) wait(ctx context.Context) error {
	g.mu.Lock()
	t := time.Now()
	if g.next.After(t) {
		t = g.next
	}
	if g.blocked.After(t) {
		t = g.blocked
	}
	g.next = t.Add(g.minGap + time.Duration(rand.Intn(150))*time.Millisecond)
	g.mu.Unlock()
	d := time.Until(t)
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *rateGate) penalize(d time.Duration) {
	g.mu.Lock()
	if until := time.Now().Add(d); until.After(g.blocked) {
		g.blocked = until
	}
	g.mu.Unlock()
}

var finnhubGate = &rateGate{minGap: finnhubMinGap}

type finnhubCacheEntry struct {
	q  *StockQuote
	at time.Time
}

var (
	finnhubCacheMu sync.Mutex
	finnhubCache   = map[string]finnhubCacheEntry{}
	finnhubFlight  singleflight.Group

	finnhubHeadroom atomic.Pointer[FinnhubLimits]
)

// FinnhubLimits is the last rate-limit state Finnhub reported in its response headers.
type FinnhubLimits struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	ResetAt   time.Time `json:"resetAt"`
	SeenAt    time.Time `json:"seenAt"`
}

// FinnhubHeadroom returns the most recent rate-limit headers seen, or nil.
func FinnhubHeadroom() *FinnhubLimits { return finnhubHeadroom.Load() }

func fetchFinnhub(ctx context.Context, ticker string) (*StockQuote, error) {
	if strings.Contains(ticker, ".") {
		return nil, errFinnhubSkipped
	}
	finnhubCacheMu.Lock()
	if e, ok := finnhubCache[ticker]; ok && time.Since(e.at) < finnhubCacheTTL {
		finnhubCacheMu.Unlock()
		c := *e.q
		return &c, nil
	}
	finnhubCacheMu.Unlock()
	// Collapse concurrent requests for the same ticker into one call.
	v, err, _ := finnhubFlight.Do(ticker, func() (any, error) { return fetchFinnhubOnce(ctx, ticker) })
	if err != nil {
		return nil, err
	}
	c := *(v.(*StockQuote))
	return &c, nil
}

func fetchFinnhubOnce(ctx context.Context, ticker string) (q *StockQuote, err error) {
	defer func() { health.Record(ctx, "finnhub", err) }()
	key := os.Getenv("FT_FINNHUB_API_KEY")
	if key == "" {
		return nil, errors.New("FT_FINNHUB_API_KEY not set")
	}
	if err = finnhubGate.wait(ctx); err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/quote?symbol=%s&token=%s", finnhubBase, url.QueryEscape(ticker), url.QueryEscape(key))
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if rerr != nil {
		return nil, rerr
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, derr := httpClient.Do(req)
	if derr != nil {
		return nil, errors.New("finnhub: request failed") // never include the URL (it carries the key)
	}
	defer res.Body.Close()
	noteFinnhubHeaders(res.Header)
	if res.StatusCode == http.StatusTooManyRequests {
		finnhubGate.penalize(30 * time.Second) // back off; the per-minute window resets within a minute
		return nil, fmt.Errorf("http 429: finnhub rate limit")
	}
	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 200))
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		C  float64 `json:"c"`
		Dp float64 `json:"dp"`
	}
	if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.C == 0 {
		err = fmt.Errorf("finnhub: zero price (unknown symbol?)")
		return nil, err
	}
	q = &StockQuote{Ticker: ticker, Price: out.C, ChangePct: out.Dp, FetchedAt: time.Now().UTC()}
	finnhubCacheMu.Lock()
	cp := *q
	finnhubCache[ticker] = finnhubCacheEntry{q: &cp, at: time.Now()}
	finnhubCacheMu.Unlock()
	return q, nil
}

// noteFinnhubHeaders records x-ratelimit-* and, when the window is nearly spent,
// holds the gate until it resets.
func noteFinnhubHeaders(h http.Header) {
	lim, _ := strconv.Atoi(h.Get("X-Ratelimit-Limit"))
	rem, e2 := strconv.Atoi(h.Get("X-Ratelimit-Remaining"))
	reset, _ := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64)
	if lim == 0 && e2 != nil {
		return
	}
	fl := &FinnhubLimits{Limit: lim, Remaining: rem, SeenAt: time.Now().UTC()}
	if reset > 0 {
		fl.ResetAt = time.Unix(reset, 0).UTC()
	}
	finnhubHeadroom.Store(fl)
	if e2 == nil && rem <= 2 && !fl.ResetAt.IsZero() {
		if d := time.Until(fl.ResetAt); d > 0 && d < 90*time.Second {
			finnhubGate.penalize(d + 250*time.Millisecond)
		}
	}
}

// ----- History enrichment via Yahoo --------------------------------------
//
// Finnhub free returns price only (US tickers). The
// Yahoo crumb path (yahoo.go) already works for both. We use it to fill in
// RSI14 / MA50 / MA200 on quotes that came back without them.

// EnrichStockHistory mutates q to add RSI14 / MA50 / MA200 derived from the
// trailing daily-close series on Yahoo's /v8/chart endpoint. Best-effort: on
// any failure the function returns silently and leaves the quote alone.
func EnrichStockHistory(ctx context.Context, q *StockQuote) {
	if q == nil || q.Ticker == "" {
		return
	}
	closes, err := FetchYahooChartCloses(ctx, q.Ticker)
	if err != nil || len(closes) < 15 {
		return
	}
	rsi := ComputeRSI14(closes)
	q.RSI14 = &rsi
	if len(closes) >= 50 {
		ma50 := mean(closes[len(closes)-50:])
		q.MA50 = &ma50
	}
	if len(closes) >= 200 {
		ma200 := mean(closes[len(closes)-200:])
		q.MA200 = &ma200
	}
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// ----- RSI ---------------------------------------------------------------

// ComputeRSI14 returns the 14-day RSI of a daily-close series using Wilder's
// smoothing. Mirrors lib/market-data/stocks.ts → computeRsi14 in the
// Next.js prototype.
func ComputeRSI14(closes []float64) float64 {
	const period = 14
	if len(closes) < period+1 {
		return 50
	}
	var gain, loss float64
	for i := 1; i <= period; i++ {
		d := closes[i] - closes[i-1]
		if d >= 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	avgGain := gain / period
	avgLoss := loss / period
	for i := period + 1; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		var g, l float64
		if d >= 0 {
			g = d
		} else {
			l = -d
		}
		avgGain = (avgGain*(period-1) + g) / period
		avgLoss = (avgLoss*(period-1) + l) / period
	}
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}
