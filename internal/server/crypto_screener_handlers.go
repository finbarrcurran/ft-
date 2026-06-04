// SC-21 — Crypto market screener endpoint.
//
// GET /api/crypto-screener
//
// Returns the full top-250 CoinGecko universe enriched with DefiLlama TVL +
// category + a held/watchlist overlay for the calling user. Sorting, filtering
// (category / held-watchlist / top-N) and column selection all happen client
// side over this single payload — the data is identical regardless of view, so
// one cached pull serves every sort.
//
// Market-data only: no framework scores / cascade state (SC-21 §scope, S-B1).

package server

import (
	"net/http"
	"sort"
	"strings"

	"ft/internal/cryptoscreener"
)

// cryptoScreenerRow is the API shape — the service Row plus the per-user
// overlay flags.
type cryptoScreenerRow struct {
	cryptoscreener.Row
	Held        bool `json:"held"`
	OnWatchlist bool `json:"onWatchlist"`
}

func (s *Server) handleCryptoScreener(w http.ResponseWriter, r *http.Request) {
	if s.cryptoScreener == nil {
		writeError(w, http.StatusServiceUnavailable, "crypto screener not configured")
		return
	}
	userID, _ := userIDFromContext(r.Context())

	rows, fetchedAt, stale, err := s.cryptoScreener.Markets(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "crypto market data unavailable: "+err.Error())
		return
	}

	// Per-user overlay sets (crypto holdings + crypto watchlist), matched by
	// upper-cased symbol.
	held := map[string]bool{}
	if holdings, err := s.store.ListCryptoHoldings(r.Context(), userID); err == nil {
		for _, h := range holdings {
			if sym := strings.ToUpper(strings.TrimSpace(h.Symbol)); sym != "" {
				held[sym] = true
			}
		}
	}
	onWatch := map[string]bool{}
	if wl, err := s.store.ListWatchlist(r.Context(), userID); err == nil {
		for _, e := range wl {
			if e.Kind == "crypto" {
				onWatch[strings.ToUpper(strings.TrimSpace(e.Ticker))] = true
			}
		}
	}

	out := make([]cryptoScreenerRow, 0, len(rows))
	catSet := map[string]bool{}
	for _, row := range rows {
		catSet[row.Category] = true
		out = append(out, cryptoScreenerRow{
			Row:         row,
			Held:        held[row.Symbol],
			OnWatchlist: onWatch[row.Symbol],
		})
	}

	categories := make([]string, 0, len(catSet))
	for c := range catSet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	writeJSON(w, http.StatusOK, map[string]any{
		"rows":       out,
		"categories": categories,
		"count":      len(out),
		"fetchedAt":  fetchedAt.Format("2006-01-02T15:04:05Z07:00"),
		"stale":      stale,
	})
}
