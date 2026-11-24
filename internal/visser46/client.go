package visser46

import (
	"context"
	"fmt"
	"ft/internal/health"
	"ft/internal/market"
	"net/url"
	"strings"
	"time"
)

// RetryBackoff is the wait before the first retry of a 429 (it grows by one unit per attempt). Tests shorten it.
var RetryBackoff = 15 * time.Second

// MaxRetries is how many times a 429 is retried (each retry is one more counted call, and is gated again).
const MaxRetries = 3

// Client is the only way Part B reaches CoinGecko.
type Client struct {
	Gate *Gate
}

// Feature names recorded with each call.
const (
	FeatureRefresh  = "partb_refresh"
	FeatureBackfill = "partb_backfill"
	// FeatureDailyCloseExempt tags the single call a day the daily close may make above the soft stop.
	FeatureDailyCloseExempt = "partb_dailyclose_exempt"
)

// get runs one gated keyed call; a 429 is retried after backoff, the gate being asked again before every attempt.
func (c *Client) get(ctx context.Context, feature, path string, q url.Values, out any) error {
	ctx = health.WithFeature(ctx, feature)
	var err error
	for attempt := 0; ; attempt++ {
		if gerr := c.Gate.Allow(ctx); gerr != nil {
			return gerr
		}
		err = market.CoinGeckoGetJSON(ctx, path, q, true, 0, out) // retries=0: the loop here owns retry so the gate runs each time
		if err == nil || attempt >= MaxRetries || !strings.Contains(err.Error(), "http 429") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * RetryBackoff):
		}
	}
}

// MarketRow is one /coins/markets row.
type MarketRow struct {
	ID        string   `json:"id"`
	Symbol    string   `json:"symbol"`
	Price     *float64 `json:"current_price"`
	MarketCap *float64 `json:"market_cap"`
	Rank      *int     `json:"market_cap_rank"`
	D24       *float64 `json:"price_change_percentage_24h_in_currency"`
	D7        *float64 `json:"price_change_percentage_7d_in_currency"`
	D30       *float64 `json:"price_change_percentage_30d_in_currency"`
}

// Markets makes ONE keyed /coins/markets call for all ids.
func (c *Client) Markets(ctx context.Context, ids []string) ([]MarketRow, error) {
	q := url.Values{}
	q.Set("vs_currency", "usd")
	q.Set("ids", strings.Join(ids, ","))
	q.Set("price_change_percentage", "24h,7d,30d")
	q.Set("per_page", "250")
	q.Set("sparkline", "false")
	var rows []MarketRow
	if err := c.get(ctx, FeatureRefresh, "/coins/markets", q, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// MarketsExempt is Markets for the daily close above the soft stop: exempt from the soft stop only (kill switch
// and key still apply), at most ONE call per UTC day (today's counter is read from the store, so a restart does
// not reset it), no retry (a failed attempt uses the day's call).
func (c *Client) MarketsExempt(ctx context.Context, st interface {
	CoinGeckoFeatureCallsOn(ctx context.Context, day, feature string) (int, error)
}, ids []string, now time.Time) ([]MarketRow, error) {
	if err := c.Gate.AllowExempt(ctx); err != nil {
		return nil, err
	}
	n, err := st.CoinGeckoFeatureCallsOn(ctx, now.UTC().Format("2006-01-02"), FeatureDailyCloseExempt)
	if err != nil {
		return nil, err
	}
	if n >= 1 {
		return nil, ErrExemptUsed
	}
	q := url.Values{}
	q.Set("vs_currency", "usd")
	q.Set("ids", strings.Join(ids, ","))
	q.Set("price_change_percentage", "24h,7d,30d")
	q.Set("per_page", "250")
	q.Set("sparkline", "false")
	var rows []MarketRow
	ctx = health.WithFeature(ctx, FeatureDailyCloseExempt)
	if err := market.CoinGeckoGetJSON(ctx, "/coins/markets", q, true, 0, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// MarketChart makes one keyed /coins/{id}/market_chart call and returns the daily closes (one per UTC date).
func (c *Client) MarketChart(ctx context.Context, id string, days int) ([]market.DailyClose, error) {
	q := url.Values{}
	q.Set("vs_currency", "usd")
	q.Set("days", fmt.Sprintf("%d", days))
	q.Set("interval", "daily")
	var data struct {
		Prices [][]float64 `json:"prices"` // [unix_ms, price]
	}
	if err := c.get(ctx, FeatureBackfill, "/coins/"+url.PathEscape(id)+"/market_chart", q, &data); err != nil {
		return nil, err
	}
	out := make([]market.DailyClose, 0, len(data.Prices))
	seen := map[string]bool{}
	for _, row := range data.Prices {
		if len(row) < 2 || row[1] <= 0 {
			continue
		}
		date := time.UnixMilli(int64(row[0])).UTC().Format("2006-01-02")
		if seen[date] { // two points for one day: keep the later
			out[len(out)-1].Close = row[1]
			continue
		}
		seen[date] = true
		out = append(out, market.DailyClose{Date: date, Close: row[1]})
	}
	return out, nil
}
