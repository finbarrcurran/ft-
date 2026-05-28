// Spec 9e Phase 1 — Crypto Indicators Tab endpoints.
//
//	GET /api/crypto-indicators                  list all 11 indicators
//	GET /api/crypto-indicators/composite/latest live composite + bands
//
// Both cookie-only for v1. Bot-side read access (token auth) can be
// added later if the bot needs to surface the composite in /digest.

package server

import (
	"context"
	"encoding/json"
	"ft/internal/cryptoindicators"
	"ft/internal/cryptoindicators/providers"
	"io"
	"net/http"
	"time"
)

// contextWithTimeout derives a request-scoped context with the given
// timeout in seconds. Local helper so the handler stays readable.
func contextWithTimeout(r *http.Request, seconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), time.Duration(seconds)*time.Second)
}

// GET /api/crypto-indicators
func (s *Server) handleListCryptoIndicators(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeJSON(w, http.StatusOK, map[string]any{"indicators": []any{}, "available": false})
		return
	}
	rows, err := s.cryptoIndicators.ListIndicators(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"indicators": rows,
		"available":  true,
		"bucketLabels": map[string]string{
			"cowen":     cryptoindicators.BucketLabel("cowen"),
			"pal":       cryptoindicators.BucketLabel("pal"),
			"universal": cryptoindicators.BucketLabel("universal"),
			"sentiment": cryptoindicators.BucketLabel("sentiment"),
		},
	})
}

// GET /api/crypto-indicators/ism — read current ISM file (v1.8.3)
func (s *Server) handleReadISM(w http.ResponseWriter, r *http.Request) {
	f, err := providers.LoadISM(s.cfg.CryptoIndicatorsDataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if f == nil {
		writeJSON(w, http.StatusOK, map[string]any{"prints": []any{}, "uploaded": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prints": f.Prints, "uploaded": true})
}

// POST /api/crypto-indicators/ism — upload new ISM JSON (v1.8.3)
//
// Accepts either:
//   - multipart/form-data with field "file" (a .json file)
//   - application/json body with the ISMFile shape directly
//
// Validates via providers.ValidateISM, atomic-writes to disk, then
// triggers a refresh so the indicator lights up immediately.
func (s *Server) handleUploadISM(w http.ResponseWriter, r *http.Request) {
	var f providers.ISMFile
	ct := r.Header.Get("Content-Type")
	if hasCTPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "missing 'file' part")
			return
		}
		defer file.Close()
		b, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, "read file: "+err.Error())
			return
		}
		if err := json.Unmarshal(b, &f); err != nil {
			writeError(w, http.StatusBadRequest, "JSON parse: "+err.Error())
			return
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
			writeError(w, http.StatusBadRequest, "JSON parse: "+err.Error())
			return
		}
	}
	if err := providers.ValidateISM(&f); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := providers.SaveISM(s.cfg.CryptoIndicatorsDataDir, &f); err != nil {
		writeError(w, http.StatusInternalServerError, "save: "+err.Error())
		return
	}
	// Fire a refresh so pal_ism lights up immediately.
	if s.cryptoIndicators != nil {
		refresher := cryptoindicators.NewRefresher(s.cryptoIndicators, s.cfg.FREDApiKey, s.cfg.CryptoIndicatorsDataDir)
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		_ = refresher.RefreshAll(ctx)
		cancel()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(f.Prints)})
}

func hasCTPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// POST /api/crypto-indicators/refresh (v1.8.2)
//
// Manual trigger for the same refresh logic the daily 00:30 UTC cron runs.
// Useful for first-load when the user wants indicators to light up
// immediately rather than wait for the next cron tick.
func (s *Server) handleRefreshCryptoIndicators(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	refresher := cryptoindicators.NewRefresher(s.cryptoIndicators, s.cfg.FREDApiKey, s.cfg.CryptoIndicatorsDataDir)
	// 60s timeout — generous; in practice all calls together take ~5s.
	ctx, cancel := contextWithTimeout(r, 60)
	defer cancel()
	if err := refresher.RefreshAll(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/crypto-indicators/composite/latest
func (s *Server) handleCryptoIndicatorsComposite(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	snap, err := s.cryptoIndicators.LatestComposite(r.Context())
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"composite": snap,
		"bandLabel": cryptoindicators.BandLabel(snap.ActionBand),
	})
}

// GET /api/crypto-indicators/btc-history?days=730  (v1.12 — Phase 2b)
//
// Returns daily BTC closes from btc_price_history. Used by the BTC
// log-band chart at the top of the cowen bucket. Default 2 years
// (matches what's visually useful at the chart resolution we render).
func (s *Server) handleCryptoIndicatorsBTCHistory(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	days := 730
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := parsePositiveInt(d, 10000); err == nil {
			days = v
		}
	}
	rows, err := s.cryptoIndicators.BTCPriceHistory(r.Context(), days)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history": rows,
		"days":    days,
	})
}

// GET /api/crypto-indicators/composite/history?days=90  (v1.12 — Phase 2c)
//
// Returns daily composite snapshots for the hero gauge trend display.
func (s *Server) handleCryptoIndicatorsCompositeHistory(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := parsePositiveInt(d, 730); err == nil {
			days = v
		}
	}
	rows, err := s.cryptoIndicators.CompositeHistory(r.Context(), days)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history": rows,
		"days":    days,
	})
}

// GET /api/crypto-indicators/etf-flow/history?days=30  (v1.14)
//
// Returns daily BTC spot-ETF aggregate net flow from the Playwright-
// scraped Farside JSON cache. Used by the ETF flow bar chart at the top
// of the universal bucket. Returns empty array if the cache has not yet
// been populated by the daily 00:25 UTC cron.
func (s *Server) handleCryptoIndicatorsETFFlowHistory(w http.ResponseWriter, r *http.Request) {
	if s.cryptoIndicators == nil {
		writeError(w, http.StatusNotFound, "crypto indicators not initialised")
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := parsePositiveInt(d, 365); err == nil {
			days = v
		}
	}
	rows, err := s.cryptoIndicators.ETFFlowHistory(r.Context(), days)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"history": rows,
		"days":    days,
	})
}

// parsePositiveInt parses a query-string integer in (0, max]. Returns
// the integer or an error.
func parsePositiveInt(s string, max int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errInvalidInt
		}
		n = n*10 + int(c-'0')
		if n > max {
			return max, nil
		}
	}
	if n < 1 {
		return 0, errInvalidInt
	}
	return n, nil
}

var errInvalidInt = errInvalidIntT{}

type errInvalidIntT struct{}

func (errInvalidIntT) Error() string { return "not a positive integer" }
