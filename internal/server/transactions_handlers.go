// Spec 10 D4 — Transaction CRUD + dividends endpoints.
//
// Endpoints (cookie auth):
//   GET    /api/transactions?holdingKind=&holdingId=&from=&to=
//   POST   /api/transactions                        create txn + recompute position
//   POST   /api/transactions/{id}/supersede         soft-supersede + recompute
//   GET    /api/holdings/{kind}/{id}/taxlots        FIFO breakdown
//   GET    /api/dividends?holdingId=
//   POST   /api/dividends                           record dividend received
//
// Critical rule (Spec 10 D4): transactions are append-only. NO UPDATE on
// quantity/price/date — corrections via supersede + new row.

package server

import (
	"context"
	"ft/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ----- listing ---------------------------------------------------------

// GET /api/transactions?holdingKind=&holdingId=&from=&to=&limit=
func (s *Server) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	q := r.URL.Query()
	kind := q.Get("holdingKind")
	holdingIDStr := q.Get("holdingId")
	limit, _ := strconv.Atoi(q.Get("limit"))

	if holdingIDStr != "" {
		holdingID, err := strconv.ParseInt(holdingIDStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad holdingId")
			return
		}
		rows, err := s.store.ListTransactions(r.Context(), kind, holdingID)
		if mapStoreError(w, err) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"transactions": rows})
		return
	}

	// Cross-holding listing — used by Settings.
	rows, err := s.store.ListAllTransactions(r.Context(), userID, limit)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": rows})
}

// ----- create ---------------------------------------------------------

type txnReq struct {
	HoldingKind string  `json:"holdingKind"`
	HoldingID   int64   `json:"holdingId"`
	TxnType     string  `json:"txnType"`
	ExecutedAt  string  `json:"executedAt"` // ISO RFC3339; defaults to now
	Quantity    float64 `json:"quantity"`
	PriceUSD    float64 `json:"priceUsd"`
	FeesUSD     float64 `json:"feesUsd"`
	Venue       string  `json:"venue,omitempty"`
	ExternalID  string  `json:"externalId,omitempty"`
	Note        string  `json:"note,omitempty"`
}

// POST /api/transactions
func (s *Server) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req txnReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.HoldingKind != "stock" && req.HoldingKind != "crypto" {
		writeError(w, http.StatusBadRequest, "holdingKind must be stock|crypto")
		return
	}
	if req.HoldingID == 0 {
		writeError(w, http.StatusBadRequest, "holdingId required")
		return
	}
	if !isValidTxnType(req.TxnType) {
		writeError(w, http.StatusBadRequest, "txnType must be buy|sell|fee|opening_position")
		return
	}
	if req.Quantity < 0 {
		writeError(w, http.StatusBadRequest, "quantity must be ≥ 0")
		return
	}
	if req.PriceUSD < 0 {
		writeError(w, http.StatusBadRequest, "priceUsd must be ≥ 0")
		return
	}

	// Resolve ticker from holding row.
	ticker, err := s.tickerForHolding(r.Context(), userID, req.HoldingKind, req.HoldingID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	executedAt := time.Now().UTC()
	if req.ExecutedAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExecutedAt); err == nil {
			executedAt = t
		}
	}
	totalUSD := req.Quantity * req.PriceUSD
	switch req.TxnType {
	case store.TxnTypeBuy, store.TxnTypeOpening:
		totalUSD += req.FeesUSD
	case store.TxnTypeSell:
		totalUSD -= req.FeesUSD
	case store.TxnTypeFee:
		totalUSD = req.FeesUSD
	}

	id, err := s.store.InsertTransaction(r.Context(), store.TransactionRow{
		HoldingKind: req.HoldingKind, HoldingID: req.HoldingID, Ticker: ticker,
		TxnType:    req.TxnType,
		ExecutedAt: executedAt,
		Quantity:   req.Quantity, PriceUSD: req.PriceUSD, FeesUSD: req.FeesUSD,
		TotalUSD:   totalUSD,
		Venue:      req.Venue, ExternalID: req.ExternalID, Note: req.Note,
	})
	if mapStoreError(w, err) {
		return
	}

	// Recompute + cache derived position.
	if err := s.recomputeAndCachePosition(r.Context(), userID, req.HoldingKind, req.HoldingID); err != nil {
		// Logged but don't fail the user's request — the txn is in.
		mapStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// POST /api/transactions/{id}/supersede
func (s *Server) handleSupersedeTransaction(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	t, err := s.store.GetTransaction(r.Context(), id)
	if mapStoreError(w, err) {
		return
	}
	if err := s.store.SupersedeTransaction(r.Context(), id); err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.recomputeAndCachePosition(r.Context(), userID, t.HoldingKind, t.HoldingID); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"superseded": id})
}

