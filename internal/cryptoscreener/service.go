// Package cryptoscreener builds the SC-21 crypto market screener: a
// sortable/filterable view over CoinGecko's top-250 universe, enriched with
// DefiLlama TVL. It is a market-structure screener ONLY — no framework scores,
// no cascade state (those live on thesis/detail views). Per SC-21 §scope.
//
// No new data provider: it reuses the existing CoinGecko client (with the
// SC-18 Demo key for rate headroom) and the existing DefiLlama client (which
// already serves stablecoin supply). TVL is enrich-and-flag — matched by
// gecko-id (chains) or symbol (DeFi protocols); unmatched coins keep a blank
// (nil) TVL rather than a guessed/zero value.
package cryptoscreener

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"ft/internal/cryptoindicators/providers"
)

// cacheTTL is how long an assembled top-250 snapshot is served before a
// refresh is attempted. The underlying data (mcap/price/TVL) moves slowly
// enough that 10 min is plenty, and it keeps us well inside the CoinGecko +
// DefiLlama rate budgets even with multiple dashboard opens.
const cacheTTL = 10 * time.Minute

// Row is one enriched screener row. All percentage + TVL fields are pointers
// because they are genuinely absent for some coins (CoinGecko returns null for
// a missing change window; BTC/XRP/memecoins have no TVL). Absent renders as a
// blank cell, never 0.
type Row struct {
	Rank        int      `json:"rank"`
	ID          string   `json:"id"` // gecko id
	Symbol      string   `json:"symbol"`
	Name        string   `json:"name"`
	PriceUSD    float64  `json:"priceUsd"`
	MarketCap   float64  `json:"marketCap"`
	Volume24h   float64  `json:"volume24h"`
	Change24h   *float64 `json:"change24h,omitempty"`
	Change7d    *float64 `json:"change7d,omitempty"`
	Change30d   *float64 `json:"change30d,omitempty"`
	VolToMcap   *float64 `json:"volToMcap,omitempty"` // liquidity / churn tell
	TVL         *float64 `json:"tvl,omitempty"`       // nullable — chains / DeFi only
	McapToTVL   *float64 `json:"mcapToTvl,omitempty"` // nullable — only when TVL present
	Category    string   `json:"category"`            // Chain | <DeFi category> | majors map | Other
	TVLMatched  bool     `json:"tvlMatched"`          // true when TVL came from DefiLlama (enrich-and-flag)
}

// majorsCategory is a small hand-maintained map for high-cap coins that have
// no DefiLlama TVL presence, so the category filter is still meaningful for
// the top of the book. Everything unmatched falls through to "Other" — we
// flag, we don't guess a DeFi category for a non-DeFi asset.
var majorsCategory = map[string]string{
	"BTC":   "Currency",
	"XRP":   "Payments",
	"BCH":   "Currency",
	"LTC":   "Currency",
	"DOGE":  "Memecoin",
	"SHIB":  "Memecoin",
	"PEPE":  "Memecoin",
	"BONK":  "Memecoin",
	"WIF":   "Memecoin",
	"FLOKI": "Memecoin",
	"USDT":  "Stablecoin",
	"USDC":  "Stablecoin",
	"DAI":   "Stablecoin",
	"FDUSD": "Stablecoin",
	"USDE":  "Stablecoin",
	"XMR":   "Privacy",
	"ZEC":   "Privacy",
	"XLM":   "Payments",
	"XVM":   "Other",
}

// Service assembles + caches the screener snapshot.
type Service struct {
	cg    *providers.CoinGeckoClient
	llama *providers.DefiLlamaClient

	mu        sync.RWMutex
	cache     []Row
	fetchedAt time.Time
}

// New wires the screener service to the shared CoinGecko + DefiLlama clients.
func New() *Service {
	return &Service{
		cg:    providers.NewCoinGeckoClient(),
		llama: providers.NewDefiLlamaClient(),
	}
}

