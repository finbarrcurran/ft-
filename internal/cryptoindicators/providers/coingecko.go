package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"time"
)

// doWithRetry runs an HTTP GET with retry on 429 (rate-limited).
// Exponential backoff with jitter: ~2s, 4s, 8s. Other errors / non-2xx
// fall through immediately. Total worst-case time: ~14s including the
// last attempt.
//
// SC-18: when a CoinGecko Demo API key is configured (FT_COINGECKO_API_KEY)
// it's sent as the x-cg-demo-api-key header, which lifts the keyless
// free-tier ceiling that was hard-walling /global + /simple/price with
// persistent 429s. Jitter on the backoff spreads retries so a burst from
// the parallel crypto-prices cron doesn't resonate into repeated 429s.
func doWithRetry(ctx context.Context, client *http.Client, url, apiKey string) ([]byte, int, error) {
	backoff := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	var lastBody []byte
	var lastStatus int
	for attempt := 0; attempt < len(backoff)+1; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		// Browser-like User-Agent — many providers (Farside via Cloudflare,
		// some CoinGecko endpoints) 403 a bare Go default. Setting one UA
		// across all retries keeps the request consistent.
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; FT/1.0; +https://ft.curranhouse.dev)")
		req.Header.Set("Accept", "text/html,application/json,*/*")
		if apiKey != "" {
			req.Header.Set("x-cg-demo-api-key", apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, readErr
		}
		lastBody = body
		lastStatus = resp.StatusCode

		if resp.StatusCode == 200 {
			return body, 200, nil
		}
		// Only retry on 429. Anything else (404, 500, etc.) — give up.
		if resp.StatusCode != 429 || attempt == len(backoff) {
			break
		}
		// Jitter ±25% so concurrent callers don't retry in lockstep.
		wait := backoff[attempt]
		jitter := time.Duration(rand.Int63n(int64(wait) / 2))
		wait = wait*3/4 + jitter
		select {
		case <-ctx.Done():
			return nil, lastStatus, ctx.Err()
		case <-time.After(wait):
		}
	}
	return lastBody, lastStatus, fmt.Errorf("HTTP %d after %d attempts", lastStatus, len(backoff)+1)
}

// CoinGeckoClient fetches the extras Spec 9e needs that aren't already
// in FT's existing CoinGecko integration (BTC dominance, ETH/BTC ratio).
//
// /global gives total market cap + dominance per coin.
// /simple/price gives spot USD prices for an arbitrary id list.
//
// Free-tier limits: ~50 calls/min. Our cron makes 2 calls/day. Fine.
type CoinGeckoClient struct {
	HTTP   *http.Client
	APIKey string // CoinGecko Demo key (FT_COINGECKO_API_KEY); "" = keyless
}

func NewCoinGeckoClient() *CoinGeckoClient {
	return &CoinGeckoClient{
		HTTP:   &http.Client{Timeout: 20 * time.Second},
		APIKey: os.Getenv("FT_COINGECKO_API_KEY"),
	}
}

type cgGlobalResp struct {
	Data struct {
		MarketCapPercentage map[string]float64 `json:"market_cap_percentage"`
	} `json:"data"`
}

// FetchBTCDominance returns BTC's % share of total crypto market cap.
// Trend4w is left nil — CoinGecko's /global is current-only. The
// refresher computes trend from local snapshot history once 28 days
// have accumulated.
func (c *CoinGeckoClient) FetchBTCDominance(ctx context.Context) Reading {
	body, _, err := doWithRetry(ctx, c.HTTP, "https://api.coingecko.com/api/v3/global", c.APIKey)
	if err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /global: %v", err)}
	}
	var data cgGlobalResp
	if err := json.Unmarshal(body, &data); err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /global decode: %v", err)}
	}
	v, ok := data.Data.MarketCapPercentage["btc"]
	if !ok {
		return Reading{Err: "coingecko /global: btc dominance absent"}
	}
	return Reading{Value: &v}
}

type cgSimplePriceResp map[string]map[string]float64

