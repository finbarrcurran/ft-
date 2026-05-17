// Spec 12 D7 — Smart-input lookup endpoint.
//
//   GET /api/lookup/ticker?q=NVDA[&kind=stock]    → TickerProfile
//   GET /api/lookup/ticker?q=Bitcoin&kind=crypto  → CryptoProfile
//
// Stocks use Yahoo's quoteSummary (summaryProfile + price + financialData)
// for name / sector / industry / currency / analyst targets. Crypto uses
// CoinGecko's /coins/{id} via the SymbolToGeckoID table.
//
// Free-text → ticker uses Yahoo's /v1/finance/search (best-effort; falls
// back to treating the query as a raw ticker).
//
// Rate-limit posture: callers are interactive (Edit modal debounced 300ms,
// or one call per row in the import preview). No server-side caching — if
// Yahoo throttles, the UI shows "lookup unavailable" and lets the user
// continue typing manually.

package server

import (
	"ft/internal/market"
	"net/http"
	"strings"
)

func (s *Server) handleLookupTicker(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q required")
		return
	}
	kind := strings.ToLower(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "stock"
	}

	if kind == "crypto" {
		p, err := market.LookupCryptoBySymbol(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, p)
		return
	}

	// Stock path. If `q` looks like a freeform name (contains spaces or
	// non-tickerish chars), run the search to resolve to a symbol first.
	ticker := q
	if needsSearch(q) {
		if sym, _, err := market.SearchYahooTicker(r.Context(), q); err == nil {
			ticker = sym
		}
	}
	p, err := market.FetchYahooProfile(r.Context(), ticker)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// needsSearch returns true when the query looks like a company name rather
// than a ticker — at which point Yahoo's /search resolves better than
// passing the freeform string to /quoteSummary.
func needsSearch(q string) bool {
	if len(q) > 8 {
		return true
	}
	for _, c := range q {
		if c == ' ' {
			return true
		}
	}
	return false
}
