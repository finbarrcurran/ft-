package market

import (
	"context"
	"encoding/json"
	"fmt"
	"ft/internal/health"
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

// Named returns let the deferred health.Record see the final error after
// Go's return-statement assigns to retErr.
func fetchYahoo(ctx context.Context, ticker string) (result *StockQuote, retErr error) {
	defer func() { health.Record(ctx, "yahoo", retErr) }()
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
	if closes, err := fetchYahooChartCloses(ctx, ticker, "3mo"); err == nil && len(closes) >= 15 {
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
	return fetchYahooChartCloses(ctx, ticker, "3mo")
}

func fetchYahooChartCloses(ctx context.Context, ticker, rng string) ([]float64, error) {
	pts, err := fetchYahooChartDailyPoints(ctx, ticker, rng)
	if err != nil {
		return nil, err
	}
	out := make([]float64, 0, len(pts))
	for _, p := range pts {
		out = append(out, p.Close)
	}
	return out, nil
}

// DailyClose pairs an ISO date ("YYYY-MM-DD") with that day's closing price.
type DailyClose struct {
	Date  string
	Close float64
}

// FetchYahooDailyCloses returns trailing daily (date, close) pairs for a stock
// ticker. Used by the daily sparkline cron — chart endpoint timestamps are
// converted from epoch-seconds to UTC date strings.
//
// `rng` is the Yahoo range string: "1mo", "3mo", "6mo", "1y", etc.
func FetchYahooDailyCloses(ctx context.Context, ticker, rng string) ([]DailyClose, error) {
	return fetchYahooChartDailyPoints(ctx, ticker, rng)
}

func fetchYahooChartDailyPoints(ctx context.Context, ticker, rng string) ([]DailyClose, error) {
	if rng == "" {
		rng = "3mo"
	}
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1d",
		url.PathEscape(ticker), url.QueryEscape(rng))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
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
	res := env.Chart.Result[0]
	rawCloses := res.Indicators.Quote[0].Close
	out := make([]DailyClose, 0, len(rawCloses))
	for i, c := range rawCloses {
		if c == nil || *c <= 0 || i >= len(res.Timestamp) {
			continue
		}
		date := time.Unix(res.Timestamp[i], 0).UTC().Format("2006-01-02")
		out = append(out, DailyClose{Date: date, Close: *c})
	}
	return out, nil
}

// OHLCBar is one daily candle from Yahoo. All values in USD (or whatever
// the ticker's native currency is — yahoo doesn't convert).
type OHLCBar struct {
	Date   string  // ISO YYYY-MM-DD
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// FetchYahooDailyBars returns full OHLC bars for the given range. Used by
// Spec 9c's ATR + S/R detection pipeline. Standard ranges: "1mo", "3mo",
// "6mo", "1y", "2y", "5y", "max".
func FetchYahooDailyBars(ctx context.Context, ticker, rng string) ([]OHLCBar, error) {
	if rng == "" {
		rng = "2y"
	}
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=1d",
		url.PathEscape(ticker), url.QueryEscape(rng))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*float64 `json:"volume"`
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
	res := env.Chart.Result[0]
	q := res.Indicators.Quote[0]
	n := len(res.Timestamp)
	out := make([]OHLCBar, 0, n)
	for i := 0; i < n; i++ {
		if i >= len(q.Close) || q.Close[i] == nil || *q.Close[i] <= 0 {
			continue
		}
		bar := OHLCBar{
			Date:  time.Unix(res.Timestamp[i], 0).UTC().Format("2006-01-02"),
			Close: *q.Close[i],
		}
		if i < len(q.Open) && q.Open[i] != nil {
			bar.Open = *q.Open[i]
		} else {
			bar.Open = bar.Close // fallback
		}
		if i < len(q.High) && q.High[i] != nil {
			bar.High = *q.High[i]
		} else {
			bar.High = bar.Close
		}
		if i < len(q.Low) && q.Low[i] != nil {
			bar.Low = *q.Low[i]
		} else {
			bar.Low = bar.Close
		}
		if i < len(q.Volume) && q.Volume[i] != nil {
			bar.Volume = *q.Volume[i]
		}
		out = append(out, bar)
	}
	return out, nil
}

// CalendarDates carries the next earnings + ex-dividend dates for a ticker.
// Either field may be empty if the upstream didn't return one (Yahoo free
// tier is patchy outside US; we render "—" then). ISO 'YYYY-MM-DD'.
type CalendarDates struct {
	Ticker       string
	EarningsDate string
	ExDivDate    string
}

// FetchYahooCalendarDates pulls upcoming earnings + ex-dividend dates via
// quoteSummary?modules=calendarEvents. Both fields are best-effort; missing
// values come back as empty strings.
func FetchYahooCalendarDates(ctx context.Context, ticker string) (*CalendarDates, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=calendarEvents",
		url.PathEscape(ticker))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		QuoteSummary struct {
			Result []struct {
				CalendarEvents struct {
					Earnings struct {
						EarningsDate []struct {
							Raw int64 `json:"raw"`
						} `json:"earningsDate"`
					} `json:"earnings"`
					ExDividendDate struct {
						Raw int64 `json:"raw"`
					} `json:"exDividendDate"`
				} `json:"calendarEvents"`
			} `json:"result"`
			Error any `json:"error"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("no calendarEvents")
	}
	ce := env.QuoteSummary.Result[0].CalendarEvents
	out := &CalendarDates{Ticker: ticker}
	if len(ce.Earnings.EarningsDate) > 0 && ce.Earnings.EarningsDate[0].Raw > 0 {
		out.EarningsDate = time.Unix(ce.Earnings.EarningsDate[0].Raw, 0).UTC().Format("2006-01-02")
	}
	if ce.ExDividendDate.Raw > 0 {
		out.ExDivDate = time.Unix(ce.ExDividendDate.Raw, 0).UTC().Format("2006-01-02")
	}
	return out, nil
}

// FetchYahooBeta returns the 5y monthly beta for a ticker via
// quoteSummary?modules=summaryDetail. Returns (nil, error) if Yahoo doesn't
// have beta for this ticker (common for ETFs and some non-US listings).
func FetchYahooBeta(ctx context.Context, ticker string) (*float64, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v10/finance/quoteSummary/%s?modules=summaryDetail,defaultKeyStatistics",
		url.PathEscape(ticker))
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return nil, err
	}
	var env struct {
		QuoteSummary struct {
			Result []struct {
				SummaryDetail struct {
					Beta struct {
						Raw float64 `json:"raw"`
					} `json:"beta"`
				} `json:"summaryDetail"`
				DefaultKeyStatistics struct {
					Beta3Y struct {
						Raw float64 `json:"raw"`
					} `json:"beta3Year"`
				} `json:"defaultKeyStatistics"`
			} `json:"result"`
		} `json:"quoteSummary"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.QuoteSummary.Result) == 0 {
		return nil, fmt.Errorf("no summaryDetail")
	}
	r := env.QuoteSummary.Result[0]
	if r.SummaryDetail.Beta.Raw != 0 {
		v := r.SummaryDetail.Beta.Raw
		return &v, nil
	}
	if r.DefaultKeyStatistics.Beta3Y.Raw != 0 {
		v := r.DefaultKeyStatistics.Beta3Y.Raw
		return &v, nil
	}
	return nil, fmt.Errorf("no beta for %s", ticker)
}
