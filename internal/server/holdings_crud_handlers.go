package server

import (
	"encoding/json"
	"errors"
	"ft/internal/domain"
	"ft/internal/store"
	"net/http"
	"strconv"
	"strings"
)

// Spec 3 CRUD handlers: create / update / soft-delete / restore for both
// holdings tables. Every mutation writes one row to holdings_audit.

// ----- request bodies ------------------------------------------------------

type stockMutationReq struct {
	Name           string   `json:"name"`
	Ticker         *string  `json:"ticker"`
	Category       *string  `json:"category"`
	Sector         *string  `json:"sector"`
	InvestedUSD    float64  `json:"investedUsd"`
	AvgOpenPrice   *float64 `json:"avgOpenPrice"`
	CurrentPrice   *float64 `json:"currentPrice"`
	StopLoss       *float64 `json:"stopLoss"`
	TakeProfit     *float64 `json:"takeProfit"`
	StrategyNote   string   `json:"strategyNote"`
	Note           *string  `json:"note"`
	Beta           *float64 `json:"beta"`
	EarningsDate   *string  `json:"earningsDate"`
	ExDividendDate *string  `json:"exDividendDate"`
	// Spec 9c — Percoco levels + stage. ATR/vol_tier_auto are refresh-
	// managed; user can override stage when correcting after an out-of-
	// FT eToro sale.
	Support1    *float64 `json:"support1"`
	Support2    *float64 `json:"support2"`
	Resistance1 *float64 `json:"resistance1"`
	Resistance2 *float64 `json:"resistance2"`
	SetupType   *string  `json:"setupType"`
	Stage       *string  `json:"stage"`
	Reason      *string  `json:"reason,omitempty"` // for update audit row
}

type cryptoMutationReq struct {
	Name            string   `json:"name"`
	Symbol          string   `json:"symbol"`
	Classification  string   `json:"classification"` // "core" | "alt"
	IsCore          bool     `json:"isCore"`
	Category        *string  `json:"category"`
	Wallet          *string  `json:"wallet"`
	QuantityHeld    float64  `json:"quantityHeld"`
	QuantityStaked  float64  `json:"quantityStaked"`
	AvgBuyEUR       *float64 `json:"avgBuyEur"`
	CostBasisEUR    *float64 `json:"costBasisEur"`
	CurrentPriceEUR *float64 `json:"currentPriceEur"`
	StrategyNote    string   `json:"strategyNote"`
	Note            *string  `json:"note"`
	VolTier         string   `json:"volTier"`
	// Spec 9c additions:
	Support1    *float64 `json:"support1"`
	Support2    *float64 `json:"support2"`
	Resistance1 *float64 `json:"resistance1"`
	Resistance2 *float64 `json:"resistance2"`
	SetupType   *string  `json:"setupType"`
	Stage       *string  `json:"stage"`
	Reason      *string  `json:"reason,omitempty"`
}

type restoreReq struct {
	Reason *string `json:"reason,omitempty"`
}

// ===========================================================================
// STOCK
// ===========================================================================

// POST /api/holdings/stocks
func (s *Server) handleCreateStock(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req stockMutationReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	h := stockFromReq(req)
	h.UserID = userID
	id, err := s.store.InsertStockHolding(r.Context(), h)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	h.ID = id

	_ = s.store.RecordAudit(r.Context(), userID, "stock", id,
		h.Ticker, nil, store.AuditCreate,
		map[string]any{"new": stockSnapshot(h)},
		req.Reason)

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// PUT /api/holdings/stocks/{id}
func (s *Server) handleUpdateStock(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req stockMutationReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Load existing row for diff
	old, err := s.store.GetStockHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	h := stockFromReq(req)
	h.UserID = userID
	h.ID = id
	// Preserve fields we don't touch via this endpoint (market data, etc.)
	h.RSI14 = old.RSI14
	h.MA50 = old.MA50
	h.MA200 = old.MA200
	h.GoldenCross = old.GoldenCross
	h.Support = old.Support
	h.Resistance = old.Resistance
	h.AnalystTarget = old.AnalystTarget
	h.ProposedEntry = old.ProposedEntry
	h.TechnicalSetup = old.TechnicalSetup
	h.AnalystRRView = old.AnalystRRView
	h.DailyChangePct = old.DailyChangePct
	h.EarningsDate = old.EarningsDate
	h.ExDividendDate = old.ExDividendDate
	// Beta is editable via this endpoint only if explicitly provided.
	if req.Beta != nil {
		h.Beta = req.Beta
	} else {
		h.Beta = old.Beta
	}
	// Spec 9c — refresh-owned fields always preserved.
	h.ATRWeekly = old.ATRWeekly
	h.VolTierAuto = old.VolTierAuto
	h.TP1HitAt = old.TP1HitAt
	h.TP2HitAt = old.TP2HitAt
	h.TimeStopReviewAt = old.TimeStopReviewAt
	// Stage default if absent — preserve old.
	if req.Stage == nil {
		h.Stage = old.Stage
	}
	if req.SetupType == nil {
		h.SetupType = old.SetupType
	}
	// Levels default if absent — preserve old.
	if req.Support1 == nil {
		h.Support1 = old.Support1
	}
	if req.Support2 == nil {
		h.Support2 = old.Support2
	}
	if req.Resistance1 == nil {
		h.Resistance1 = old.Resistance1
	}
	if req.Resistance2 == nil {
		h.Resistance2 = old.Resistance2
	}

	if err := s.store.UpdateStockHolding(r.Context(), h); err != nil {
		mapStoreError(w, err)
		return
	}

	changes := stockDiff(old, h)
	if len(changes) > 0 {
		_ = s.store.RecordAudit(r.Context(), userID, "stock", id,
			h.Ticker, nil, store.AuditUpdate, changes, req.Reason)
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "changed": len(changes)})
}

