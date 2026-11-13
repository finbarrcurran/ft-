package market

import (
	"context"
	"fmt"
	"ft/internal/health"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CoinGecko call accounting (SC-jordi46 Part A, v1.98.0). Every call FT makes to CoinGecko goes through
// cgGetJSON, which records it in provider_daily: keyless calls under "coingecko" (the name the health
// panel already used), calls that carry the Demo key under "coingecko_key" (the 10,000 a month
// allowance). A call is counted when it is made, so a 429 and its retry are two calls.
const (
	ProviderCoinGecko    = "coingecko"
	ProviderCoinGeckoKey = "coingecko_key"
)

// cgBackoffUnit is the wait before the first retry of a 429 (doubling per attempt would be too slow for
// the sequential daily job; it adds one unit each time). Tests shorten it.
var cgBackoffUnit = 15 * time.Second

func coingeckoKey() string { return os.Getenv("FT_COINGECKO_API_KEY") }

// cgGetJSON GETs a CoinGecko URL. keyed sends the Demo key when one is configured (the call is then
// counted against the key). A 429 is retried up to retries times after backoff[i]; each attempt is one
// counted call.
func cgGetJSON(ctx context.Context, rawURL string, keyed bool, retries int, out any) error {
	key := ""
	if keyed {
		key = coingeckoKey()
	}
	provider := ProviderCoinGecko
	var hdr map[string]string
	if key != "" {
		provider = ProviderCoinGeckoKey
		hdr = map[string]string{"x-cg-demo-api-key": key}
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = redactQuery(httpGetJSONHeaders(ctx, rawURL, hdr, out))
		health.Record(ctx, provider, err)
		if err == nil || attempt >= retries || !strings.Contains(err.Error(), "http 429") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * cgBackoffUnit):
		}
	}
}

// redactedErr is an error whose text has its URL query strings removed; it still unwraps to the original
// (so errors.Is for a timeout or cancellation keeps working).
type redactedErr struct {
	msg string
	err error
}

func (e *redactedErr) Error() string { return e.msg }
func (e *redactedErr) Unwrap() error { return e.err }

var queryRe = regexp.MustCompile(`\?[^"\s)]*`)

// redactQuery removes the query string of any URL from an error's text (SC-70, FT-127): a failed
// /coins/markets call otherwise puts the whole list of held coin ids in the Diagnostics provider error.
func redactQuery(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if red := queryRe.ReplaceAllString(msg, "?<redacted>"); red != msg {
		return &redactedErr{msg: red, err: err}
	}
	return err
}

// 7d / 30d changes: one keyed /coins/markets call for every held coin, at most once an hour. The
// changes barely move in 15 minutes, so the 15-minute refresh re-applies the cached values.
const changesTTL = time.Hour

var changesCache = struct {
	sync.Mutex
	at  time.Time
	d7  map[string]float64 // coingecko id -> 7d %
	d30 map[string]float64
}{}

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
	err := cgGetJSON(ctx, u.String(), false, 0, &prices)
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

	// 7d / 30d from one /coins/markets call (cached for an hour); a failure keeps the cached values.
	d7, d30 := refreshChanges(ctx, idsForRequest)
	for _, q := range out {
		id := SymbolToGeckoID[q.Symbol]
		if v, ok := d7[id]; ok {
			v := v
			q.Change7dPct = &v
		}
		if v, ok := d30[id]; ok {
			v := v
			q.Change30dPct = &v
		}
	}

	return out
}

type geckoMarketRow struct {
	ID  string   `json:"id"`
	D7  *float64 `json:"price_change_percentage_7d_in_currency"`
	D30 *float64 `json:"price_change_percentage_30d_in_currency"`
}

// refreshChanges returns the 7d and 30d changes for the ids, calling /coins/markets (keyed, ONE call for
// all of them) when the cache is older than changesTTL or lacks an id.
func refreshChanges(ctx context.Context, ids []string) (d7, d30 map[string]float64) {
	changesCache.Lock()
	defer changesCache.Unlock()
	fresh := time.Since(changesCache.at) < changesTTL && changesCache.d7 != nil
	if fresh {
		for _, id := range ids {
			if _, ok := changesCache.d7[id]; !ok {
				fresh = false
				break
			}
		}
	}
	if !fresh {
		u, _ := url.Parse("https://api.coingecko.com/api/v3/coins/markets")
		q := u.Query()
		q.Set("vs_currency", "usd")
		q.Set("ids", strings.Join(ids, ","))
		q.Set("price_change_percentage", "7d,30d")
		q.Set("per_page", "250")
		q.Set("sparkline", "false")
		u.RawQuery = q.Encode()
		var rows []geckoMarketRow
		if err := cgGetJSON(ctx, u.String(), true, 1, &rows); err != nil {
			slog.Warn("coingecko coins/markets (7d/30d) failed; keeping cached changes", "err", err)
		} else {
			n7, n30 := map[string]float64{}, map[string]float64{}
			for _, r := range rows {
				if r.D7 != nil {
					n7[r.ID] = *r.D7
				}
				if r.D30 != nil {
					n30[r.ID] = *r.D30
				}
			}
			changesCache.at, changesCache.d7, changesCache.d30 = time.Now(), n7, n30
		}
	}
	return changesCache.d7, changesCache.d30
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
	if err := cgGetJSON(ctx, u.String(), true, 3, &data); err != nil { // keyed: the keyless pool 429s after about five calls in a row
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

// CryptoProfile is the shape returned by /api/lookup/ticker for crypto.
type CryptoProfile struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name,omitempty"`
	IsCore   bool   `json:"isCore"`
	Category string `json:"category,omitempty"`
	Source   string `json:"source"`
}

// LookupCryptoBySymbol resolves a symbol (BTC, ETH, SOL…) to a profile via
// the SymbolToGeckoID map + CoinGecko's /coins/{id}. Spec 12 D7.
//
// We avoid /coins/list (1000+ rows, slow) and stick to the curated
// SymbolToGeckoID table — that's the only set Fin holds anyway, and the
// table is trivially extensible.
func LookupCryptoBySymbol(ctx context.Context, symbol string) (p *CryptoProfile, retErr error) {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	id, ok := SymbolToGeckoID[sym]
	if !ok {
		return nil, fmt.Errorf("unknown symbol %s (extend SymbolToGeckoID)", sym)
	}
	u := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/%s?localization=false&tickers=false&market_data=false&community_data=false&developer_data=false&sparkline=false",
		url.PathEscape(id))
	var resp struct {
		Name       string   `json:"name"`
		Symbol     string   `json:"symbol"`
		Categories []string `json:"categories"`
	}
	if err := cgGetJSON(ctx, u, false, 0, &resp); err != nil {
		return nil, err
	}
	out := &CryptoProfile{
		Symbol: sym,
		Name:   resp.Name,
		IsCore: sym == "BTC" || sym == "ETH",
		Source: "coingecko",
	}
	if len(resp.Categories) > 0 {
		out.Category = resp.Categories[0]
	}
	return out, nil
}
