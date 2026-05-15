// Package persistence parses and writes the master xlsx file.
//
// Master format v2: a single .xlsx workbook with three sheets:
//
//	stocks_etfs  Portfolio rows (all fields, including Category)
//	crypto       Crypto Portfolio rows (Phase 4 schema, all fields)
//	meta         schema version, timestamps, save location label, FX snapshot
//
// The import path is tolerant: it accepts the simple 5-column legacy stocks
// format (Stock Name / Ticker / Invested ($) / Avg Open Price / Current Price)
// and the original 8-column crypto export (sample_file.xlsx). Sheets are
// detected by name first, then by inspecting headers, so renamed sheets still
// work.
package persistence

import (
	"fmt"
	"ft/internal/domain"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	SchemaVersion = 2

	sheetStocks = "stocks_etfs"
	sheetCrypto = "crypto"
	sheetMeta   = "meta"
)

// ImportResult is what the parser returns on success.
type ImportResult struct {
	Stocks               []*domain.StockHolding `json:"stocks"`
	Crypto               []*domain.CryptoHolding `json:"crypto"`
	FXSnapshotEURUSD     *float64               `json:"fxSnapshotEurUsd,omitempty"`
	HasCrypto            bool                   `json:"hasCrypto"`
	IsMasterFormatStocks bool                   `json:"isMasterFormatStocks"`
	Skipped              []SkippedRow           `json:"skipped"`
	Warnings             []string               `json:"warnings"`
	SchemaVersion        *int                   `json:"schemaVersion,omitempty"`
}

type SkippedRow struct {
	Reason string `json:"reason"`
	// We don't echo the raw row — keep the diagnostic lightweight.
	Row int `json:"row"`
}

var totalsRowRE = regexp.MustCompile(`(?i)^portfolio totals?$`)

// =============================================================================
// PARSE
// =============================================================================

// ParseXLSX reads a master xlsx from r and returns an ImportResult.
func ParseXLSX(r io.Reader) (*ImportResult, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	out := &ImportResult{
		Stocks:   []*domain.StockHolding{},
		Crypto:   []*domain.CryptoHolding{},
		Skipped:  []SkippedRow{},
		Warnings: []string{},
	}

	// Resolve sheets by name preference, falling back to header probing.
	sheets := f.GetSheetList()
	stockSheet := findSheet(f, sheets, sheetStocks, hasStockHeaders)
	cryptoSheet := findSheet(f, sheets, sheetCrypto, hasCryptoHeaders)
	metaSheet := findSheet(f, sheets, sheetMeta, hasMetaHeaders)

	if stockSheet != "" {
		s, isMaster, err := parseStocks(f, stockSheet, out)
		if err != nil {
			return nil, fmt.Errorf("parse stocks: %w", err)
		}
		out.Stocks = s
		out.IsMasterFormatStocks = isMaster
	}

	if cryptoSheet != "" {
		c, err := parseCrypto(f, cryptoSheet, out)
		if err != nil {
			return nil, fmt.Errorf("parse crypto: %w", err)
		}
		out.Crypto = c
		out.HasCrypto = len(c) > 0
	}

	if metaSheet != "" {
		fx, sv := parseMeta(f, metaSheet)
		out.FXSnapshotEURUSD = fx
		out.SchemaVersion = sv
	}

	return out, nil
}

// findSheet returns the first matching sheet name: an exact match against
// `preferred`, otherwise the first sheet whose headers satisfy `probe`.
// Returns "" if neither matches.
func findSheet(f *excelize.File, names []string, preferred string, probe func(*excelize.File, string) bool) string {
	for _, n := range names {
		if n == preferred {
			return n
		}
	}
	for _, n := range names {
		if probe(f, n) {
			return n
		}
	}
	return ""
}

// rowsAsMaps reads the sheet and returns rows as map[header]string.
// Empty rows are dropped.
func rowsAsMaps(f *excelize.File, sheet string) ([]map[string]string, []string, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) < 2 {
		return nil, nil, nil
	}
	headers := rows[0]
	out := make([]map[string]string, 0, len(rows)-1)
	for _, r := range rows[1:] {
		m := make(map[string]string, len(headers))
		anyVal := false
		for i, h := range headers {
			if i < len(r) {
				v := strings.TrimSpace(r[i])
				m[h] = v
				if v != "" {
					anyVal = true
				}
			}
		}
		if anyVal {
			out = append(out, m)
		}
	}
	return out, headers, nil
}

