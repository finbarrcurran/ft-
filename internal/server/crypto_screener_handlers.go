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
	// upper-cased symbol. Demo mode (SC-22 doctrine: portfolio data is never served in demo mode,
	// enforced server-side) skips them entirely, so no row carries a held or watchlist flag.
	demo := s.demoModeOn(r.Context())
	held := map[string]bool{}
	onWatch := map[string]bool{}
	if !demo {
		if holdings, err := s.store.ListCryptoHoldings(r.Context(), userID); err == nil {
			for _, h := range holdings {
				if sym := strings.ToUpper(strings.TrimSpace(h.Symbol)); sym != "" {
					held[sym] = true
				}
			}
		}
		if wl, err := s.store.ListWatchlist(r.Context(), userID); err == nil {
			for _, e := range wl {
				if e.Kind == "crypto" {
					onWatch[strings.ToUpper(strings.TrimSpace(e.Ticker))] = true
				}
			}
		}
	}

	out, catSet := overlayCryptoRows(rows, held, onWatch)

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

// handleCryptoIndex serves the reference list of an index's current members (Jordi-46 Phase 1):
// GET /api/crypto-index/visser46. Read-only reference data (constituents, verticals, ids); it carries no
// holdings, watchlist or broker data, so demo mode has nothing to mask. No prices: the screener has those.
func (s *Server) handleCryptoIndex(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key != "visser46" {
		writeError(w, http.StatusNotFound, "unknown index")
		return
	}
	members, err := s.store.ListCryptoIndexMembers(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	verticals := map[string]int{}
	tokens, stocks, listVersion := 0, 0, ""
	for _, m := range members {
		verticals[m.Vertical]++
		listVersion = m.ListVersion
		if m.AssetKind == "stock" {
			stocks++
		} else {
			tokens++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"indexKey": key, "listVersion": listVersion, "count": len(members), "tokens": tokens, "stocks": stocks,
		"verticals": verticals, "members": members,
		"note": "Reference list only: constituents and vertical labels of the Visser-Labs 46-name index. Not a recommendation, not a score.",
	})
}

// overlayCryptoRows adds the per-user held / watchlist flags to the screener rows and collects the
// categories. Empty sets (demo mode) leave every flag false.
func overlayCryptoRows(rows []cryptoscreener.Row, held, onWatch map[string]bool) ([]cryptoScreenerRow, map[string]bool) {
	out := make([]cryptoScreenerRow, 0, len(rows))
	catSet := map[string]bool{}
	for _, row := range rows {
		catSet[row.Category] = true
		out = append(out, cryptoScreenerRow{Row: row, Held: held[row.Symbol], OnWatchlist: onWatch[row.Symbol]})
	}
	return out, catSet
}
