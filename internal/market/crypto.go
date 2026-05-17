package market

import (
	"context"
	"fmt"
	"ft/internal/health"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SymbolToGeckoID maps ticker symbols to CoinGecko coin IDs.
// Ported verbatim from lib/market-data/crypto.ts. Add new entries as needed;
// unknown symbols are silently skipped.
var SymbolToGeckoID = map[string]string{
	"BTC":  "bitcoin",
	"ETH":  "ethereum",
	"XRP":  "ripple",
	"AVAX": "avalanche-2",
	"ADA":  "cardano",
	"SUI":  "sui",
	"SOL":  "solana",
	"ARB":  "arbitrum",
	"POL":  "polygon-ecosystem-token",
	"OP":   "optimism",
	"LINK": "chainlink",
	"HBAR": "hedera-hashgraph",
	// Volt (XVM) is a small-cap; CoinGecko id varies if listed at all
	"XVM": "venus-xvs",
}

// FetchCryptoQuotes returns one CryptoQuote per known symbol. 24h change comes
// from /simple/price; 7d and 30d changes are filled in best-effort via per-coin
// /market_chart calls in parallel.
func FetchCryptoQuotes(ctx context.Context, symbols []string) []*CryptoQuote {
	type priceEntry struct {
		USD          float64 `json:"usd"`
		EUR          float64 `json:"eur"`
		USD24hChange float64 `json:"usd_24h_change"`
	}

	// Build the symbol → id lookup and the comma-joined id list for /simple/price.
	idsForRequest := make([]string, 0, len(symbols))
	idToSymbol := map[string]string{}
	for _, s := range symbols {
		sym := strings.ToUpper(s)
		id, ok := SymbolToGeckoID[sym]
		if !ok {
			continue
		}
		idsForRequest = append(idsForRequest, id)
		idToSymbol[id] = sym
	}
	if len(idsForRequest) == 0 {
		return nil
	}

	u, _ := url.Parse("https://api.coingecko.com/api/v3/simple/price")
	q := u.Query()
	q.Set("ids", strings.Join(idsForRequest, ","))
	q.Set("vs_currencies", "usd,eur")
	q.Set("include_24hr_change", "true")
	u.RawQuery = q.Encode()

	var prices map[string]priceEntry
	err := httpGetJSON(ctx, u.String(), &prices)
	health.Record(ctx, "coingecko", err)
	if err != nil {
		slog.Warn("coingecko simple/price failed", "err", err)
		return nil
	}

	fetchedAt := time.Now().UTC()
	out := make([]*CryptoQuote, 0, len(idsForRequest))
	for id, p := range prices {
		sym, ok := idToSymbol[id]
		if !ok {
			continue
		}
		out = append(out, &CryptoQuote{
			Symbol:       sym,
			PriceUSD:     p.USD,
			PriceEUR:     p.EUR,
			Change24hPct: p.USD24hChange,
			FetchedAt:    fetchedAt,
		})
	}

	// Best-effort 7d / 30d in parallel; failures are dropped silently.
	var wg sync.WaitGroup
	for _, q := range out {
		q := q
		id := SymbolToGeckoID[q.Symbol]
		if id == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c7, err := fetchPctChange(ctx, id, 7); err == nil {
				q.Change7dPct = &c7
			}
			if c30, err := fetchPctChange(ctx, id, 30); err == nil {
				q.Change30dPct = &c30
			}
		}()
	}
	wg.Wait()

	return out
}

type geckoMarketChart struct {
	Prices [][]float64 `json:"prices"` // [unix_ms, price]
}

// FetchCryptoDailyCloses returns trailing daily closes for a single crypto
// symbol. Used by the Spec 3 D8 sparkline cron. Empty result if the symbol
// isn't in SymbolToGeckoID. Each DailyClose carries an ISO 'YYYY-MM-DD' date.
func FetchCryptoDailyCloses(ctx context.Context, symbol string, days int) ([]DailyClose, error) {
	id, ok := SymbolToGeckoID[strings.ToUpper(symbol)]
	if !ok {
		return nil, fmt.Errorf("unknown crypto symbol %q", symbol)
	}
	if days <= 0 {
		days = 30
	}
	u, _ := url.Parse(fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s/market_chart", id))
	q := u.Query()
	q.Set("vs_currency", "usd")
	q.Set("days", fmt.Sprintf("%d", days))
	q.Set("interval", "daily")
	u.RawQuery = q.Encode()

	var data geckoMarketChart
	if err := httpGetJSON(ctx, u.String(), &data); err != nil {
		return nil, err
	}
	out := make([]DailyClose, 0, len(data.Prices))
	seen := map[string]bool{}
	for _, row := range data.Prices {
		if len(row) < 2 || row[1] <= 0 {
			continue
		}
		ts := int64(row[0] / 1000) // unix ms → s
		date := time.Unix(ts, 0).UTC().Format("2006-01-02")
		// CoinGecko sometimes returns two points for the same day; dedupe to last.
		if seen[date] {
			out[len(out)-1].Close = row[1]
			continue
		}
		seen[date] = true
		out = append(out, DailyClose{Date: date, Close: row[1]})
	}
	return out, nil
}

func fetchPctChange(ctx context.Context, geckoID string, days int) (float64, error) {
	u, _ := url.Parse(fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s/market_chart", geckoID))
	q := u.Query()
	q.Set("vs_currency", "usd")
	q.Set("days", fmt.Sprintf("%d", days))
	q.Set("interval", "daily")
	u.RawQuery = q.Encode()
	var data geckoMarketChart
	if err := httpGetJSON(ctx, u.String(), &data); err != nil {
		return 0, err
	}
	if len(data.Prices) < 2 {
		return 0, fmt.Errorf("not enough data")
	}
	first := data.Prices[0][1]
	last := data.Prices[len(data.Prices)-1][1]
	if first == 0 {
		return 0, fmt.Errorf("zero first price")
	}
	return ((last - first) / first) * 100, nil
}
