// Spec 9b D11 — Macro Economics cards.

package server

import (
	"ft/internal/macro"
	"net/http"
)

// GET /api/macro?upcoming_days=14&recent_days=7
func (s *Server) handleMacro(w http.ResponseWriter, r *http.Request) {
	upcoming := macro.Upcoming(parseIntDefault(r.URL.Query().Get("upcoming_days"), 14))
	recent := macro.Recent(parseIntDefault(r.URL.Query().Get("recent_days"), 7))
	writeJSON(w, http.StatusOK, map[string]any{
		"upcoming": upcoming,
		"recent":   recent,
		"loadedAt": macro.LoadedAt().Format("2006-01-02T15:04:05Z"),
	})
}

func parseIntDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	n := 0
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return def
	}
	return n
}
