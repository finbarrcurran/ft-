package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ----- HTTP client --------------------------------------------------------

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

func httpGetJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
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
// Strategy: try Finnhub first (60 req/min free, fast). If Finnhub rejects the
// symbol (typically 403 for non-US tickers on free tier), fall back to
// TwelveData (8 req/min, 800/day, global coverage). Caller-visible behavior:
// one StockQuote per successful ticker, regardless of which provider supplied it.

// FetchStockQuote returns a quote for one ticker. Tries Finnhub → TwelveData → Yahoo.
// Returns the first successful provider's result. Returns nil + err if all fail.
//
// Finnhub free tier: US tickers only, quote only (no candles for RSI/MA).
// TwelveData free tier: US tickers only (international paywalled mid-2024).
// Yahoo: covers everything for free, but requires the cookie+crumb dance and
// can break when Yahoo changes their token scheme.
func FetchStockQuote(ctx context.Context, ticker string) (*StockQuote, error) {
	if q, err := fetchFinnhub(ctx, ticker); err == nil {
		return q, nil
	} else {
		slog.Debug("finnhub miss", "ticker", ticker, "err", err)
	}
	if q, err := fetchTwelveData(ctx, ticker); err == nil {
		return q, nil
	} else {
		slog.Debug("twelvedata miss", "ticker", ticker, "err", err)
	}
	q, err := fetchYahoo(ctx, ticker)
	if err != nil {
		return nil, fmt.Errorf("all providers failed: %w", err)
	}
	return q, nil
}

// FetchStockQuotes batch-fetches multiple tickers sequentially. Failures are
// logged + skipped; the returned slice contains only successful quotes.
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
			slog.Warn("stock quote failed (both providers)", "ticker", t, "err", err)
			continue
		}
		out = append(out, q)
		// 100 ms inter-request — comfortably under provider rate limits.
		time.Sleep(100 * time.Millisecond)
	}
	return out
}

// ----- Finnhub (primary) -------------------------------------------------

const finnhubBase = "https://finnhub.io/api/v1"

func fetchFinnhub(ctx context.Context, ticker string) (*StockQuote, error) {
	key := os.Getenv("FT_FINNHUB_API_KEY")
	if key == "" {
		return nil, errors.New("FT_FINNHUB_API_KEY not set")
	}
	var res struct {
		C  float64 `json:"c"`
		Dp float64 `json:"dp"`
		Pc float64 `json:"pc"`
		T  int64   `json:"t"`
	}
	u := fmt.Sprintf("%s/quote?symbol=%s&token=%s",
		finnhubBase, url.QueryEscape(ticker), url.QueryEscape(key))
	if err := httpGetJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	if res.C == 0 {
		return nil, fmt.Errorf("finnhub: zero price (unknown symbol?)")
	}
	return &StockQuote{
		Ticker:    ticker,
		Price:     res.C,
		ChangePct: res.Dp,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// ----- TwelveData (fallback) ---------------------------------------------
//
// TwelveData accepts the same ticker suffix convention the prototype uses:
// RR.L, SU.PA, RHM.DE, 4063.T all resolve to the right listing.
//
// Numeric fields come back as strings — parse explicitly.

const twelveDataBase = "https://api.twelvedata.com"

func fetchTwelveData(ctx context.Context, ticker string) (*StockQuote, error) {
	key := os.Getenv("FT_TWELVEDATA_API_KEY")
	if key == "" {
		return nil, errors.New("FT_TWELVEDATA_API_KEY not set")
	}
	var res struct {
		Symbol        string `json:"symbol"`
		Name          string `json:"name"`
		Close         string `json:"close"`
		PercentChange string `json:"percent_change"`
		Currency      string `json:"currency"`
		Status        string `json:"status"`  // "ok" or "error"
		Code          int    `json:"code"`
		Message       string `json:"message"`
	}
	u := fmt.Sprintf("%s/quote?symbol=%s&apikey=%s",
		twelveDataBase, url.QueryEscape(ticker), url.QueryEscape(key))
	if err := httpGetJSON(ctx, u, &res); err != nil {
		return nil, err
	}
	if res.Status == "error" || res.Code != 0 || res.Close == "" {
		if res.Message != "" {
			return nil, fmt.Errorf("twelvedata: %s", res.Message)
		}
		return nil, fmt.Errorf("twelvedata: empty result")
	}
	price, err := strconv.ParseFloat(res.Close, 64)
	if err != nil {
		return nil, fmt.Errorf("twelvedata: parse close: %w", err)
	}
	var changePct float64
	if v, err := strconv.ParseFloat(res.PercentChange, 64); err == nil {
		changePct = v
	}
	name := res.Name
	currency := res.Currency
	q := &StockQuote{
		Ticker:    ticker,
		Name:      name,
		Price:     price,
		ChangePct: changePct,
		Currency:  currency,
		FetchedAt: time.Now().UTC(),
	}
	return q, nil
}

// ----- History enrichment via Yahoo --------------------------------------
//
// Finnhub free returns price only; TwelveData free 404s for non-US. The
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
