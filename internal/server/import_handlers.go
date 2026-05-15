package server

import (
	"fmt"
	"ft/internal/domain"
	"ft/internal/persistence"
	"log/slog"
	"net/http"
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