// GET /api/holdings/{kind}/{id}/taxlots
func (s *Server) handleStockTaxLots(w http.ResponseWriter, r *http.Request) {
	s.handleTaxLots(w, r, "stock")
}
func (s *Server) handleCryptoTaxLots(w http.ResponseWriter, r *http.Request) {
	s.handleTaxLots(w, r, "crypto")
}
func (s *Server) handleTaxLots(w http.ResponseWriter, r *http.Request, kind string) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	txns, err := s.store.ListTransactions(r.Context(), kind, id)
	if mapStoreError(w, err) {
		return
	}
	currentPrice, _ := s.currentPriceForHolding(r.Context(), userID, kind, id)
	pos := store.ComputeFIFOPosition(txns, currentPrice)
	writeJSON(w, http.StatusOK, map[string]any{
		"position": pos,
		"currentPrice": currentPrice,
	})
}

// ----- dividends ------------------------------------------------------

type dividendReq struct {
	HoldingID         int64   `json:"holdingId"`
	ExDate            string  `json:"exDate"`
	PayDate           string  `json:"payDate,omitempty"`
	AmountPerShareUSD float64 `json:"amountPerShareUsd"`
	SharesHeld        float64 `json:"sharesHeld"`
	Note              string  `json:"note,omitempty"`
}

func (s *Server) handleCreateDividend(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req dividendReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.HoldingID == 0 || req.ExDate == "" || req.AmountPerShareUSD <= 0 || req.SharesHeld <= 0 {
		writeError(w, http.StatusBadRequest, "holdingId, exDate, amountPerShareUsd, sharesHeld required")
		return
	}
	ticker, err := s.tickerForHolding(r.Context(), userID, "stock", req.HoldingID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	id, err := s.store.InsertDividend(r.Context(), store.DividendRow{
		HoldingID: req.HoldingID, Ticker: ticker,
		ExDate: req.ExDate, PayDate: req.PayDate,
		AmountPerShareUSD: req.AmountPerShareUSD, SharesHeld: req.SharesHeld,
		TotalReceivedUSD: req.AmountPerShareUSD * req.SharesHeld,
		Note:              req.Note,
	})
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleListDividends(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	holdingID, err := strconv.ParseInt(q.Get("holdingId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "holdingId required")
		return
	}
	rows, err := s.store.ListDividendsForHolding(r.Context(), holdingID)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dividends": rows})
}

// ----- shared helpers -------------------------------------------------

func isValidTxnType(t string) bool {
	switch t {
	case store.TxnTypeBuy, store.TxnTypeSell, store.TxnTypeFee, store.TxnTypeOpening:
		return true
	}
	return false
}

func (s *Server) tickerForHolding(ctx context.Context, userID int64, kind string, holdingID int64) (string, error) {
	if kind == "stock" {
		h, err := s.store.GetStockHolding(ctx, userID, holdingID)
		if err != nil {
			return "", err
		}
		if h.Ticker != nil {
			return strings.ToUpper(*h.Ticker), nil
		}
		return strings.ToUpper(h.Name), nil
	}
	h, err := s.store.GetCryptoHolding(ctx, userID, holdingID)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(h.Symbol), nil
}

func (s *Server) currentPriceForHolding(ctx context.Context, userID int64, kind string, holdingID int64) (float64, error) {
	if kind == "stock" {
		h, err := s.store.GetStockHolding(ctx, userID, holdingID)
		if err != nil || h == nil || h.CurrentPrice == nil {
			return 0, err
		}
		return *h.CurrentPrice, nil
	}
	h, err := s.store.GetCryptoHolding(ctx, userID, holdingID)
	if err != nil || h == nil || h.CurrentPriceUSD == nil {
		return 0, err
	}
	return *h.CurrentPriceUSD, nil
}

// recomputeAndCachePosition runs FIFO on the active transactions log and
// updates the holding row's cached quantity/cost/realized_pnl. Called
// after every transaction mutation.
func (s *Server) recomputeAndCachePosition(ctx context.Context, userID int64, kind string, holdingID int64) error {
	txns, err := s.store.ListTransactions(ctx, kind, holdingID)
	if err != nil {
		return err
	}
	currentPrice, _ := s.currentPriceForHolding(ctx, userID, kind, holdingID)
	pos := store.ComputeFIFOPosition(txns, currentPrice)
	if kind == "stock" {
		return s.store.CacheDerivedStockPosition(ctx, holdingID, pos)
	}
	return s.store.CacheDerivedCryptoPosition(ctx, holdingID, pos)
}
