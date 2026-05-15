// Package market is the home of the third-party data adapters: stock quotes
// (Yahoo Finance), crypto prices (CoinGecko), EUR/USD FX (Frankfurter), and
// the crypto Fear & Greed Index (alternative.me).
//
// Every adapter degrades gracefully: a network error, a non-2xx response, or
// a parse failure returns an empty/zero value plus an error, never panics.
// The refresh orchestrator decides whether a partial failure is OK or not.
package market

import "time"

// StockQuote is what we get back from Yahoo Finance for one ticker.
type StockQuote struct {
	Ticker     string
	Name       string
	Price      float64
	ChangePct  float64
	Currency   string
	Volume     int64
	MA50       *float64
	MA200      *float64
	RSI14      *float64 // computed client-side from history
	FetchedAt  time.Time
}

// CryptoQuote is one row from CoinGecko's /simple/price + market_chart.
type CryptoQuote struct {
	Symbol       string
	PriceUSD     float64
	PriceEUR     float64
	Change24hPct float64
	Change7dPct  *float64
	Change30dPct *float64
	FetchedAt    time.Time
}

// FXRate is Frankfurter's EUR→USD spot rate.
type FXRate struct {
	EURToUSD  float64
	FetchedAt time.Time
}

// FearGreed is alternative.me's crypto Fear & Greed Index value 0–100.
type FearGreed struct {
	Value          int
	Classification string // "Extreme Fear" / "Fear" / "Neutral" / "Greed" / "Extreme Greed"
	FetchedAt      time.Time
}
