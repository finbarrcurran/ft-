// SC-29 — Crypto tab export to a multi-sheet .xlsx.
//
//	GET /api/crypto/export.xlsx
//
// Streams a native Excel workbook (Holdings + Theses & Scores + Meta sheets)
// for the Crypto tab. Export-only (imports stay on the Import tab, D29.1).
//
// SC-22 (S-29a): holdings are loaded through s.loadCryptoHoldings, which masks
// the figures when demo mode is on — so the *exported file payload* carries the
// same synthetic values the UI shows, not the real book. The export is not a
// back-door around the mask.

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"ft/internal/persistence"
)

// GET /api/crypto/export.xlsx
func (s *Server) handleCryptoExportXLSX(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	// Demo-aware load (S-29a): masked when demo mode is on.
	holdings, err := s.loadCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}

	// Locked + draft crypto theses (framework scores — not position money, so
	// exported as-is in both modes). An empty list still produces the sheet
	// with headers (S-29c).
	rows, err := s.cryptoTheses.ListAll(r.Context())
	if mapStoreError(w, err) {
		return
	}
	theses := make([]persistence.CryptoThesisExportRow, 0, len(rows))
	for _, t := range rows {
		theses = append(theses, persistence.CryptoThesisExportRow{
			Symbol:         t.CoinSymbol,
			Name:           t.CoinName,
			AdapterSlug:    t.AdapterSlug,
			AdapterType:    string(t.AdapterType),
			ScorecardType:  string(t.ScorecardType),
			Score:          t.Score,
			MaxScore:       t.MaxScore,
			Band:           string(t.Band),
			Status:         string(t.Status),
			HoldingHorizon: string(t.HoldingHorizon),
			BTCBeta:        string(t.BTCBeta),
			Version:        t.Version,
			NextReviewDate: t.NextReviewDate,
		})
	}

	var fx *float64
	if v, err := s.store.GetMeta(r.Context(), "fx_snapshot_eur_usd"); err == nil {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			fx = &f
		}
	}

	name := fmt.Sprintf("ft-crypto-%s.xlsx", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")

	if err := persistence.WriteCryptoXLSX(w, holdings, theses, fx); err != nil {
		// Headers already sent — best effort error in the body.
		_, _ = fmt.Fprintf(w, "crypto xlsx write failed: %s", err)
		return
	}
}
