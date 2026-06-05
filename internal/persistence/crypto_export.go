// SC-29 — Crypto tab export to a multi-sheet .xlsx.
//
// The Stocks/Watchlist tabs export a flat .csv (GET /api/export.csv?tab=...).
// The Crypto tab gets a richer, Excel-native multi-sheet workbook instead
// (D29.2): a Holdings sheet whose columns mirror the Stocks export's
// semantics (crypto-equivalent), plus a Theses/Scores sheet for the locked
// crypto theses, plus a Meta sheet.
//
// IMPORTANT (SC-22 / S-29a): this writer never fetches data itself — the
// caller passes already-loaded (and, in demo mode, already-masked) holdings.
// The masking is applied upstream at the holdings-load chokepoint
// (server.loadCryptoHoldings), so a demo-mode export carries the same
// synthetic figures the UI shows — the export is not a back-door around the
// mask. Thesis SCORES are framework/methodology values (not position money),
// so they are exported as-is in both modes.
//
// Empty datasets still get their sheet with headers (S-29c) so the file shape
// is predictable across exports.

package persistence

import (
	"io"
	"time"

	"ft/internal/domain"

	"github.com/xuri/excelize/v2"
)

// CryptoThesisExportRow is a flat, presentation-ready view of a locked crypto
// thesis for the Theses sheet. The server maps cryptotheses.ThesisRow into
// this so the persistence package stays decoupled from the scoring package
// (no import cycle, no cross-package type bleed).
type CryptoThesisExportRow struct {
	Symbol         string
	Name           string
	AdapterSlug    string
	AdapterType    string
	ScorecardType  string
	Score          int
	MaxScore       int
	Band           string
	Status         string
	HoldingHorizon string
	BTCBeta        string
	Version        string
	NextReviewDate string
}

const (
	sheetCryptoHoldings = "Holdings"
	sheetCryptoTheses   = "Theses & Scores"
	sheetCryptoMeta     = "Meta"
)

