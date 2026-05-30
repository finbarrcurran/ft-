// Spec 9l D26/D27 — Crypto theses cross-table + detail handlers.
//
//   GET /api/crypto/theses                       cross-thesis table data
//   GET /api/crypto/theses/{symbol}/{version}    per-thesis detail
//   GET /api/crypto/theses/{symbol}/{version}/dependencies   cascade deps
//   GET /api/crypto/theses/{symbol}/{version}/events         cascade events
//   GET /api/crypto/allocation                   allocation panel current
//   PUT /api/crypto/allocation                   save allocation
//
// Thesis CRUD (POST/PUT to create theses) lands in Step 6's Scoring Engine.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"ft/internal/cryptotheses"
	"net/http"
	"time"
)

// GET /api/crypto/theses
func (s *Server) handleCryptoThesesList(w http.ResponseWriter, r *http.Request) {
	out, err := s.cryptoTheses.ListAll(r.Context())
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theses": out})
}

// GET /api/crypto/theses/{symbol}/{version}
func (s *Server) handleCryptoThesisGet(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	d, err := s.cryptoTheses.Get(r.Context(), symbol, version)
	if errors.Is(err, cryptotheses.ErrThesisNotFound) {
		writeError(w, http.StatusNotFound, "thesis not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thesis": d})
}

// GET /api/crypto/theses/{symbol}/{version}/events
func (s *Server) handleCryptoThesisEvents(w http.ResponseWriter, r *http.Request) {
	symbol := r.PathValue("symbol")
	version := r.PathValue("version")
	d, err := s.cryptoTheses.Get(r.Context(), symbol, version)
	if errors.Is(err, cryptotheses.ErrThesisNotFound) {
		writeError(w, http.StatusNotFound, "thesis not found")
		return
	}
	if mapStoreError(w, err) {
		return
	}
	unresolved := r.URL.Query().Get("unresolvedOnly") == "true"
	events, err := s.cryptoCascade.ListCascadeEvents(r.Context(), d.ID, unresolved)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// ===== Allocation Panel =====================================================

// AllocationResp is the response shape for GET /api/crypto/allocation.
type AllocationResp struct {
	Current cryptotheses.Allocation   `json:"current"`
	History []cryptotheses.Allocation `json:"history"`
}

// GET /api/crypto/allocation
func (s *Server) handleCryptoAllocationGet(w http.ResponseWriter, r *http.Request) {
	current, err := s.fetchAllocationCurrent(r.Context())
	if mapStoreError(w, err) {
		return
	}
	history, err := s.fetchAllocationHistory(r.Context(), 10)
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, AllocationResp{Current: *current, History: history})
}

// PUT /api/crypto/allocation
func (s *Server) handleCryptoAllocationPut(w http.ResponseWriter, r *http.Request) {
	var req cryptotheses.Allocation
	if !decodeJSON(r, w, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.store.DB.BeginTx(r.Context(), nil)
	if mapStoreError(w, err) {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE crypto_allocation_current
		   SET pct_stocks = ?, pct_btc = ?, pct_eth = ?, pct_alts = ?, pct_cash = ?,
		       note = ?, updated_at = strftime('%s','now')
		 WHERE id = 1`,
		req.PctStocks, req.PctBTC, req.PctETH, req.PctAlts, req.PctCash,
		nullableStr(req.Note)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO crypto_allocation_history (pct_stocks, pct_btc, pct_eth, pct_alts, pct_cash, note)
		VALUES (?, ?, ?, ?, ?, ?)`,
		req.PctStocks, req.PctBTC, req.PctETH, req.PctAlts, req.PctCash,
		nullableStr(req.Note)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

func (s *Server) fetchAllocationCurrent(ctx context.Context) (*cryptotheses.Allocation, error) {
	row := s.store.DB.QueryRowContext(ctx, `
		SELECT pct_stocks, pct_btc, pct_eth, pct_alts, pct_cash, COALESCE(note,''), updated_at
		  FROM crypto_allocation_current WHERE id = 1`)
	var a cryptotheses.Allocation
	if err := row.Scan(&a.PctStocks, &a.PctBTC, &a.PctETH, &a.PctAlts, &a.PctCash, &a.Note, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Server) fetchAllocationHistory(ctx context.Context, limit int) ([]cryptotheses.Allocation, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT pct_stocks, pct_btc, pct_eth, pct_alts, pct_cash, COALESCE(note,''), created_at
		  FROM crypto_allocation_history
		 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []cryptotheses.Allocation{}
	for rows.Next() {
		var a cryptotheses.Allocation
		if err := rows.Scan(&a.PctStocks, &a.PctBTC, &a.PctETH, &a.PctAlts, &a.PctCash, &a.Note, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// nullableStr converts empty string → null for SQL.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Ensure encoding/json + fmt imports stay used even if handlers are pruned later.
var _ = json.Marshal
var _ = fmt.Sprintf
var _ = time.Now
