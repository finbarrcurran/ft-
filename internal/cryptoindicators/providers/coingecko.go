package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CoinGeckoClient fetches the extras Spec 9e needs that aren't already
// in FT's existing CoinGecko integration (BTC dominance, ETH/BTC ratio).
//
// /global gives total market cap + dominance per coin.
// /simple/price gives spot USD prices for an arbitrary id list.
//
// Free-tier limits: ~50 calls/min. Our cron makes 2 calls/day. Fine.
type CoinGeckoClient struct {
	HTTP *http.Client
}

func NewCoinGeckoClient() *CoinGeckoClient {
	return &CoinGeckoClient{HTTP: &http.Client{Timeout: 20 * time.Second}}
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
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.coingecko.com/api/v3/global", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /global: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Reading{Err: fmt.Sprintf("coingecko /global HTTP %d", resp.StatusCode)}
	}
	var data cgGlobalResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /global decode: %v", err)}
	}
	v, ok := data.Data.MarketCapPercentage["btc"]
	if !ok {
		return Reading{Err: "coingecko /global: btc dominance absent"}
	}
	return Reading{Value: &v}
}

type cgSimplePriceResp map[string]map[string]float64

// FetchETHBTCRatio returns ETH price / BTC price. Used for Cowen's
// rotation gauge. Trend computed from snapshot history later.
func (c *CoinGeckoClient) FetchETHBTCRatio(ctx context.Context) Reading {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Reading{Err: fmt.Sprintf("coingecko /simple/price: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Reading{Err: fmt.Sprintf("coingecko /simple/price HTTP %d", resp.StatusCode)}
	}
	var data cgSimplePriceResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
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
// daily snapshot to record BTC price alongside the composite for
// future backtest.
func (c *CoinGeckoClient) FetchBTCPriceUSD(ctx context.Context) (*float64, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd", nil)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var data cgSimplePriceResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if v, ok := data["bitcoin"]["usd"]; ok {
		return &v, nil
	}
	return nil, fmt.Errorf("bitcoin price not in response")
}
