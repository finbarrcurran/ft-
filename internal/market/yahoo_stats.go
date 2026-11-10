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
	ATHDate     string // first day of the month holding the highest high
	HistoryFrom string // first and last bar of the history ATH was taken from
	HistoryTo   string
	Price       *float64 // last regular-market price in the chart meta (FT-082); read at ChartAsOf
	ChartAsOf   int64 // the provider's own quote time (unix); the fetch time when it gave none
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
					Currency          string   `json:"currency"`
					RegularMarketTime int64    `json:"regularMarketTime"`
					RegularMarketPrice *float64 `json:"regularMarketPrice"`
					FiftyTwoWeekHigh  *float64 `json:"fiftyTwoWeekHigh"`
					FiftyTwoWeekLow   *float64 `json:"fiftyTwoWeekLow"`
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
				best, bestDate = *highs[i], d
			}
		}
		if bestDate != "" {
			out.ATH, out.ATHDate, out.HistoryFrom, out.HistoryTo = &best, bestDate, first, last
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