// WriteCryptoXLSX streams a multi-sheet crypto workbook to w.
//
// Sheets:
//   - Holdings        — one row per crypto position (mirrors the Stocks export)
//   - Theses & Scores — one row per crypto thesis (version/status/band/score)
//   - Meta            — schema version, save time, FX snapshot
func WriteCryptoXLSX(w io.Writer, holdings []*domain.CryptoHolding, theses []CryptoThesisExportRow, fxEURUSD *float64) error {
	f := excelize.NewFile()
	defer f.Close()

	defaultSheet := f.GetSheetName(0)

	// ---- Holdings ----
	if _, err := f.NewSheet(sheetCryptoHoldings); err != nil {
		return err
	}
	holdingHeaders := []string{
		"Asset", "Symbol", "Class", "Category", "Wallet", "Current Location",
		"Qty Held", "Qty Staked",
		"Avg Buy EUR", "Cost Basis EUR", "Current Price EUR", "Current Value EUR",
		"Avg Buy USD", "Cost Basis USD", "Current Price USD", "Current Value USD",
		"RSI(14)", "Daily Change %", "Change 7d %", "Change 30d %",
		"Vol Tier", "Volatility 12m %", "Thesis Link", "Note",
	}
	if err := writeHeaderRow(f, sheetCryptoHoldings, holdingHeaders); err != nil {
		return err
	}
	for i, c := range holdings {
		row := i + 2
		writeCell(f, sheetCryptoHoldings, "A", row, c.Name)
		writeCell(f, sheetCryptoHoldings, "B", row, c.Symbol)
		writeCell(f, sheetCryptoHoldings, "C", row, c.Classification)
		writeCell(f, sheetCryptoHoldings, "D", row, strDeref(c.Category))
		writeCell(f, sheetCryptoHoldings, "E", row, strDeref(c.Wallet))
		writeCell(f, sheetCryptoHoldings, "F", row, strDeref(c.CurrentLocation))
		writeCell(f, sheetCryptoHoldings, "G", row, c.QuantityHeld)
		writeCell(f, sheetCryptoHoldings, "H", row, c.QuantityStaked)
		writeCell(f, sheetCryptoHoldings, "I", row, numDeref(c.AvgBuyEUR))
		writeCell(f, sheetCryptoHoldings, "J", row, numDeref(c.CostBasisEUR))
		writeCell(f, sheetCryptoHoldings, "K", row, numDeref(c.CurrentPriceEUR))
		writeCell(f, sheetCryptoHoldings, "L", row, numDeref(c.CurrentValueEUR))
		writeCell(f, sheetCryptoHoldings, "M", row, numDeref(c.AvgBuyUSD))
		writeCell(f, sheetCryptoHoldings, "N", row, numDeref(c.CostBasisUSD))
		writeCell(f, sheetCryptoHoldings, "O", row, numDeref(c.CurrentPriceUSD))
		writeCell(f, sheetCryptoHoldings, "P", row, numDeref(c.CurrentValueUSD))
		writeCell(f, sheetCryptoHoldings, "Q", row, numDeref(c.RSI14))
		writeCell(f, sheetCryptoHoldings, "R", row, numDeref(c.DailyChangePct))
		writeCell(f, sheetCryptoHoldings, "S", row, numDeref(c.Change7dPct))
		writeCell(f, sheetCryptoHoldings, "T", row, numDeref(c.Change30dPct))
		writeCell(f, sheetCryptoHoldings, "U", row, c.VolTier)
		writeCell(f, sheetCryptoHoldings, "V", row, numDeref(c.Volatility12mPct))
		writeCell(f, sheetCryptoHoldings, "W", row, strDeref(c.ThesisLink))
		writeCell(f, sheetCryptoHoldings, "X", row, strDeref(c.Note))
	}

	// ---- Theses & Scores ----
	if _, err := f.NewSheet(sheetCryptoTheses); err != nil {
		return err
	}
	thesisHeaders := []string{
		"Symbol", "Name", "Adapter", "Adapter Type", "Scorecard Type",
		"Score", "Max Score", "Band", "Status", "Holding Horizon",
		"BTC Beta", "Version", "Next Review",
	}
	if err := writeHeaderRow(f, sheetCryptoTheses, thesisHeaders); err != nil {
		return err
	}
	for i, t := range theses {
		row := i + 2
		writeCell(f, sheetCryptoTheses, "A", row, t.Symbol)
		writeCell(f, sheetCryptoTheses, "B", row, t.Name)
		writeCell(f, sheetCryptoTheses, "C", row, t.AdapterSlug)
		writeCell(f, sheetCryptoTheses, "D", row, t.AdapterType)
		writeCell(f, sheetCryptoTheses, "E", row, t.ScorecardType)
		writeCell(f, sheetCryptoTheses, "F", row, t.Score)
		writeCell(f, sheetCryptoTheses, "G", row, t.MaxScore)
		writeCell(f, sheetCryptoTheses, "H", row, t.Band)
		writeCell(f, sheetCryptoTheses, "I", row, t.Status)
		writeCell(f, sheetCryptoTheses, "J", row, t.HoldingHorizon)
		writeCell(f, sheetCryptoTheses, "K", row, t.BTCBeta)
		writeCell(f, sheetCryptoTheses, "L", row, t.Version)
		writeCell(f, sheetCryptoTheses, "M", row, t.NextReviewDate)
	}

	// ---- Meta ----
	if _, err := f.NewSheet(sheetCryptoMeta); err != nil {
		return err
	}
	if err := writeHeaderRow(f, sheetCryptoMeta, []string{
		"schemaVersion", "savedAt", "exportKind", "fxSnapshotEurUsd",
	}); err != nil {
		return err
	}
	writeCell(f, sheetCryptoMeta, "A", 2, SchemaVersion)
	writeCell(f, sheetCryptoMeta, "B", 2, time.Now().UTC().Format(time.RFC3339))
	writeCell(f, sheetCryptoMeta, "C", 2, "crypto-export")
	if fxEURUSD != nil {
		writeCell(f, sheetCryptoMeta, "D", 2, *fxEURUSD)
	}

	if defaultSheet != "" {
		_ = f.DeleteSheet(defaultSheet)
	}
	if idx, err := f.GetSheetIndex(sheetCryptoHoldings); err == nil {
		f.SetActiveSheet(idx)
	}

	return f.Write(w)
}