// FetchETHBTCRatio returns ETH price / BTC price.
func (c *CoinGeckoClient) FetchETHBTCRatio(ctx context.Context) Reading {
	body, _, err := doWithRetry(ctx, c.HTTP,
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd", c.APIKey)
	if err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /simple/price: %v", err)}
	}
	var data cgSimplePriceResp
	if err := json.Unmarshal(body, &data); err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /simple/price decode: %v", err)}
	}
	btc, okB := data["bitcoin"]["usd"]
	eth, okE := data["ethereum"]["usd"]
	if !okB || !okE || btc == 0 {
		return Reading{Err: "coingecko /simple/price: missing BTC or ETH price"}
	}
	ratio := eth / btc
	return Reading{Value: &ratio}
}

// FetchBTCPriceUSD returns just BTC's spot USD price. Used by the
// daily snapshot to record BTC price alongside the composite.
func (c *CoinGeckoClient) FetchBTCPriceUSD(ctx context.Context) (*float64, error) {
	body, _, err := doWithRetry(ctx, c.HTTP,
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd", c.APIKey)
	if err != nil {
		return nil, err
	}
	var data cgSimplePriceResp
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if v, ok := data["bitcoin"]["usd"]; ok {
		return &v, nil
	}
	return nil, fmt.Errorf("bitcoin price not in response")
}

// CGMarket is one row of CoinGecko's /coins/markets top-N response. SC-21:
// feeds the crypto market screener. Percentage fields are pointers because
// CoinGecko returns null for coins missing a given window.
type CGMarket struct {
	ID            string   `json:"id"`     // gecko id (e.g. "ethereum")
	Symbol        string   `json:"symbol"` // lowercase ticker (e.g. "eth")
	Name          string   `json:"name"`
	CurrentPrice  float64  `json:"current_price"`
	MarketCap     float64  `json:"market_cap"`
	MarketCapRank int      `json:"market_cap_rank"`
	TotalVolume   float64  `json:"total_volume"`
	Change24h     *float64 `json:"price_change_percentage_24h_in_currency"`
	Change7d      *float64 `json:"price_change_percentage_7d_in_currency"`
	Change30d     *float64 `json:"price_change_percentage_30d_in_currency"`
}

// FetchMarkets pulls the top-`perPage` coins by market cap (one page) with
// 24h/7d/30d change windows. SC-21 uses perPage=250 (CoinGecko's max). One
// call; the Demo key (SC-18) gives the rate headroom.
func (c *CoinGeckoClient) FetchMarkets(ctx context.Context, perPage int) ([]CGMarket, error) {
	if perPage <= 0 || perPage > 250 {
		perPage = 250
	}
	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/coins/markets?vs_currency=usd&order=market_cap_desc&per_page=%d&page=1&sparkline=false&price_change_percentage=24h%%2C7d%%2C30d&locale=en",
		perPage)
	body, _, err := doWithRetry(ctx, c.HTTP, url, c.APIKey)
	if err != nil {
		return nil, fmt.Errorf("coingecko /coins/markets: %w", err)
	}
	var rows []CGMarket
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("coingecko /coins/markets decode: %w", err)
	}
	return rows, nil
}

// BTCMarketChartDay is one daily close for the historical BTC time series.
type BTCMarketChartDay struct {
	Date  time.Time
	Close float64
}

// FetchBTCDailyHistory pulls every daily close CoinGecko has for BTC
// (currently ~2013 onwards). Used by v1.8.3 to seed btc_price_history
// for the Cowen log-band fit + 200wma. One call; ~3500 rows.
//
// CoinGecko endpoint /coins/bitcoin/market_chart?days=max returns
// timestamps (ms) + price pairs.
func (c *CoinGeckoClient) FetchBTCDailyHistory(ctx context.Context) ([]BTCMarketChartDay, error) {
	// Note: free-tier limit is 365 days back for /market_chart on most
	// endpoints, BUT bitcoin is exempt and goes back to inception.
	body, _, err := doWithRetry(ctx, c.HTTP,
		"https://api.coingecko.com/api/v3/coins/bitcoin/market_chart?vs_currency=usd&days=max&interval=daily", c.APIKey)
	if err != nil {
		return nil, err
	}
	var data struct {
		Prices [][]float64 `json:"prices"` // [[ts_ms, price], ...]
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	out := make([]BTCMarketChartDay, 0, len(data.Prices))
	for _, p := range data.Prices {
		if len(p) < 2 {
			continue
		}
		ts := time.UnixMilli(int64(p[0])).UTC()
		out = append(out, BTCMarketChartDay{Date: ts, Close: p[1]})
	}
	return out, nil
}
