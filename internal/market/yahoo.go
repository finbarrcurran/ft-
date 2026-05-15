package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// yahooClient handles Yahoo Finance's cookie+crumb anti-bot scheme.
//
// Workflow on first request:
//   1. GET https://fc.yahoo.com → sets session cookies in the jar (no body read).
//   2. GET https://query1.finance.yahoo.com/v1/test/getcrumb → returns the
//      crumb token as plain text (with the cookies attached).
//   3. Subsequent /v7/quote and /v8/chart calls include both the cookies (via
//      the jar) and ?crumb=<value> as a query param.
//
// Crumb is cached for ~30 min; on 401 we invalidate and retry once.
//
// Concurrency: ensureCrumb is mutex-guarded so multiple parallel refreshes
// don't pile up on the warm-up dance.

type yahooClient struct {
	mu      sync.Mutex
	client  *http.Client
	crumb   string
	expires time.Time
}

var yahoo = newYahooClient()

func newYahooClient() *yahooClient {
	jar, _ := cookiejar.New(nil)
	return &yahooClient{
		client: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}
}

func (y *yahooClient) ensureCrumb(ctx context.Context) (string, error) {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.crumb != "" && time.Now().Before(y.expires) {
		return y.crumb, nil
	}

	// 1) Warm-up: hit fc.yahoo.com (or finance.yahoo.com) to get session cookies.
	for _, warmURL := range []string{
		"https://fc.yahoo.com",
		"https://finance.yahoo.com",
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, warmURL, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		res, err := y.client.Do(req)
		if err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}

	// 2) Fetch crumb.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://query1.finance.yahoo.com/v1/test/getcrumb", nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	res, err := y.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
		return "", fmt.Errorf("getcrumb http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 256))
	if err != nil {
		return "", err
	}
	crumb := strings.TrimSpace(string(body))
	if crumb == "" {
		return "", fmt.Errorf("empty crumb")
	}
	y.crumb = crumb
	y.expires = time.Now().Add(30 * time.Minute)
	return crumb, nil
}

func (y *yahooClient) invalidate() {
	y.mu.Lock()
	y.crumb = ""
	y.mu.Unlock()
}

// yahooGet wraps the crumbed GET + one-retry-on-401 dance.
func (y *yahooClient) yahooGet(ctx context.Context, baseURL string) (json.RawMessage, error) {
	crumb, err := y.ensureCrumb(ctx)
	if err != nil {
		return nil, err
	}
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	full := fmt.Sprintf("%s%scrumb=%s", baseURL, sep, url.QueryEscape(crumb))

	doReq := func() (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		return y.client.Do(req)
	}

	res, err := doReq()
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 401 {
		res.Body.Close()
		y.invalidate()
		newCrumb, err2 := y.ensureCrumb(ctx)
		if err2 != nil {
			return nil, err2
		}
		full = fmt.Sprintf("%s%scrumb=%s", baseURL, sep, url.QueryEscape(newCrumb))
		res, err = doReq()
		if err != nil {
			return nil, err
		}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(res.Body, 1<<20))
}

// ----- public Yahoo fetcher used by the chain in stocks.go ----------------

func fetchYahoo(ctx context.Context, ticker string) (*StockQuote, error) {
	// Quote endpoint
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v7/finance/quote?symbols=%s",
		url.QueryEscape(ticker))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		QuoteResponse struct {
			Result []struct {
				Symbol                     string  `json:"symbol"`
				LongName                   string  `json:"longName"`
				ShortName                  string  `json:"shortName"`
				Currency                   string  `json:"currency"`
				RegularMarketPrice         float64 `json:"regularMarketPrice"`
				RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
				FiftyDayAverage            float64 `json:"fiftyDayAverage"`
				TwoHundredDayAverage       float64 `json:"twoHundredDayAverage"`
			} `json:"result"`
		} `json:"quoteResponse"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.QuoteResponse.Result) == 0 {
		return nil, fmt.Errorf("no quote for %s", ticker)
	}
	q := env.QuoteResponse.Result[0]
	if q.RegularMarketPrice == 0 {
		return nil, fmt.Errorf("zero price")
	}

	out := &StockQuote{
		Ticker:    ticker,
		Name:      q.LongName,
		Price:     q.RegularMarketPrice,
		ChangePct: q.RegularMarketChangePercent,
		Currency:  q.Currency,
		FetchedAt: time.Now().UTC(),
	}
	if out.Name == "" {
		out.Name = q.ShortName
	}
	if q.FiftyDayAverage > 0 {
		v := q.FiftyDayAverage
		out.MA50 = &v
	}
	if q.TwoHundredDayAverage > 0 {
		v := q.TwoHundredDayAverage
		out.MA200 = &v
	}

	// Best-effort: also try chart for RSI(14). Failures here don't fail the quote.
	if closes, err := fetchYahooChartCloses(ctx, ticker); err == nil && len(closes) >= 15 {
		rsi := ComputeRSI14(closes)
		out.RSI14 = &rsi
	}

	return out, nil
}

// FetchYahooChartCloses returns the trailing daily-close series for a ticker
// via Yahoo's /v8/chart endpoint. Exported so the refresh package can use it
// as a history source even when the QUOTE came from a different provider.
//
// Default range is 3 months (~63 trading days). Adjust the URL if you need
// more for MA200 reliability.
func FetchYahooChartCloses(ctx context.Context, ticker string) ([]float64, error) {
	return fetchYahooChartCloses(ctx, ticker)
}

func fetchYahooChartCloses(ctx context.Context, ticker string) ([]float64, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=3mo&interval=1d",
		url.PathEscape(ticker))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		Chart struct {
			Result []struct {
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Chart.Result) == 0 || len(env.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("no chart")
	}
	rawCloses := env.Chart.Result[0].Indicators.Quote[0].Close
	out := make([]float64, 0, len(rawCloses))
	for _, c := range rawCloses {
		if c != nil {
			out = append(out, *c)
		}
	}
	return out, nil
}
