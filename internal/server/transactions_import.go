// Spec 10 D10 — CSV import for historical transactions.
//
// POST /api/transactions/import (multipart/form-data with "file" field)
//
// CSV schema (header row required, order ignored):
//   ticker,holding_kind,executed_at,txn_type,quantity,price_usd,fees_usd,venue,note
//
// Behaviour:
//   1. Parse CSV, validate per-row (kind, type, numbers).
//   2. Resolve holding_id by matching ticker (case-insensitive) against
//      stock_holdings then crypto_holdings for the user.
//   3. Skip rows with no matching holding; report counts.
//   4. Bulk insert in a transaction.
//   5. Recompute every touched holding's FIFO position.
//
// Idempotency-of-import is NOT enforced here — re-importing the same CSV
// will duplicate rows. Users should supersede or restart from a clean
// state if they need to re-import.

package server

import (
	"encoding/csv"
	"fmt"
	"ft/internal/store"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// POST /api/transactions/import — multipart upload.
func (s *Server) handleImportTransactions(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	if err := r.ParseMultipartForm(2 << 20); err != nil { // 2 MiB cap
		writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' form field")
		return
	}
	defer file.Close()

	cr := csv.NewReader(file)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1 // tolerate ragged columns
	rows, err := cr.ReadAll()
	if err != nil {
		writeError(w, http.StatusBadRequest, "csv parse: "+err.Error())
		return
	}
	if len(rows) < 2 {
		writeError(w, http.StatusBadRequest, "csv must have a header row + at least one data row")
		return
	}

	// Header → column index map.
	header := rows[0]
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	requireCol := func(name string) (int, error) {
		idx, ok := col[name]
		if !ok {
			return 0, fmt.Errorf("missing required column: %s", name)
		}
		return idx, nil
	}
	for _, n := range []string{"ticker", "holding_kind", "executed_at", "txn_type", "quantity", "price_usd"} {
		if _, ok := col[n]; !ok {
			writeError(w, http.StatusBadRequest, "missing required column: "+n)
			return
		}
	}
	tickerIdx, _ := requireCol("ticker")
	kindIdx, _ := requireCol("holding_kind")
	whenIdx, _ := requireCol("executed_at")
	typeIdx, _ := requireCol("txn_type")
	qtyIdx, _ := requireCol("quantity")
	priceIdx, _ := requireCol("price_usd")
	feesIdx := col["fees_usd"]   // optional
	venueIdx := col["venue"]     // optional
	noteIdx := col["note"]       // optional

	// Resolve ticker → holding_id (per kind) once, before insert loop.
	stocks, _ := s.store.ListStockHoldings(r.Context(), userID)
	cryptos, _ := s.store.ListCryptoHoldings(r.Context(), userID)
	stockByTicker := map[string]int64{}
	for _, h := range stocks {
		if h.Ticker != nil {
			stockByTicker[strings.ToUpper(*h.Ticker)] = h.ID
		}
	}
	cryptoBySymbol := map[string]int64{}
	for _, h := range cryptos {
		cryptoBySymbol[strings.ToUpper(h.Symbol)] = h.ID
	}

	type rowOut struct {
		Row     int    `json:"row"`
		Ticker  string `json:"ticker"`
		Status  string `json:"status"` // "imported" | "skipped" | "error"
		Reason  string `json:"reason,omitempty"`
		TxnID   int64  `json:"txnId,omitempty"`
	}
	out := make([]rowOut, 0, len(rows)-1)
	touched := map[string]map[int64]bool{
		"stock":  {},
		"crypto": {},
	}
	imported := 0
	skipped := 0
	errored := 0

	// Use `rec` instead of `r` for the CSV row so we don't shadow the
	// outer *http.Request — that's needed for r.Context() in the InsertTransaction call.
	for i, rec := range rows[1:] {
		row := rowOut{Row: i + 2} // 1-indexed + skip header
		getCol := func(idx int) string {
			if idx < 0 || idx >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[idx])
		}
		ticker := strings.ToUpper(getCol(tickerIdx))
		kind := strings.ToLower(getCol(kindIdx))
		row.Ticker = ticker
		if kind != "stock" && kind != "crypto" {
			row.Status = "error"
			row.Reason = "holding_kind must be stock|crypto"
			errored++
			out = append(out, row)
			continue
		}
		var holdingID int64
		if kind == "stock" {
			holdingID = stockByTicker[ticker]
		} else {
			holdingID = cryptoBySymbol[ticker]
		}
		if holdingID == 0 {
			row.Status = "skipped"
			row.Reason = "no matching holding for ticker"
			skipped++
			out = append(out, row)
			continue
		}
		txnType := strings.ToLower(getCol(typeIdx))
		if !isValidTxnType(txnType) {
			row.Status = "error"
			row.Reason = "bad txn_type"
			errored++
			out = append(out, row)
			continue
		}
		qty, err := strconv.ParseFloat(getCol(qtyIdx), 64)
		if err != nil || qty < 0 {
			row.Status = "error"
			row.Reason = "bad quantity"
			errored++
			out = append(out, row)
			continue
		}
		price, err := strconv.ParseFloat(getCol(priceIdx), 64)
		if err != nil || price < 0 {
			row.Status = "error"
			row.Reason = "bad price_usd"
			errored++
			out = append(out, row)
			continue
		}
		fees := 0.0
		if feesIdx > 0 {
			if v := getCol(feesIdx); v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					fees = f
				}
			}
		}
		executedAt := time.Now().UTC()
		if rawT := getCol(whenIdx); rawT != "" {
			if t, err := time.Parse(time.RFC3339, rawT); err == nil {
				executedAt = t
			} else if t, err := time.Parse("2006-01-02", rawT); err == nil {
				executedAt = t.UTC()
			} else {
				row.Status = "error"
				row.Reason = "bad executed_at (use RFC3339 or YYYY-MM-DD)"
				errored++
				out = append(out, row)
				continue
			}
		}

		totalUSD := qty * price
		switch txnType {
		case store.TxnTypeBuy, store.TxnTypeOpening:
			totalUSD += fees
		case store.TxnTypeSell:
			totalUSD -= fees
		case store.TxnTypeFee:
			totalUSD = fees
		}

		venue := ""
		if venueIdx > 0 {
			venue = getCol(venueIdx)
		}
		note := ""
		if noteIdx > 0 {
			note = getCol(noteIdx)
		}

		id, err := s.store.InsertTransaction(r.Context(), store.TransactionRow{
			HoldingKind: kind, HoldingID: holdingID, Ticker: ticker,
			TxnType: txnType, ExecutedAt: executedAt,
			Quantity: qty, PriceUSD: price, FeesUSD: fees, TotalUSD: totalUSD,
			Venue: venue, Note: note,
		})
		if err != nil {
			row.Status = "error"
			row.Reason = err.Error()
			errored++
			out = append(out, row)
			continue
		}
		touched[kind][holdingID] = true
		imported++
		row.Status = "imported"
		row.TxnID = id
		out = append(out, row)
	}

	// Recompute FIFO for every touched holding.
	for kind, ids := range touched {
		for hid := range ids {
			_ = s.recomputeAndCachePosition(r.Context(), userID, kind, hid)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary": map[string]int{
			"total":    len(rows) - 1,
			"imported": imported,
			"skipped":  skipped,
			"errored":  errored,
		},
		"rows": out,
	})
}