func parseStocks(f *excelize.File, sheet string, out *ImportResult) ([]*domain.StockHolding, bool, error) {
	rows, _, err := rowsAsMaps(f, sheet)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}

	sample := rows[0]
	_, hasStop := sample["Stop Loss"]
	_, hasStrategy := sample["Strategy Note"]
	isMaster := hasStop || hasStrategy

	stocks := []*domain.StockHolding{}
	for i, row := range rows {
		name := pickStr(row, "Stock Name", "name", "Stock", "Name")
		if name == "" {
			out.Skipped = append(out.Skipped, SkippedRow{Reason: "Missing Stock Name", Row: i + 2})
			continue
		}
		if totalsRowRE.MatchString(name) {
			out.Skipped = append(out.Skipped, SkippedRow{Reason: "Totals row skipped", Row: i + 2})
			continue
		}
		tickerStr := pickStr(row, "Ticker", "ticker", "Symbol", "SYMBOL")
		var ticker *string
		if tickerStr != "" && !strings.EqualFold(tickerStr, "n/a") {
			t := tickerStr
			ticker = &t
		}

		invested := pickNum(row, "Invested ($)", "investedUsd", "Invested")
		avgOpen := pickNum(row, "Avg Open Price", "avgOpenPrice", "Avg Open")
		current := pickNum(row, "Current Price", "currentPrice")
		if avgOpen == nil && current == nil {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("%s: imported without price data — fill in Avg Open and Current Price to compute P&L.", name))
		}

		invUSD := 0.0
		if invested != nil {
			invUSD = *invested
		}

		h := &domain.StockHolding{
			Name:         name,
			Ticker:       ticker,
			Category:     ptrStrIfSet(pickStr(row, "Category", "category")),
			Sector:       ptrStrIfSet(pickStr(row, "Sector")),
			InvestedUSD:  invUSD,
			AvgOpenPrice: avgOpen,
			CurrentPrice: current,
		}
		if isMaster {
			h.RSI14 = pickNum(row, "RSI(14)", "rsi14", "RSI")
			h.MA50 = pickNum(row, "MA50", "ma50")
			h.MA200 = pickNum(row, "MA200", "ma200")
			h.GoldenCross = pickBool(row, "Golden Cross", "goldenCross")
			h.Support = pickNum(row, "Support", "support")
			h.Resistance = pickNum(row, "Resistance", "resistance")
			h.AnalystTarget = pickNum(row, "Analyst Target", "analystTarget")
			h.ProposedEntry = pickNum(row, "Proposed Entry", "proposedEntry")
			h.TechnicalSetup = ptrStrIfSet(pickStr(row, "Technical Setup"))
			h.AnalystRRView = ptrStrIfSet(pickStr(row, "Analyst RR View"))
			h.StopLoss = pickNum(row, "Stop Loss", "stopLoss")
			h.TakeProfit = pickNum(row, "Take Profit", "takeProfit")
			h.StrategyNote = pickStr(row, "Strategy Note")
		}
		stocks = append(stocks, h)
	}
	return stocks, isMaster, nil
}

