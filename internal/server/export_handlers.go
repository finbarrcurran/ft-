package server

import (
	"fmt"
	"ft/internal/persistence"
	"net/http"
	"strconv"
	"time"
)

// GET /api/export.xlsx
//
// Streams the user's current state as a master xlsx file. Same shape the
// import parser accepts. File name defaults to
// `ft-master-YYYY-MM-DD.xlsx` unless overridden via ?name=...
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	crypto, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	var fx *float64
	if v, err := s.store.GetMeta(r.Context(), "fx_snapshot_eur_usd"); err == nil {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			fx = &f
		}
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		name = fmt.Sprintf("ft-master-%s.xlsx", time.Now().UTC().Format("2006-01-02"))
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")

	if err := persistence.WriteXLSX(w, stocks, crypto, fx); err != nil {
		// Response headers already sent — best we can do is log.
		// Write a minimal error message into the body.
		_, _ = fmt.Fprintf(w, "xlsx write failed: %s", err)
		return
	}
}
