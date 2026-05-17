// Spec 9c D9 — Percoco auto-score endpoint.
//
// POST /api/holdings/{kind}/{id}/autoscore
//   body: {entryPrice?, stopPrice?, tp1?, tp2?}  all optional;
//         falls back to suggested values from /levels
//
// Returns the 6 auto-scored question values + rationales. Frontend
// pre-fills these into the score modal; user fills Q7 (chart_cleanliness)
// and Q8 (catalyst_proximity) by hand.

package server

import (
	"errors"
	"ft/internal/store"
	"ft/internal/technicals"
	"net/http"
	"time"
)

type autoscoreReq struct {
	EntryPrice float64 `json:"entryPrice"`
	StopPrice  float64 `json:"stopPrice"`
	TP1        float64 `json:"tp1"`
	TP2        float64 `json:"tp2"`
}

type autoscoreResp struct {
	Scores     map[string]int    `json:"scores"`
	Rationales map[string]string `json:"rationales"`
	UsedEntry  float64           `json:"usedEntry"`
	UsedStop   float64           `json:"usedStop"`
	UsedTP1    float64           `json:"usedTp1"`
	UsedTP2    float64           `json:"usedTp2"`
	ATRWeekly  float64           `json:"atrWeekly"`
}

func (s *Server) handleStockAutoscore(w http.ResponseWriter, r *http.Request) {
	s.handleAutoscore(w, r, "stock")
}
func (s *Server) handleCryptoAutoscore(w http.ResponseWriter, r *http.Request) {
	s.handleAutoscore(w, r, "crypto")
}

func (s *Server) handleAutoscore(w http.ResponseWriter, r *http.Request, kind string) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req autoscoreReq
	// Body is optional — defaults to all zero, handler fills from holding.
	if r.ContentLength > 0 {
		if !decodeJSON(r, w, &req) {
			return
		}
	}

	// Load holding to get current values, levels, and ticker.
	var ticker string
	var currentPrice, support1, resistance1, resistance2 float64
	var entry, stop float64
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
			ticker = *h.Ticker
		}
		if h.CurrentPrice != nil {
			currentPrice = *h.CurrentPrice
		}
		if h.Support1 != nil {
			support1 = *h.Support1
		}
		if h.Resistance1 != nil {
			resistance1 = *h.Resistance1
		}
		if h.Resistance2 != nil {
			resistance2 = *h.Resistance2
		}
		if h.StopLoss != nil {
			stop = *h.StopLoss
		}
		if h.AvgOpenPrice != nil {
			entry = *h.AvgOpenPrice
		}
	} else {
		h, err := s.store.GetCryptoHolding(r.Context(), userID, id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if mapStoreError(w, err) {
			return
		}
		ticker = h.Symbol
		if h.CurrentPriceUSD != nil {
			currentPrice = *h.CurrentPriceUSD
		}
		if h.Support1 != nil {
			support1 = *h.Support1
		}
		if h.Resistance1 != nil {
			resistance1 = *h.Resistance1
		}
		if h.Resistance2 != nil {
			resistance2 = *h.Resistance2
		}
		if h.AvgBuyUSD != nil {
			entry = *h.AvgBuyUSD
		}
	}

	// Body overrides.
	if req.EntryPrice > 0 {
		entry = req.EntryPrice
	}
	if req.StopPrice > 0 {
		stop = req.StopPrice
	}
	tp1 := req.TP1
	tp2 := req.TP2

	// Build daily bars from store.
	rows, _ := s.store.GetDailyBars(r.Context(), ticker, kind)
	daily := make([]technicals.Bar, 0, len(rows))
	for _, b := range rows {
		t, err := time.Parse("2006-01-02", b.Date)
		if err != nil {
			continue
		}
		daily = append(daily, technicals.Bar{
			Date: t, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close,
		})
	}

	in := technicals.BuildAutoScoreInputs(daily, currentPrice, support1, resistance1, resistance2, entry, stop, tp1, tp2)
	// Fill TP1/TP2 defaults from suggestions when caller didn't supply.
	if in.TP1 == 0 && resistance1 > 0 && in.ATRWeekly > 0 {
		in.TP1 = technicals.SuggestedTP1(resistance1, in.ATRWeekly)
	}
	if in.TP2 == 0 && resistance2 > 0 && in.ATRWeekly > 0 {
		in.TP2 = technicals.SuggestedTP2(resistance2, in.ATRWeekly)
	}
	// Fill stop from suggestion when caller has none.
	if in.StopPrice == 0 && support1 > 0 && in.ATRWeekly > 0 {
		volTier := technicals.VolTier(in.ATRWeekly, currentPrice)
		in.StopPrice = technicals.SuggestedSL(support1, in.ATRWeekly, volTier)
	}
	// Fill entry from current price when caller has none.
	if in.EntryPrice == 0 {
		in.EntryPrice = currentPrice
	}

	out := technicals.AutoScorePercoco(in)
	writeJSON(w, http.StatusOK, autoscoreResp{
		Scores:     out.Scores,
		Rationales: out.Rationales,
		UsedEntry:  in.EntryPrice,
		UsedStop:   in.StopPrice,
		UsedTP1:    in.TP1,
		UsedTP2:    in.TP2,
		ATRWeekly:  in.ATRWeekly,
	})
}