func parseCrypto(f *excelize.File, sheet string, out *ImportResult) ([]*domain.CryptoHolding, error) {
	rows, _, err := rowsAsMaps(f, sheet)
	if err != nil {
		return nil, err
	}
	crypto := []*domain.CryptoHolding{}
	for i, row := range rows {
		name := pickStr(row, "Asset", "ASSET", "name", "Name")
		if name == "" {
			out.Skipped = append(out.Skipped, SkippedRow{Reason: "Missing Asset name", Row: i + 2})
			continue
		}
		if totalsRowRE.MatchString(name) {
			out.Skipped = append(out.Skipped, SkippedRow{Reason: "Totals row skipped", Row: i + 2})
			continue
		}
		symbol := pickStr(row, "Symbol", "SYMBOL", "symbol")
		if symbol == "" {
			out.Skipped = append(out.Skipped, SkippedRow{
				Reason: fmt.Sprintf("Missing Symbol for %s", name), Row: i + 2,
			})
			continue
		}
		symU := strings.ToUpper(symbol)

		classRaw := strings.ToLower(pickStr(row, "Class", "classification"))
		classification := "alt"
		if classRaw == "core" || symU == "BTC" || symU == "ETH" {
			classification = "core"
		}

		qtyHeld := pickNumDefault(row, 0, "Qty Held", "Quantity Held (Units)", "quantityHeld")
		qtyStaked := pickNumDefault(row, 0, "Qty Staked", "quantityStaked")
		avgEur := pickNum(row, "Avg Buy EUR", "Average Trading Price", "avgBuyEur")
		costEur := pickNum(row, "Cost Basis EUR", "Total Cost at Purchase", "costBasisEur")
		curEur := pickNum(row, "Current Price EUR", "CURRENT PRICE EUR", "currentPriceEur")
		valEur := pickNum(row, "Current Value EUR", "CURRENT VALUE EUR", "currentValueEur")
		avgUsd := pickNum(row, "Avg Buy USD", "avgBuyUsd")
		costUsd := pickNum(row, "Cost Basis USD", "costBasisUsd")
		curUsd := pickNum(row, "Current Price USD", "currentPriceUsd")
		valUsd := pickNum(row, "Current Value USD", "currentValueUsd")

		if avgEur == nil && costEur == nil {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("%s: cost basis unknown — P&L can't be computed.", name))
		}

		h := &domain.CryptoHolding{
			Name:            name,
			Symbol:          symU,
			Classification:  classification,
			Category:        ptrStrIfSet(pickStr(row, "Category", "category")),
			Wallet:          ptrStrIfSet(pickStr(row, "Wallet", "wallet")),
			QuantityHeld:    qtyHeld,
			QuantityStaked:  qtyStaked,
			AvgBuyEUR:       avgEur,
			CostBasisEUR:    costEur,
			CurrentPriceEUR: curEur,
			CurrentValueEUR: valEur,
			AvgBuyUSD:       avgUsd,
			CostBasisUSD:    costUsd,
			CurrentPriceUSD: curUsd,
			CurrentValueUSD: valUsd,
			RSI14:           pickNum(row, "RSI(14)", "rsi14"),
			Change7dPct:     pickNum(row, "Change 7d %", "change7dPct"),
			Change30dPct:    pickNum(row, "Change 30d %", "change30dPct"),
			StrategyNote:    pickStr(row, "Strategy Note"),
		}
		crypto = append(crypto, h)
	}
	return crypto, nil
}

func parseMeta(f *excelize.File, sheet string) (*float64, *int) {
	rows, _, err := rowsAsMaps(f, sheet)
	if err != nil || len(rows) == 0 {
		return nil, nil
	}
	row := rows[0]
	fx := pickNum(row, "fxSnapshotEurUsd")
	sv := pickNum(row, "schemaVersion")
	var svPtr *int
	if sv != nil {
		v := int(*sv)
		svPtr = &v
	}
	return fx, svPtr
}

// =============================================================================
// EXPORT
// =============================================================================