// DELETE /api/holdings/stocks/{id}   (soft delete)
func (s *Server) handleSoftDeleteStock(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	// Reason may be in JSON body OR ?reason=... query param.
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		// Try body
		var body restoreReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason != nil {
			reason = *body.Reason
		}
	}

	old, err := s.store.GetStockHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.SoftDeleteStockHolding(r.Context(), userID, id); err != nil {
		mapStoreError(w, err)
		return
	}
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	_ = s.store.RecordAudit(r.Context(), userID, "stock", id,
		old.Ticker, nil, store.AuditSoftDelete,
		map[string]any{}, reasonPtr)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/holdings/stocks/{id}/restore
func (s *Server) handleRestoreStock(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	old, err := s.store.GetStockHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.RestoreStockHolding(r.Context(), userID, id); err != nil {
		mapStoreError(w, err)
		return
	}
	_ = s.store.RecordAudit(r.Context(), userID, "stock", id,
		old.Ticker, nil, store.AuditRestore, map[string]any{}, nil)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/holdings/stocks/deleted
func (s *Server) handleListDeletedStocks(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	rows, err := s.store.ListDeletedStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": rows})
}

// ===========================================================================
// CRYPTO
// ===========================================================================

// POST /api/holdings/crypto
func (s *Server) handleCreateCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	var req cryptoMutationReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Symbol) == "" {
		writeError(w, http.StatusBadRequest, "name and symbol are required")
		return
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	if req.Classification != "core" && req.Classification != "alt" {
		req.Classification = "alt"
		if req.Symbol == "BTC" || req.Symbol == "ETH" {
			req.Classification = "core"
		}
	}
	if !domain.IsValidVolTier(req.VolTier) {
		req.VolTier = "medium"
	}

	h := cryptoFromReq(req)
	h.UserID = userID
	id, err := s.store.InsertCryptoHolding(r.Context(), h)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	h.ID = id

	_ = s.store.RecordAudit(r.Context(), userID, "crypto", id,
		nil, &h.Symbol, store.AuditCreate,
		map[string]any{"new": cryptoSnapshot(h)},
		req.Reason)

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// PUT /api/holdings/crypto/{id}
func (s *Server) handleUpdateCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var req cryptoMutationReq
	if !decodeJSON(r, w, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Symbol) == "" {
		writeError(w, http.StatusBadRequest, "name and symbol are required")
		return
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	if req.Classification != "core" && req.Classification != "alt" {
		req.Classification = "alt"
	}
	if !domain.IsValidVolTier(req.VolTier) {
		req.VolTier = "medium"
	}

	old, err := s.store.GetCryptoHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	h := cryptoFromReq(req)
	h.UserID = userID
	h.ID = id
	// Preserve market-data fields
	h.AvgBuyUSD = old.AvgBuyUSD
	h.CostBasisUSD = old.CostBasisUSD
	h.CurrentPriceUSD = old.CurrentPriceUSD
	h.CurrentValueEUR = old.CurrentValueEUR
	h.CurrentValueUSD = old.CurrentValueUSD
	h.RSI14 = old.RSI14
	h.Change7dPct = old.Change7dPct
	h.Change30dPct = old.Change30dPct
	h.DailyChangePct = old.DailyChangePct
	// Spec 9c — refresh-owned + stage preservation.
	h.ATRWeekly = old.ATRWeekly
	h.VolTierAuto = old.VolTierAuto
	h.TP1HitAt = old.TP1HitAt
	h.TP2HitAt = old.TP2HitAt
	h.TimeStopReviewAt = old.TimeStopReviewAt
	if req.Stage == nil {
		h.Stage = old.Stage
	}
	if req.SetupType == nil {
		h.SetupType = old.SetupType
	}
	if req.Support1 == nil {
		h.Support1 = old.Support1
	}
	if req.Support2 == nil {
		h.Support2 = old.Support2
	}
	if req.Resistance1 == nil {
		h.Resistance1 = old.Resistance1
	}
	if req.Resistance2 == nil {
		h.Resistance2 = old.Resistance2
	}

	if err := s.store.UpdateCryptoHolding(r.Context(), h); err != nil {
		mapStoreError(w, err)
		return
	}

	changes := cryptoDiff(old, h)
	if len(changes) > 0 {
		_ = s.store.RecordAudit(r.Context(), userID, "crypto", id,
			nil, &h.Symbol, store.AuditUpdate, changes, req.Reason)
	}

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "changed": len(changes)})
}

