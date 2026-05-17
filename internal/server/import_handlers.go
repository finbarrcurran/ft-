package server

import (
	"context"
	"fmt"
	"ft/internal/domain"
	"ft/internal/market"
	"ft/internal/persistence"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// pendingImport holds a parsed-but-not-yet-applied import for one user.
// Survives in memory between /api/import/preview and /api/import/apply.
// TTL'd so abandoned imports don't pile up.
type pendingImport struct {
	Stocks   []*domain.StockHolding
	Crypto   []*domain.CryptoHolding
	Stored   time.Time
	FileName string
	FX       *float64
}

var (
	pendingImports   = map[int64]*pendingImport{}
	pendingImportsMu sync.Mutex
	pendingTTL       = 10 * time.Minute
)

func storePending(userID int64, p *pendingImport) {
	pendingImportsMu.Lock()
	defer pendingImportsMu.Unlock()
	// Drop anything older than TTL while we have the lock.
	cutoff := time.Now().Add(-pendingTTL)
	for k, v := range pendingImports {
		if v.Stored.Before(cutoff) {
			delete(pendingImports, k)
		}
	}
	pendingImports[userID] = p
}

func popPending(userID int64) *pendingImport {
	pendingImportsMu.Lock()
	defer pendingImportsMu.Unlock()
	p, ok := pendingImports[userID]
	if !ok {
		return nil
	}
	delete(pendingImports, userID)
	if time.Since(p.Stored) > pendingTTL {
		return nil
	}
	return p
}

// POST /api/import/preview
//
// Multipart upload of an .xlsx master file. Parses it, computes a diff against
// the user's current holdings, stashes the parsed result in memory keyed by
// user_id, and returns the diff plus a few sample rows for the modal.
func (s *Server) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	// 5 MiB cap — master files are tiny (a few KB).
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("multipart parse: %s", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer file.Close()

	parsed, err := persistence.ParseXLSX(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse: %s", err))
		return
	}

	currentStocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	currentCrypto, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	// Spec 12 D7 — pre-fill missing fields on each parsed row via the
	// lookup endpoint. Mutates parsed.Stocks in place, returns a list of
	// {ticker, fields[]} hints so the UI can highlight which cells came
	// from Yahoo vs the user's sheet.
	enriched := enrichParsedStocks(r.Context(), parsed.Stocks)

	stockDiff := persistence.DiffStocks(currentStocks, parsed.Stocks)
	cryptoDiff := persistence.DiffCrypto(currentCrypto, parsed.Crypto)

	// Stash for the apply step.
	storePending(userID, &pendingImport{
		Stocks:   parsed.Stocks,
		Crypto:   parsed.Crypto,
		Stored:   time.Now(),
		FileName: header.Filename,
		FX:       parsed.FXSnapshotEURUSD,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"fileName":             header.Filename,
		"stockCount":           len(parsed.Stocks),
		"cryptoCount":          len(parsed.Crypto),
		"isMasterFormatStocks": parsed.IsMasterFormatStocks,
		"hasCrypto":            parsed.HasCrypto,
		"schemaVersion":        parsed.SchemaVersion,
		"fxSnapshotEurUsd":     parsed.FXSnapshotEURUSD,
		"warnings":             parsed.Warnings,
		"skipped":              parsed.Skipped,
		"stockDiff":            stockDiff,
		"cryptoDiff":           cryptoDiff,
		"enriched":             enriched, // Spec 12 D7 — UI highlight hints
		"ttlSeconds":           int(pendingTTL.Seconds()),
	})
}

// POST /api/import/apply
//
// JSON body: { applyStocks: bool, applyCrypto: bool }
// Reads the pending import, applies a transactional slam-replace per section,
// updates fx_snapshot if the master file carried one, and clears the pending.
func (s *Server) handleImportApply(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	var req struct {
		ApplyStocks bool `json:"applyStocks"`
		ApplyCrypto bool `json:"applyCrypto"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if !req.ApplyStocks && !req.ApplyCrypto {
		writeError(w, http.StatusBadRequest, "must select at least one section to apply")
		return
	}

	pending := popPending(userID)
	if pending == nil {
		writeError(w, http.StatusBadRequest, "no pending import (expired or never previewed)")
		return
	}

	var stocksApplied, cryptoApplied int

	if req.ApplyStocks && len(pending.Stocks) > 0 {
		if err := s.store.DeleteAllStockHoldings(r.Context(), userID); err != nil {
			mapStoreError(w, err)
			return
		}
		for _, h := range pending.Stocks {
			h.UserID = userID
			if _, err := s.store.InsertStockHolding(r.Context(), h); err != nil {
				slog.Error("import: insert stock", "name", h.Name, "err", err)
				mapStoreError(w, err)
				return
			}
			stocksApplied++
		}
	}

	if req.ApplyCrypto && len(pending.Crypto) > 0 {
		if err := s.store.DeleteAllCryptoHoldings(r.Context(), userID); err != nil {
			mapStoreError(w, err)
			return
		}
		for _, h := range pending.Crypto {
			h.UserID = userID
			if _, err := s.store.InsertCryptoHolding(r.Context(), h); err != nil {
				slog.Error("import: insert crypto", "symbol", h.Symbol, "err", err)
				mapStoreError(w, err)
				return
			}
			cryptoApplied++
		}
	}

	// Persist FX snapshot if the master file carried one.
	if pending.FX != nil {
		_ = s.store.SetMeta(r.Context(), "fx_snapshot_eur_usd",
			fmt.Sprintf("%g", *pending.FX))
	}

	slog.Info("import applied",
		"user_id", userID,
		"stocks", stocksApplied,
		"crypto", cryptoApplied,
		"file", pending.FileName)

	writeJSON(w, http.StatusOK, map[string]any{
		"stocksApplied": stocksApplied,
		"cryptoApplied": cryptoApplied,
	})
}

// enrichParsedStocks runs the Spec 12 D7 lookup on every parsed row whose
// name / sector / currency is missing. Mutates h in place; returns one
// hint per affected ticker so the preview UI can highlight enriched cells.
//
// Performed sequentially with a small inter-call gap to stay under Yahoo's
// quoteSummary budget. If a single ticker fails, the rest still run.
func enrichParsedStocks(ctx context.Context, stocks []*domain.StockHolding) []map[string]any {
	out := make([]map[string]any, 0)
	for _, h := range stocks {
		if h == nil || h.Ticker == nil || *h.Ticker == "" {
			continue
		}
		needName := strings.TrimSpace(h.Name) == ""
		needSector := h.Sector == nil || strings.TrimSpace(*h.Sector) == ""
		needCurrency := h.Currency == nil || strings.TrimSpace(*h.Currency) == ""
		if !needName && !needSector && !needCurrency {
			continue
		}
		p, err := market.FetchYahooProfile(ctx, *h.Ticker)
		if err != nil || p == nil {
			continue
		}
		filled := []string{}
		if needName && p.Name != "" {
			h.Name = p.Name
			filled = append(filled, "name")
		}
		if needSector && p.Sector != "" {
			s := p.Sector
			h.Sector = &s
			filled = append(filled, "sector")
		}
		if needCurrency && p.Currency != "" {
			c := p.Currency
			h.Currency = &c
			filled = append(filled, "currency")
		}
		if len(filled) > 0 {
			out = append(out, map[string]any{
				"ticker": *h.Ticker,
				"fields": filled,
				"source": "yahoo",
			})
		}
		// 250ms inter-call gap so we don't hammer Yahoo on a 36-row sheet.
		select {
		case <-ctx.Done():
			return out
		case <-time.After(250 * time.Millisecond):
		}
	}
	return out
}