// WriteXLSX renders the current state as a master xlsx file to w.
func WriteXLSX(w io.Writer, stocks []*domain.StockHolding, crypto []*domain.CryptoHolding, fxEURUSD *float64) error {
	f := excelize.NewFile()
	defer f.Close()

	// Always start by deleting the default Sheet1, replaced with named sheets.
	defaultSheet := f.GetSheetName(0)

	// ---- stocks_etfs ----
	if _, err := f.NewSheet(sheetStocks); err != nil {
		return err
	}
	stockHeaders := []string{
		"Stock Name", "Ticker", "Category", "Invested ($)", "Avg Open Price",
		"Current Price", "Sector", "RSI(14)", "MA50", "MA200", "Golden Cross",
		"Support", "Resistance", "Analyst Target", "Proposed Entry",
		"Technical Setup", "Analyst RR View", "Stop Loss", "Take Profit",
		"Strategy Note",
	}
	if err := writeHeaderRow(f, sheetStocks, stockHeaders); err != nil {
		return err
	}
	for i, h := range stocks {
		row := i + 2
		writeCell(f, sheetStocks, "A", row, h.Name)
		writeCell(f, sheetStocks, "B", row, strDeref(h.Ticker))
		writeCell(f, sheetStocks, "C", row, strDeref(h.Category))
		writeCell(f, sheetStocks, "D", row, h.InvestedUSD)
		writeCell(f, sheetStocks, "E", row, numDeref(h.AvgOpenPrice))
		writeCell(f, sheetStocks, "F", row, numDeref(h.CurrentPrice))
		writeCell(f, sheetStocks, "G", row, strDeref(h.Sector))
		writeCell(f, sheetStocks, "H", row, numDeref(h.RSI14))
		writeCell(f, sheetStocks, "I", row, numDeref(h.MA50))
		writeCell(f, sheetStocks, "J", row, numDeref(h.MA200))
		writeCell(f, sheetStocks, "K", row, boolYesNo(h.GoldenCross))
		writeCell(f, sheetStocks, "L", row, numDeref(h.Support))
		writeCell(f, sheetStocks, "M", row, numDeref(h.Resistance))
		writeCell(f, sheetStocks, "N", row, numDeref(h.AnalystTarget))
		writeCell(f, sheetStocks, "O", row, numDeref(h.ProposedEntry))
		writeCell(f, sheetStocks, "P", row, strDeref(h.TechnicalSetup))
		writeCell(f, sheetStocks, "Q", row, strDeref(h.AnalystRRView))
		writeCell(f, sheetStocks, "R", row, numDeref(h.StopLoss))
		writeCell(f, sheetStocks, "S", row, numDeref(h.TakeProfit))
		writeCell(f, sheetStocks, "T", row, h.StrategyNote)
	}

	// ---- crypto ----
	if _, err := f.NewSheet(sheetCrypto); err != nil {
		return err
	}
	cryptoHeaders := []string{
		"Asset", "Symbol", "Class", "Category", "Wallet",
		"Qty Held", "Qty Staked",
		"Avg Buy EUR", "Cost Basis EUR", "Current Price EUR", "Current Value EUR",
		"Avg Buy USD", "Cost Basis USD", "Current Price USD", "Current Value USD",
		"RSI(14)", "Change 7d %", "Change 30d %", "Strategy Note",
	}
	if err := writeHeaderRow(f, sheetCrypto, cryptoHeaders); err != nil {
		return err
	}
	for i, c := range crypto {
		row := i + 2
		writeCell(f, sheetCrypto, "A", row, c.Name)
		writeCell(f, sheetCrypto, "B", row, c.Symbol)
		writeCell(f, sheetCrypto, "C", row, c.Classification)
		writeCell(f, sheetCrypto, "D", row, strDeref(c.Category))
		writeCell(f, sheetCrypto, "E", row, strDeref(c.Wallet))
		writeCell(f, sheetCrypto, "F", row, c.QuantityHeld)
		writeCell(f, sheetCrypto, "G", row, c.QuantityStaked)
		writeCell(f, sheetCrypto, "H", row, numDeref(c.AvgBuyEUR))
		writeCell(f, sheetCrypto, "I", row, numDeref(c.CostBasisEUR))
		writeCell(f, sheetCrypto, "J", row, numDeref(c.CurrentPriceEUR))
		writeCell(f, sheetCrypto, "K", row, numDeref(c.CurrentValueEUR))
		writeCell(f, sheetCrypto, "L", row, numDeref(c.AvgBuyUSD))
		writeCell(f, sheetCrypto, "M", row, numDeref(c.CostBasisUSD))
		writeCell(f, sheetCrypto, "N", row, numDeref(c.CurrentPriceUSD))
		writeCell(f, sheetCrypto, "O", row, numDeref(c.CurrentValueUSD))
		writeCell(f, sheetCrypto, "P", row, numDeref(c.RSI14))
		writeCell(f, sheetCrypto, "Q", row, numDeref(c.Change7dPct))
		writeCell(f, sheetCrypto, "R", row, numDeref(c.Change30dPct))
		writeCell(f, sheetCrypto, "S", row, c.StrategyNote)
	}

	// ---- meta ----
	if _, err := f.NewSheet(sheetMeta); err != nil {
		return err
	}
	if err := writeHeaderRow(f, sheetMeta, []string{
		"schemaVersion", "savedAt", "saveLocationLabel", "fxSnapshotEurUsd",
	}); err != nil {
		return err
	}
	writeCell(f, sheetMeta, "A", 2, SchemaVersion)
	writeCell(f, sheetMeta, "B", 2, time.Now().UTC().Format(time.RFC3339))
	writeCell(f, sheetMeta, "C", 2, "")
	if fxEURUSD != nil {
		writeCell(f, sheetMeta, "D", 2, *fxEURUSD)
	}

	// Drop the default sheet now that the named ones exist.
	if defaultSheet != "" {
		_ = f.DeleteSheet(defaultSheet)
	}

	// Set stocks_etfs as the active sheet.
	if idx, err := f.GetSheetIndex(sheetStocks); err == nil {
		f.SetActiveSheet(idx)
	}

	return f.Write(w)
}

