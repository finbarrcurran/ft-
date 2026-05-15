package server

import (
	"ft/internal/marketdata"
	"net/http"
	"time"
)

// GET /api/marketstatus
//
// Returns the current US-markets state (Spec 2 D5). Multi-exchange detail
// lands in Spec 5. Public to authenticated users; cookie required.
//
// Response:
//
//	{
//	  "us": {
//	    "open": true,
//	    "nextChange": "2026-05-15T20:00:00Z",
//	    "nextChangeKind": "close"
//	  },
//	  "asOf": "2026-05-15T18:25:12Z"
//	}
//
// Client converts the ISO timestamp to local time for the countdown.
func (s *Server) handleMarketStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	us := marketdata.USMarketStatus(now)
	writeJSON(w, http.StatusOK, map[string]any{
		"us":   us,
		"asOf": now.Format(time.RFC3339),
	})
}
