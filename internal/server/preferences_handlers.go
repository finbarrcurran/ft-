// Spec 6 D2 — preference key/value handlers.
//
// Routes:
//
//	GET    /api/preferences            list all
//	GET    /api/preferences/{key}      get one
//	PUT    /api/preferences/{key}      set one (body: {"value": "..."})
//
// All require cookie auth (single-user; no bearer surface).

package server

import (
	"errors"
	"ft/internal/store"
	"net/http"
	"strings"
)

// ----- list / get --------------------------------------------------------

func (s *Server) handleListPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.store.ListPreferences(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preferences": prefs})
}

func (s *Server) handleGetPreference(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	v, err := s.store.GetPreference(r.Context(), key)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": nil})
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": v})
}

// ----- set ---------------------------------------------------------------

type preferenceReq struct {
	Value string `json:"value"`
}

func (s *Server) handleSetPreference(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing key")
		return
	}
	var req preferenceReq
	if !decodeJSON(r, w, &req) {
		return
	}
	// Per-key validation so we don't accept garbage into the store.
	if !validPreferenceValue(key, req.Value) {
		writeError(w, http.StatusBadRequest, "value not allowed for key "+key)
		return
	}
	if err := s.store.SetPreference(r.Context(), key, req.Value); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": req.Value})
}

// validPreferenceValue checks per-key invariants. Unknown keys accept any
// non-empty short string (~256 chars).
func validPreferenceValue(key, value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	switch key {
	case "heatmap_mode":
		// Backlog polish — "pnl" surfaces tile-sized-by-P&L mode.
		return value == "market_cap" || value == "my_holdings" || value == "pnl"
	case "news_filter_mode":
		return value == "all" || value == "mine"
	case "regime_skip_week":
		// ISO week format like "2026-W20" or "skip" — short and bounded.
		return len(value) <= 24
	case "alerts_snooze_until":
		// Backlog polish — unix-seconds timestamp as a string. "0" clears.
		return len(value) > 0 && len(value) <= 16
	case "cash_balance_usd", "cash_balance_eur":
		// Spec 12 D2 — non-negative decimal string. Generous cap for ultra-
		// wealthy users (16 chars handles up to $9.9 trillion).
		if len(value) == 0 || len(value) > 16 {
			return false
		}
		// Permit digits + at most one decimal point. "0" is valid.
		dot := 0
		for _, c := range value {
			if c == '.' {
				dot++
				if dot > 1 {
					return false
				}
				continue
			}
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	case "focused_exchange":
		// Spec 12 D3 — one of the 7 supported markets.
		switch value {
		case "US", "LSE", "EURONEXT", "XETRA", "TSE", "HKEX", "B3":
			return true
		}
		return false
	case "pnl_currency":
		// Spec 12 D5g — toggle for stocks-table P&L display.
		return value == "USD" || value == "EUR"
	case "jordi_current_sector_read":
		// Spec 9f D7 — free-text macro strip note. Bounded to avoid abuse.
		return len(value) > 0 && len(value) <= 500
	}
	// Spec 9c.1 — llm_* keys are bounded TEXT/bool/number values; accept
	// anything that survives the generic length check above.
	if strings.HasPrefix(key, "llm_") {
		return true
	}
	// Spec 9c risk-cap keys.
	if strings.HasPrefix(key, "risk_") {
		return true
	}
	return true
}