// DELETE /api/holdings/crypto/{id}
func (s *Server) handleSoftDeleteCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		var body restoreReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Reason != nil {
			reason = *body.Reason
		}
	}
	old, err := s.store.GetCryptoHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.SoftDeleteCryptoHolding(r.Context(), userID, id); err != nil {
		mapStoreError(w, err)
		return
	}
	var reasonPtr *string
	if reason != "" {
		reasonPtr = &reason
	}
	_ = s.store.RecordAudit(r.Context(), userID, "crypto", id,
		nil, &old.Symbol, store.AuditSoftDelete, map[string]any{}, reasonPtr)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/holdings/crypto/{id}/restore
func (s *Server) handleRestoreCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	old, err := s.store.GetCryptoHolding(r.Context(), userID, id)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	if err := s.store.RestoreCryptoHolding(r.Context(), userID, id); err != nil {
		mapStoreError(w, err)
		return
	}
	_ = s.store.RecordAudit(r.Context(), userID, "crypto", id,
		nil, &old.Symbol, store.AuditRestore, map[string]any{}, nil)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/holdings/crypto/deleted
func (s *Server) handleListDeletedCrypto(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	rows, err := s.store.ListDeletedCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"holdings": rows})
}

// ===========================================================================
// AUDIT LOG ENDPOINT (Spec 3 D13)
// ===========================================================================

// GET /api/audit?limit=N&offset=M
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.store.ListAudit(r.Context(), userID, limit, offset)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": rows})
}

// ===========================================================================
// Helpers — request → domain, diff, snapshot
// ===========================================================================

func stockFromReq(req stockMutationReq) *domain.StockHolding {
	stage := "pre_tp1"
	if req.Stage != nil && *req.Stage != "" {
		stage = *req.Stage
	}
	return &domain.StockHolding{
		Name:           strings.TrimSpace(req.Name),
		Ticker:         normalizeTickerPtr(req.Ticker),
		Category:       trimStrPtr(req.Category),
		Sector:         trimStrPtr(req.Sector),
		InvestedUSD:    req.InvestedUSD,
		AvgOpenPrice:   req.AvgOpenPrice,
		CurrentPrice:   req.CurrentPrice,
		StopLoss:       req.StopLoss,
		TakeProfit:     req.TakeProfit,
		StrategyNote:   strings.TrimSpace(req.StrategyNote),
		Note:           trimStrPtr(req.Note),
		Beta:           req.Beta,
		EarningsDate:   trimStrPtr(req.EarningsDate),
		ExDividendDate: trimStrPtr(req.ExDividendDate),
		// Spec 9c
		Support1:    req.Support1,
		Support2:    req.Support2,
		Resistance1: req.Resistance1,
		Resistance2: req.Resistance2,
		SetupType:   trimStrPtr(req.SetupType),
		Stage:       stage,
	}
}

func cryptoFromReq(req cryptoMutationReq) *domain.CryptoHolding {
	stage := "pre_tp1"
	if req.Stage != nil && *req.Stage != "" {
		stage = *req.Stage
	}
	return &domain.CryptoHolding{
		Name:            strings.TrimSpace(req.Name),
		Symbol:          req.Symbol,
		Classification:  req.Classification,
		IsCore:          req.IsCore || req.Classification == "core",
		Category:        trimStrPtr(req.Category),
		Wallet:          trimStrPtr(req.Wallet),
		QuantityHeld:    req.QuantityHeld,
		QuantityStaked:  req.QuantityStaked,
		AvgBuyEUR:       req.AvgBuyEUR,
		CostBasisEUR:    req.CostBasisEUR,
		CurrentPriceEUR: req.CurrentPriceEUR,
		StrategyNote:    strings.TrimSpace(req.StrategyNote),
		Note:            trimStrPtr(req.Note),
		VolTier:         req.VolTier,
		// Spec 9c
		Support1:    req.Support1,
		Support2:    req.Support2,
		Resistance1: req.Resistance1,
		Resistance2: req.Resistance2,
		SetupType:   trimStrPtr(req.SetupType),
		Stage:       stage,
	}
}

