package market

// SC-57 — 52-week range, all-time high (from the available history only) and market cap from Yahoo.
// Nothing here estimates: a value Yahoo does not return stays nil.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"ft/internal/health"
)

// MarketStats is one ticker's range and size data. Nil pointers mean "not available".
type MarketStats struct {
	Currency    string
	High52w     *float64
	Low52w      *float64
	ATH         *float64
	ATHDate     string // ParseChartRange: the start of the bar holding the high; FetchYahooMarketStats narrows it to the trading day ("" if it could not)
	ATHBarStart int64  // start (unix) of the history bar that holds the highest high: a week or a month, not the day
	HistoryFrom string // first and last bar of the history ATH was taken from
	HistoryTo   string
	Price       *float64 // last regular-market price in the chart meta (FT-082); read at ChartAsOf
	ChartAsOf   int64    // the provider's own quote time (unix); the fetch time when it gave none
	MarketCap   *float64
	CapAsOf     int64 // when the market cap was read (Yahoo gives no timestamp for it)
	ChartErr    error // set when the chart half failed (the cap half may still be filled)
	CapErr      error
}

// ParseChartRange reads a v8/chart (range=max, interval=1mo) response. Pure, so fixtures can test it.
func ParseChartRange(raw []byte, fetched time.Time) (*MarketStats, error) {
	var env struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Currency           string   `json:"currency"`
					RegularMarketTime  int64    `json:"regularMarketTime"`
					RegularMarketPrice *float64 `json:"regularMarketPrice"`
					FiftyTwoWeekHigh   *float64 `json:"fiftyTwoWeekHigh"`
					FiftyTwoWeekLow    *float64 `json:"fiftyTwoWeekLow"`
				} `json:"meta"`
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						High []*float64 `json:"high"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Chart.Result) == 0 {
		return nil, fmt.Errorf("no chart result")
	}
	res := env.Chart.Result[0]
	out := &MarketStats{Currency: res.Meta.Currency, ChartAsOf: res.Meta.RegularMarketTime}
	if out.ChartAsOf == 0 {
		out.ChartAsOf = fetched.Unix()
	}
	if v := res.Meta.RegularMarketPrice; v != nil && *v > 0 {
		out.Price = v
	}
	if v := res.Meta.FiftyTwoWeekHigh; v != nil && *v > 0 {
		out.High52w = v
	}
	if v := res.Meta.FiftyTwoWeekLow; v != nil && *v > 0 {
		out.Low52w = v
	}
	if len(res.Indicators.Quote) > 0 {
		highs := res.Indicators.Quote[0].High
		first, last := "", ""
		var best float64
		bestDate := ""
		var bestTS int64
		for i, ts := range res.Timestamp {
			if i >= len(highs) || highs[i] == nil || *highs[i] <= 0 {
				continue
			}
			d := time.Unix(ts, 0).UTC().Format("2006-01-02")
			if first == "" {
				first = d
			}
			last = d
			if *highs[i] > best {
				best, bestDate, bestTS = *highs[i], d, ts
			}
		}
		if bestDate != "" {
			out.ATH, out.ATHDate, out.HistoryFrom, out.HistoryTo = &best, bestDate, first, last
			out.ATHBarStart = bestTS
		}
	}
	return out, nil
}

// FetchYahooMarketStats does one chart call (history + 52-week meta) and one price call (market cap).
// Either half may fail on its own; the error is kept on the result and the other half is still returned.
// It only returns an error when both halves failed.
func FetchYahooMarketStats(ctx context.Context, symbol string) (out *MarketStats, retErr error) {
	defer func() { health.Record(ctx, "yahoo", retErr) }()
	out = &MarketStats{}
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=max&interval=1mo", url.PathEscape(symbol))
	raw, err := yahoo.yahooGet(ctx, u)
	if err == nil {
		var cs *MarketStats
		if cs, err = ParseChartRange(raw, time.Now()); err == nil {
			out = cs
			// SC-57 v1.110.1: the history bars are weeks or months, so the date found so far is a bar start.
			// Resolve the actual day; the high may be a little higher on the day than in a rounded bar.
			if out.ATH != nil {
				day, high := resolveATHDay(ctx, symbol, out.ATHBarStart)
				out.ATHDate = day
				if day != "" && high > *out.ATH {
					out.ATH = &high
				}
			}
		}
	}
	out.ChartErr = err
	f, ferr := FetchYahooFundamentals(ctx, symbol)
	if ferr == nil && f.MarketCap != nil {
		out.MarketCap, out.CapAsOf = f.MarketCap, time.Now().Unix()
	} else if ferr == nil {
		ferr = fmt.Errorf("no market cap for %s", symbol)
	}
	out.CapErr = ferr
	if out.ChartErr != nil && out.CapErr != nil {
		return out, fmt.Errorf("chart: %v; cap: %v", out.ChartErr, out.CapErr)
	}
	return out, nil
}

// ParseDailyHighDay reads a v8/chart daily response and returns the trading day with the highest high (FT-082b / SC-57
// v1.110.1). Pure, so a fixture can test it. ok is false when the response has no usable high.
func ParseDailyHighDay(raw []byte) (day string, high float64, ok bool) {
	var env struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						High []*float64 `json:"high"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if json.Unmarshal(raw, &env) != nil || len(env.Chart.Result) == 0 || len(env.Chart.Result[0].Indicators.Quote) == 0 {
		return "", 0, false
	}
	r := env.Chart.Result[0]
	highs := r.Indicators.Quote[0].High
	for i, ts := range r.Timestamp {
		if i >= len(highs) || highs[i] == nil || *highs[i] <= 0 {
			continue
		}
		if *highs[i] > high {
			high, day, ok = *highs[i], time.Unix(ts, 0).UTC().Format("2006-01-02"), true
		}
	}
	return day, high, ok
}

// resolveATHDay narrows the all-time-high date from the start of its history bar to the actual trading day, with one
// daily chart call over the bar's window. On any failure the date is left empty rather than showing a bar start.
func resolveATHDay(ctx context.Context, symbol string, barStart int64) (string, float64) {
	if barStart <= 0 {
		return "", 0
	}
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d",
		url.PathEscape(symbol), barStart-86400, barStart+40*86400)
	raw, err := yahoo.yahooGet(ctx, u)
	if err != nil {
		return "", 0
	}
	day, high, ok := ParseDailyHighDay(raw)
	if !ok {
		return "", 0
	}
	return day, high
}
