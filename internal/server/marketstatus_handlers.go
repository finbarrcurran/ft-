package server

import (
	"ft/internal/marketdata"
	"net/http"
	"sort"
	"time"
)

// GET /api/marketstatus
//
// Back-compat shape (Spec 2 D5). Returns the US snapshot in the original
// {us, asOf} structure so any older clients keep working.
func (s *Server) handleMarketStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	us := marketdata.USMarketStatus(now)
	writeJSON(w, http.StatusOK, map[string]any{
		"us":   us,
		"asOf": now.Format(time.RFC3339),
	})
}

// GET /api/marketstatus/all  (Spec 5 D4)
//
// Returns every supported exchange's status PLUS a `summary` block that the
// top-bar pill can show without further client logic:
//
//	{
//	  "asOf": "...",
//	  "summary": {
//	    "anyOpen": true,
//	    "primaryExchange": "US",            // earliest-closing open market, else earliest-opening closed
//	    "primaryLabel": "US Markets",
//	    "primaryOpen": true,
//	    "primaryNextChange": "...",
//	    "primaryNextChangeKind": "close"
//	  },
//	  "exchanges": [
//	    {"exchange": "US", "name": "US Markets", "open": true, ...},
//	    ...
//	  ]
//	}
func (s *Server) handleMarketStatusAll(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	all := marketdata.AllStatuses(now)
	summary := pickPrimary(all)
	writeJSON(w, http.StatusOK, map[string]any{
		"asOf":      now.Format(time.RFC3339),
		"summary":   summary,
		"exchanges": all,
	})
}

// pickPrimary chooses the headline market for the top-bar pill:
//   - If any are open: the one closing soonest (NextChange ASC).
//   - Else: the one opening soonest (NextChange ASC).
func pickPrimary(all []marketdata.MarketStatus) map[string]any {
	anyOpen := false
	for _, s := range all {
		if s.Open {
			anyOpen = true
			break
		}
	}
	// Sort a copy by NextChange ascending; partition by open-ness.
	sorted := make([]marketdata.MarketStatus, len(all))
	copy(sorted, all)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].NextChange.Before(sorted[j].NextChange)
	})

	var pick marketdata.MarketStatus
	if anyOpen {
		for _, s := range sorted {
			if s.Open {
				pick = s
				break
			}
		}
	} else {
		// All closed → earliest opening.
		for _, s := range sorted {
			if !s.NextChange.IsZero() {
				pick = s
				break
			}
		}
	}
	return map[string]any{
		"anyOpen":               anyOpen,
		"primaryExchange":       pick.Exchange,
		"primaryLabel":          pick.Name,
		"primaryOpen":           pick.Open,
		"primaryOnBreak":        pick.OnBreak,
		"primaryNextChange":     pick.NextChange,
		"primaryNextChangeKind": pick.NextChangeKind,
		"primaryTZName":         pick.TZName,
	}
}
