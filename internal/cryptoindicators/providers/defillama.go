package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DefiLlama provides the stablecoin total supply Pal uses as a "dry powder
// waiting inside crypto" indicator. Endpoint:
//
//   https://stablecoins.llama.fi/stablecoins?includePrices=false
//
// Response shape (per docs as of 2024): an array of stablecoin objects,
// each with .chainCirculating.{chain}.peggedUSD numerics PLUS, on the
// root object, a `totalSupply` field which is what we want. We compute
// 4-week rate-of-change from local snapshot history (no historical
// endpoint in their free API).
type DefiLlamaClient struct {
	HTTP *http.Client
}

func NewDefiLlamaClient() *DefiLlamaClient {
	return &DefiLlamaClient{HTTP: &http.Client{Timeout: 25 * time.Second}}
}

// FetchStablecoinSupply returns the current total circulating supply of
// stablecoins (USD-pegged, all chains) in USD billions PLUS the 4-week
// rate-of-change computed from DefiLlama's history endpoint. This means
// the indicator scores correctly on day 1 — no need to wait for local
// snapshot accumulation.
//
// Endpoints used:
//   /stablecoincharts/all → daily total over time (current + 4wk-ago)
//
// (The /stablecoins endpoint also exists for current-only; we use the
// chart endpoint because it gives us both current and historical in one
// call.)
func (c *DefiLlamaClient) FetchStablecoinSupply(ctx context.Context) Reading {
	body, _, err := doWithRetry(ctx, c.HTTP,
		"https://stablecoins.llama.fi/stablecoincharts/all")
	if err != nil {
		return Reading{Err: fmt.Sprintf("defillama: %v", err)}
	}
	// Response shape: array of {date: "<unix-secs-string>", totalCirculatingUSD: {peggedUSD: X}}
	var data []struct {
		Date                string `json:"date"`
		TotalCirculatingUSD struct {
			PeggedUSD float64 `json:"peggedUSD"`
		} `json:"totalCirculatingUSD"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return Reading{Err: fmt.Sprintf("defillama decode: %v", err)}
	}
	if len(data) == 0 {
		return Reading{Err: "defillama: empty stablecoincharts response"}
	}
	// Latest entry is at the END of the array (chronological). 28 days back
	// is at index len-28-1 (if available).
	latest := data[len(data)-1]
	latestUSD := latest.TotalCirculatingUSD.PeggedUSD
	if latestUSD <= 0 {
		return Reading{Err: "defillama: latest supply zero"}
	}
	latestB := latestUSD / 1e9

	out := Reading{Value: &latestB}

	// 4-week ROC.
	if len(data) > 29 {
		prior := data[len(data)-29]
		priorUSD := prior.TotalCirculatingUSD.PeggedUSD
		if priorUSD > 0 {
			roc := (latestUSD - priorUSD) / priorUSD * 100
			out.Trend4w = &roc
		}
	}
	return out
}
