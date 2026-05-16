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
		return value == "market_cap" || value == "my_holdings"
	case "news_filter_mode":
		return value == "all" || value == "mine"
	case "regime_skip_week":
		// ISO week format like "2026-W20" or "skip" — short and bounded.
		return len(value) <= 24
	}
	return true
}