func stockSnapshot(h *domain.StockHolding) map[string]any {
	return map[string]any{
		"name":         h.Name,
		"ticker":       h.Ticker,
		"category":     h.Category,
		"sector":       h.Sector,
		"investedUsd":  h.InvestedUSD,
		"avgOpenPrice": h.AvgOpenPrice,
		"currentPrice": h.CurrentPrice,
		"stopLoss":     h.StopLoss,
		"takeProfit":   h.TakeProfit,
		"strategyNote": h.StrategyNote,
		"note":         h.Note,
	}
}

func cryptoSnapshot(h *domain.CryptoHolding) map[string]any {
	return map[string]any{
		"name":           h.Name,
		"symbol":         h.Symbol,
		"classification": h.Classification,
		"isCore":         h.IsCore,
		"category":       h.Category,
		"wallet":         h.Wallet,
		"quantityHeld":   h.QuantityHeld,
		"quantityStaked": h.QuantityStaked,
		"avgBuyEur":      h.AvgBuyEUR,
		"costBasisEur":   h.CostBasisEUR,
		"volTier":        h.VolTier,
		"strategyNote":   h.StrategyNote,
		"note":           h.Note,
	}
}

func stockDiff(old, new *domain.StockHolding) map[string]any {
	d := map[string]any{}
	addStrDiff(d, "name", old.Name, new.Name)
	addStrPtrDiff(d, "ticker", old.Ticker, new.Ticker)
	addStrPtrDiff(d, "category", old.Category, new.Category)
	addStrPtrDiff(d, "sector", old.Sector, new.Sector)
	addFloatDiff(d, "investedUsd", old.InvestedUSD, new.InvestedUSD)
	addFloatPtrDiff(d, "avgOpenPrice", old.AvgOpenPrice, new.AvgOpenPrice)
	addFloatPtrDiff(d, "currentPrice", old.CurrentPrice, new.CurrentPrice)
	addFloatPtrDiff(d, "stopLoss", old.StopLoss, new.StopLoss)
	addFloatPtrDiff(d, "takeProfit", old.TakeProfit, new.TakeProfit)
	addStrDiff(d, "strategyNote", old.StrategyNote, new.StrategyNote)
	addStrPtrDiff(d, "note", old.Note, new.Note)
	return d
}

func cryptoDiff(old, new *domain.CryptoHolding) map[string]any {
	d := map[string]any{}
	addStrDiff(d, "name", old.Name, new.Name)
	addStrDiff(d, "symbol", old.Symbol, new.Symbol)
	addStrDiff(d, "classification", old.Classification, new.Classification)
	addBoolDiff(d, "isCore", old.IsCore, new.IsCore)
	addStrDiff(d, "volTier", old.VolTier, new.VolTier)
	addStrPtrDiff(d, "category", old.Category, new.Category)
	addStrPtrDiff(d, "wallet", old.Wallet, new.Wallet)
	addFloatDiff(d, "quantityHeld", old.QuantityHeld, new.QuantityHeld)
	addFloatDiff(d, "quantityStaked", old.QuantityStaked, new.QuantityStaked)
	addFloatPtrDiff(d, "avgBuyEur", old.AvgBuyEUR, new.AvgBuyEUR)
	addFloatPtrDiff(d, "costBasisEur", old.CostBasisEUR, new.CostBasisEUR)
	addStrDiff(d, "strategyNote", old.StrategyNote, new.StrategyNote)
	addStrPtrDiff(d, "note", old.Note, new.Note)
	return d
}

func addStrDiff(d map[string]any, k, a, b string) {
	if a != b {
		d[k] = map[string]any{"old": a, "new": b}
	}
}
func addBoolDiff(d map[string]any, k string, a, b bool) {
	if a != b {
		d[k] = map[string]any{"old": a, "new": b}
	}
}
func addFloatDiff(d map[string]any, k string, a, b float64) {
	if a != b {
		d[k] = map[string]any{"old": a, "new": b}
	}
}
func addStrPtrDiff(d map[string]any, k string, a, b *string) {
	ae, be := strOrEmpty(a), strOrEmpty(b)
	if ae != be {
		d[k] = map[string]any{"old": a, "new": b}
	}
}
func addFloatPtrDiff(d map[string]any, k string, a, b *float64) {
	if (a == nil) != (b == nil) {
		d[k] = map[string]any{"old": a, "new": b}
		return
	}
	if a == nil {
		return
	}
	if *a != *b {
		d[k] = map[string]any{"old": *a, "new": *b}
	}
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// normalizeTickerPtr trims and uppercases a ticker. Empty → nil.
func normalizeTickerPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.ToUpper(strings.TrimSpace(*p))
	if t == "" {
		return nil
	}
	return &t
}
func trimStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

// Avoid unused-import warnings if iteration trims these:
var _ = errors.Is