func writeHeaderRow(f *excelize.File, sheet string, headers []string) error {
	for i, h := range headers {
		col, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, fmt.Sprintf("%s1", col), h); err != nil {
			return err
		}
	}
	return nil
}

func writeCell(f *excelize.File, sheet, col string, row int, v any) {
	addr := fmt.Sprintf("%s%d", col, row)
	_ = f.SetCellValue(sheet, addr, v)
}

// =============================================================================
// Header probing
// =============================================================================

func sheetHeaders(f *excelize.File, sheet string) []string {
	rows, err := f.GetRows(sheet)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rows[0]
}
func headerSetContains(headers []string, needles ...string) bool {
	for _, h := range headers {
		for _, n := range needles {
			if strings.EqualFold(strings.TrimSpace(h), n) {
				return true
			}
		}
	}
	return false
}
func hasStockHeaders(f *excelize.File, sheet string) bool {
	hs := sheetHeaders(f, sheet)
	return headerSetContains(hs, "Stock Name", "Avg Open Price")
}
func hasCryptoHeaders(f *excelize.File, sheet string) bool {
	hs := sheetHeaders(f, sheet)
	return (headerSetContains(hs, "Asset", "ASSET") || headerSetContains(hs, "Symbol", "SYMBOL")) &&
		headerSetContains(hs, "Symbol", "SYMBOL", "Asset", "ASSET")
}
func hasMetaHeaders(f *excelize.File, sheet string) bool {
	return headerSetContains(sheetHeaders(f, sheet), "schemaVersion")
}

// =============================================================================
// Lenient picking
// =============================================================================

func pickStr(row map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok {
			s := strings.TrimSpace(v)
			if s != "" && !strings.EqualFold(s, "n/a") {
				return s
			}
		}
	}
	return ""
}

var numStripRE = regexp.MustCompile(`[$,€\s]`)

func pickNum(row map[string]string, keys ...string) *float64 {
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			continue
		}
		s := strings.TrimSpace(v)
		if s == "" || strings.EqualFold(s, "n/a") {
			continue
		}
		clean := numStripRE.ReplaceAllString(s, "")
		if n, err := strconv.ParseFloat(clean, 64); err == nil {
			return &n
		}
	}
	return nil
}

func pickNumDefault(row map[string]string, def float64, keys ...string) float64 {
	if v := pickNum(row, keys...); v != nil {
		return *v
	}
	return def
}

func pickBool(row map[string]string, keys ...string) *bool {
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			continue
		}
		s := strings.ToLower(strings.TrimSpace(v))
		if s == "" || s == "n/a" {
			continue
		}
		switch s {
		case "yes", "y", "true", "1":
			t := true
			return &t
		case "no", "n", "false", "0":
			f := false
			return &f
		}
	}
	return nil
}

// =============================================================================
// Output helpers
// =============================================================================

func ptrStrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func numDeref(p *float64) any {
	if p == nil {
		return ""
	}
	return *p
}
func boolYesNo(p *bool) string {
	if p == nil {
		return ""
	}
	if *p {
		return "yes"
	}
	return "no"
}
