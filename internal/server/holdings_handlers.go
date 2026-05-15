package server

import (
	"ft/internal/alert"
	"ft/internal/domain"
	"ft/internal/metrics"
	"net/http"
)

// stockResp is the API shape returned by /api/holdings/stocks. The holding
// itself is embedded so the JSON has every StockHolding field at the top
// level, plus a `metrics` sub-object and an `alert` classification.
type stockResp struct {
	*domain.StockHolding
	Metrics metrics.StockMetrics `json:"metrics"`
	Alert   domain.AlertResult   `json:"alert"`
}

type cryptoResp struct {
	*domain.CryptoHolding
	Metrics metrics.CryptoMetrics `json:"metrics"`
}

// GET /api/holdings/stocks
func (s *Server) handleListStocks(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	holdings, err := s.store.ListStockHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	out := make([]stockResp, 0, len(holdings))
	for _, h := range holdings {
		m := metrics.ComputeStock(h)
		out = append(out, stockResp{
			StockHolding: h,
			Metrics:      m,
			Alert:        alert.Compute(h, m),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out})
}

// GET /api/holdings/crypto
func (s *Server) handleListCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	holdings, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	// Read FX snapshot for the response footer so the client can render the
	// rate that was used in EUR→USD conversions.
	out := make([]cryptoResp, 0, len(holdings))
	for _, h := range holdings {
		out = append(out, cryptoResp{
			CryptoHolding: h,
			Metrics:       metrics.ComputeCrypto(h),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": out})
}