// Markets returns the assembled top-250 snapshot. It serves a cached snapshot
// while fresh (< cacheTTL); on a stale cache it attempts one refresh and, if
// that fails, serves the last-good snapshot with stale=true (SC-18 serve-stale
// discipline). fetchedAt is when the served snapshot was assembled.
func (s *Service) Markets(ctx context.Context) (rows []Row, fetchedAt time.Time, stale bool, err error) {
	s.mu.RLock()
	cached, cachedAt := s.cache, s.fetchedAt
	s.mu.RUnlock()

	if cached != nil && time.Since(cachedAt) < cacheTTL {
		return cached, cachedAt, false, nil
	}

	fresh, ferr := s.refresh(ctx)
	if ferr != nil {
		if cached != nil {
			slog.Warn("cryptoscreener: refresh failed, serving stale", "err", ferr, "age", time.Since(cachedAt))
			return cached, cachedAt, true, nil
		}
		return nil, time.Time{}, false, ferr
	}
	now := time.Now().UTC()
	s.mu.Lock()
	s.cache, s.fetchedAt = fresh, now
	s.mu.Unlock()
	return fresh, now, false, nil
}

// refresh pulls a fresh top-250 + TVL snapshot and assembles the rows. TVL
// failures are non-fatal (the screener still renders market data with blank
// TVL) — only a markets-pull failure aborts.
func (s *Service) refresh(ctx context.Context) ([]Row, error) {
	markets, err := s.cg.FetchMarkets(ctx, 250)
	if err != nil {
		return nil, err
	}

	// TVL is best-effort enrichment. If DefiLlama is down we still serve the
	// market table (TVL columns blank).
	chainTVL, err := s.llama.FetchChainTVL(ctx)
	if err != nil {
		slog.Warn("cryptoscreener: chain TVL fetch failed", "err", err)
		chainTVL = map[string]float64{}
	}
	protoTVL, err := s.llama.FetchProtocolTVL(ctx)
	if err != nil {
		slog.Warn("cryptoscreener: protocol TVL fetch failed", "err", err)
		protoTVL = map[string]providers.ProtocolTVL{}
	}

	rows := make([]Row, 0, len(markets))
	for _, m := range markets {
		sym := strings.ToUpper(strings.TrimSpace(m.Symbol))
		row := Row{
			Rank:         m.MarketCapRank,
			ID:           m.ID,
			Symbol:       sym,
			Name:         m.Name,
			PriceUSD:     m.CurrentPrice,
			MarketCap:    m.MarketCap,
			Volume24h:    m.TotalVolume,
			Change24h:    m.Change24h,
			Change7d:     m.Change7d,
			Change30d:    m.Change30d,
		}

		// Volume / market-cap ratio (liquidity tell). Skip when mcap absent.
		if m.MarketCap > 0 {
			v := m.TotalVolume / m.MarketCap
			row.VolToMcap = &v
		}

		// TVL: chain (matched by gecko id) takes precedence over protocol
		// (matched by symbol), because an L1's chain TVL is the headline
		// figure for that asset.
		category := ""
		if tvl, ok := chainTVL[m.ID]; ok && tvl > 0 {
			row.TVL = &tvl
			row.TVLMatched = true
			category = "Chain"
		} else if p, ok := protoTVL[sym]; ok && p.TVL > 0 {
			tvl := p.TVL
			row.TVL = &tvl
			row.TVLMatched = true
			category = p.Category
			if category == "" {
				category = "DeFi"
			}
		}

		// Market-cap / TVL ratio — only meaningful when TVL is present.
		if row.TVL != nil && *row.TVL > 0 && m.MarketCap > 0 {
			r := m.MarketCap / *row.TVL
			row.McapToTVL = &r
		}

		// Category fallback: majors map, else "Other". Never guess a DeFi
		// category for an asset DefiLlama didn't match.
		if category == "" {
			if c, ok := majorsCategory[sym]; ok {
				category = c
			} else {
				category = "Other"
			}
		}
		row.Category = category

		rows = append(rows, row)
	}
	return rows, nil
}
