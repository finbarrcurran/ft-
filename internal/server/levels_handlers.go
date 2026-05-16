// Spec 9c — per-holding levels endpoint.
//
// GET /api/holdings/{kind}/{id}/levels   (cookie OR token auth)
//
// Returns the S/R candidates + suggested SL/TP/R-multiples for a holding.
// Used by:
//   - The Edit modal's one-click "Use $___" buttons next to S/R inputs
//   - The Position Size Calculator panel
//   - The Telegram bot's `/levels TICKER` command

package server

import (
	"encoding/json"
	"errors"
	"ft/internal/store"
	"ft/internal/technicals"
	"net/http"
	"strings"
)

type levelsResponse struct {
	Ticker         string         `json:"ticker"`
	Kind           string         `json:"kind"`
	CurrentPrice   *float64       `json:"currentPrice,omitempty"`
	ATRWeekly      *float64       `json:"atrWeekly,omitempty"`
	VolTier        string         `json:"volTier"`          // user-set
	VolTierAuto    *string        `json:"volTierAuto,omitempty"`

	Support1       *float64       `json:"support1,omitempty"`
	Support2       *float64       `json:"support2,omitempty"`
	Resistance1    *float64       `json:"resistance1,omitempty"`
	Resistance2    *float64       `json:"resistance2,omitempty"`

	Suggestions    levelsSuggestions `json:"suggestions"`
	Candidates     levelsCandidates  `json:"candidates"`
	Stage          string         `json:"stage"`
}

type levelsSuggestions struct {
	SL              float64 `json:"sl,omitempty"`
	TP1             float64 `json:"tp1,omitempty"`
	TP2             float64 `json:"tp2,omitempty"`
	RMultipleTP1    float64 `json:"rMultipleTp1,omitempty"`
	RMultipleTP2    float64 `json:"rMultipleTp2,omitempty"`
	UsingTier       string  `json:"usingTier"`         // tier the math used (auto || manual || 'medium' default)
	UsingTierSource string  `json:"usingTierSource"`   // 'user' | 'auto' | 'default'
}

type levelsCandidates struct {
	Supports    []levelCandidate `json:"supports"`
	Resistances []levelCandidate `json:"resistances"`
}

type levelCandidate struct {
	Price       float64 `json:"price"`
	Touches     int     `json:"touches"`
	LastTouchAt string  `json:"lastTouchAt"`
	Score       float64 `json:"score"`
}

// GET /api/holdings/stocks/{id}/levels  + crypto sibling
func (s *Server) handleStockLevels(w http.ResponseWriter, r *http.Request) {
	s.handleLevels(w, r, "stock")
}
func (s *Server) handleCryptoLevels(w http.ResponseWriter, r *http.Request) {
	s.handleLevels(w, r, "crypto")
}

func (s *Server) handleLevels(w http.ResponseWriter, r *http.Request, kind string) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var ticker string
	var resp levelsResponse
	resp.Kind = kind
	resp.Stage = "pre_tp1"

	if kind == "stock" {
		h, err := s.store.GetStockHolding(r.Context(), userID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if mapStoreError(w, err) {
			return
		}
		if h.Ticker != nil {
			ticker = strings.ToUpper(*h.Ticker)
		}
		resp.Ticker = ticker
		resp.CurrentPrice = h.CurrentPrice
		resp.ATRWeekly = h.ATRWeekly
		resp.VolTierAuto = h.VolTierAuto
		resp.Support1 = h.Support1
		resp.Support2 = h.Support2
		resp.Resistance1 = h.Resistance1
		resp.Resistance2 = h.Resistance2
		resp.Stage = h.Stage
		// stock has no user vol_tier — auto is canonical.
	} else {
		h, err := s.store.GetCryptoHolding(r.Context(), userID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if mapStoreError(w, err) {
			return
		}
		ticker = strings.ToUpper(h.Symbol)
		resp.Ticker = ticker
		resp.CurrentPrice = h.CurrentPriceUSD
		resp.ATRWeekly = h.ATRWeekly
		resp.VolTier = h.VolTier
		resp.VolTierAuto = h.VolTierAuto
		resp.Support1 = h.Support1
		resp.Support2 = h.Support2
		resp.Resistance1 = h.Resistance1
		resp.Resistance2 = h.Resistance2
		resp.Stage = h.Stage
	}

	// Resolve the tier that the SL math should use: user manual wins,
	// else auto, else 'medium' default.
	tier := resp.VolTier
	source := "user"
	if tier == "" {
		if resp.VolTierAuto != nil && *resp.VolTierAuto != "" {
			tier = *resp.VolTierAuto
			source = "auto"
		} else {
			tier = "medium"
			source = "default"
		}
	}
	resp.Suggestions.UsingTier = tier
	resp.Suggestions.UsingTierSource = source

	atr := 0.0
	if resp.ATRWeekly != nil {
		atr = *resp.ATRWeekly
	}
	// SL = support_1 − N × ATR
	if resp.Support1 != nil && atr > 0 {
		resp.Suggestions.SL = technicals.SuggestedSL(*resp.Support1, atr, tier)
	}
	if resp.Resistance1 != nil && atr > 0 {
		resp.Suggestions.TP1 = technicals.SuggestedTP1(*resp.Resistance1, atr)
	}
	if resp.Resistance2 != nil && atr > 0 {
		resp.Suggestions.TP2 = technicals.SuggestedTP2(*resp.Resistance2, atr)
	}

	// R-multiples vs current price as the assumed entry (UI overrides
	// with a user-input entry for "what-if" pricing).
	if resp.CurrentPrice != nil && resp.Suggestions.SL > 0 {
		entry := *resp.CurrentPrice
		if resp.Suggestions.TP1 > 0 {
			resp.Suggestions.RMultipleTP1 = technicals.RMultiple(entry, resp.Suggestions.SL, resp.Suggestions.TP1)
		}
		if resp.Suggestions.TP2 > 0 {
			resp.Suggestions.RMultipleTP2 = technicals.RMultiple(entry, resp.Suggestions.SL, resp.Suggestions.TP2)
		}
	}

	// Candidates from sr_candidates table.
	cands, _ := s.store.GetSRCandidates(r.Context(), ticker, kind)
	for _, c := range cands {
		lc := levelCandidate{Price: c.Price, Touches: c.Touches, LastTouchAt: c.LastTouchAt, Score: c.Score}
		switch c.LevelType {
		case "support":
			resp.Candidates.Supports = append(resp.Candidates.Supports, lc)
		case "resistance":
			resp.Candidates.Resistances = append(resp.Candidates.Resistances, lc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
