package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// YahooClient fetches BTC daily price history from Yahoo Finance's public
// chart endpoint. Used to seed btc_price_history for the Cowen indicator
// suite (log-band fit, 200-week MA) after CoinGecko's free-tier endpoint
// started returning 401 in mid-2026.
//
// Yahoo's chart endpoint:
//
//	https://query1.finance.yahoo.com/v8/finance/chart/BTC-USD?range=15y&interval=1d
//
// is unauthenticated and returns timestamps + close-price arrays. Same
// shape FT already uses for stock charts (see internal/market/yahoo.go);
// we re-implement the BTC-specific call here to keep the cryptoindicators
// package self-contained and avoid a circular import.
type YahooClient struct {
	HTTP *http.Client
}

func NewYahooClient() *YahooClient {
	return &YahooClient{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// FetchBTCDailyHistory pulls every daily close Yahoo has for BTC-USD
// (back to mid-2014, ~4200 rows). Returned in chronological order
// (oldest first).
//
// Uses explicit period1/period2 epoch timestamps rather than range=max.
// Yahoo's range=max is broken for crypto symbols — it silently caps at
// ~140 rows. Explicit period bounds always return the full available
// history, verified 2026-05-28.
//
// Returns the same BTCMarketChartDay shape the CoinGecko fetcher used so
// callers in btc_history.go don't need to change.
func (c *YahooClient) FetchBTCDailyHistory(ctx context.Context) ([]BTCMarketChartDay, error) {
	// 2014-01-01 UTC is comfortably before BTC-USD's Yahoo firstTradeDate
	// (2014-09-17). Yahoo silently clips to firstTradeDate.
	const period1 = 1388534400 // 2014-01-01 UTC
	period2 := time.Now().UTC().Unix()
	u := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d",
		url.PathEscape("BTC-USD"),
		period1,
		period2,
	)
	body, status, err := doWithRetry(ctx, c.HTTP, u, "")
	if err != nil {
		return nil, fmt.Errorf("yahoo BTC history fetch (HTTP %d): %w", status, err)
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
			Error any `json:"error"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("yahoo BTC history parse: %w", err)
	}
	if len(env.Chart.Result) == 0 || len(env.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo BTC history: empty chart result")
	}
	res := env.Chart.Result[0]
	rawCloses := res.Indicators.Quote[0].Close
	out := make([]BTCMarketChartDay, 0, len(rawCloses))
	for i, c := range rawCloses {
		if c == nil || *c <= 0 || i >= len(res.Timestamp) {
			continue
		}
		out = append(out, BTCMarketChartDay{
			Date:  time.Unix(res.Timestamp[i], 0).UTC(),
			Close: *c,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("yahoo BTC history: zero usable rows")
	}
	return out, nil
}

// ICEDXYSymbol is Yahoo's symbol for the ICE US Dollar Index (the "DXY" the 100/105 thresholds are written for).
// FRED's DTWEXBGS, used before FT-084, is the Fed's broad trade-weighted index (about 121 in 2026) and is not on that
// scale, so its level never matched the 100/105 rows.
const ICEDXYSymbol = "DX-Y.NYB"

// FetchICEDXY reads the ICE US Dollar Index from Yahoo's daily chart: the latest close, its date, and the % change
// against the close on or before the date 28 calendar days earlier.
func (c *YahooClient) FetchICEDXY(ctx context.Context) Reading {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=3mo&interval=1d", url.PathEscape(ICEDXYSymbol))
	body, status, err := doWithRetry(ctx, c.HTTP, u, "")
	if err != nil {
		return Reading{Err: fmt.Sprintf("yahoo %s (HTTP %d): %v", ICEDXYSymbol, status, err)}
	}
	return ParseICEDXY(body)
}

// ParseICEDXY turns a v8/chart daily response into a Reading. Pure, so a fixture can test it.
func ParseICEDXY(body []byte) Reading {
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
	if err := json.Unmarshal(body, &env); err != nil || len(env.Chart.Result) == 0 || len(env.Chart.Result[0].Indicators.Quote) == 0 {
		return Reading{Err: "yahoo DX-Y.NYB: unreadable response"}
	}
	r := env.Chart.Result[0]
	closes := r.Indicators.Quote[0].Close
	type pt struct {
		t time.Time
		v float64
	}
	var pts []pt
	for i, ts := range r.Timestamp {
		if i < len(closes) && closes[i] != nil && *closes[i] > 0 {
			pts = append(pts, pt{time.Unix(ts, 0).UTC(), *closes[i]})
		}
	}
	if len(pts) == 0 {
		return Reading{Err: "yahoo DX-Y.NYB: no closes"}
	}
	last := pts[len(pts)-1]
	v := last.v
	out := Reading{Value: &v, AsOf: last.t.Format("2006-01-02")}
	target := last.t.AddDate(0, 0, -28)
	for i := len(pts) - 1; i >= 0; i-- {
		if !pts[i].t.After(target) {
			roc := (last.v - pts[i].v) / pts[i].v * 100
			out.Trend4w = &roc
			break
		}
	}
	return out
}

// FetchICEDXYHistory returns the ICE US Dollar Index daily closes from start, in the shape the backfill uses for
// FRED series, so a backfill rebuilds the DXY history on the same scale as the live reading (FT-084).
func (c *YahooClient) FetchICEDXYHistory(ctx context.Context, start time.Time) ([]FREDHistoricalPoint, error) {
	u := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d",
		url.PathEscape(ICEDXYSymbol), start.UTC().Unix(), time.Now().UTC().Unix())
	body, status, err := doWithRetry(ctx, c.HTTP, u, "")
	if err != nil {
		return nil, fmt.Errorf("yahoo %s history (HTTP %d): %w", ICEDXYSymbol, status, err)
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
	if err := json.Unmarshal(body, &env); err != nil || len(env.Chart.Result) == 0 || len(env.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo %s history: unreadable response", ICEDXYSymbol)
	}
	r := env.Chart.Result[0]
	var out []FREDHistoricalPoint
	for i, ts := range r.Timestamp {
		if i < len(r.Indicators.Quote[0].Close) && r.Indicators.Quote[0].Close[i] != nil && *r.Indicators.Quote[0].Close[i] > 0 {
			out = append(out, FREDHistoricalPoint{Date: time.Unix(ts, 0).UTC(), Value: *r.Indicators.Quote[0].Close[i]})
		}
	}
	return out, nil
}
