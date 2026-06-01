// Package etoro parses eToro account-statement .xlsx exports and computes
// the SC-17 Phase 1 performance history (annual + YTD).
//
// Compute model (verified against real statements):
//   - Financial Summary sheet is the AUTHORITATIVE per-type headline P&L,
//     dividends, fees and interest. Summing Closed Positions Profit(USD) by
//     Type reconciles exactly to the FS "(Profit or Loss)" rows.
//   - Strategy axis (discretionary vs copy) comes from Closed Positions
//     `Copied From` (`-`/empty = discretionary, including discretionary CFDs;
//     a username = copy). Per SC-17 R1, Type=CFD is a wrapper, never the
//     strategy or asset-class signal.
//
// A statement normally covers a single calendar year (full year or YTD). The
// primary year is taken from the statement's End Date; a warning is emitted
// if trades fall outside it.
package etoro

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// AssetPerf is one (asset_type) breakdown within a year.
type AssetPerf struct {
	AssetType        string  `json:"assetType"` // Stocks | ETFs | Crypto | CFDs
	RealisedPnLUSD   float64 `json:"realisedPnlUsd"`
	RealisedPnLEUR   float64 `json:"realisedPnlEur"`
	RealisedDiscUSD  float64 `json:"realisedDiscUsd"`
	RealisedDiscEUR  float64 `json:"realisedDiscEur"`
	RealisedCopyUSD  float64 `json:"realisedCopyUsd"`
	RealisedCopyEUR  float64 `json:"realisedCopyEur"`
	DividendsUSD     float64 `json:"dividendsUsd"`
	DividendsEUR     float64 `json:"dividendsEur"`
	FeesUSD          float64 `json:"feesUsd"`
	FeesEUR          float64 `json:"feesEur"`
	TradeCount       int     `json:"tradeCount"`
}

// YearPerf is the per-year authoritative summary plus its asset breakdown.
type YearPerf struct {
	Year           int         `json:"year"`
	RangeStart     string      `json:"rangeStart"` // YYYY-MM-DD
	RangeEnd       string      `json:"rangeEnd"`   // YYYY-MM-DD
	IsYTD          bool        `json:"isYtd"`
	RealisedPnLUSD float64     `json:"realisedPnlUsd"`
	RealisedPnLEUR float64     `json:"realisedPnlEur"`
	DividendsUSD   float64     `json:"dividendsUsd"`
	DividendsEUR   float64     `json:"dividendsEur"`
	FeesUSD        float64     `json:"feesUsd"`
	FeesEUR        float64     `json:"feesEur"`
	InterestUSD    float64     `json:"interestUsd"`
	InterestEUR    float64     `json:"interestEur"`
	NetUSD         float64     `json:"netUsd"`
	NetEUR         float64     `json:"netEur"`
	ComputedPnLUSD float64     `json:"computedPnlUsd"`
	ComputedPnLEUR float64     `json:"computedPnlEur"`
	ReconDeltaUSD  float64     `json:"reconDeltaUsd"`
	ReconDeltaEUR  float64     `json:"reconDeltaEur"`
	Assets         []AssetPerf `json:"assets"`
}

// Statement is the parsed-and-computed result of one upload.
type Statement struct {
	FileName string     `json:"fileName"`
	Years    []YearPerf `json:"years"`
	Warnings []string   `json:"warnings"`
}

const (
	sheetAccountSummary = "Account Summary"
	sheetClosedPos      = "Closed Positions"
	sheetDividends      = "Dividends"
	sheetFinancialSum   = "Financial Summary"
)

// Parse reads an eToro statement and computes the performance history. `now`
// is used to flag YTD (partial) years.
func Parse(r io.Reader, fileName string, now time.Time) (*Statement, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	have := map[string]bool{}
	for _, s := range f.GetSheetList() {
		have[s] = true
	}
	for _, req := range []string{sheetClosedPos, sheetFinancialSum} {
		if !have[req] {
			return nil, fmt.Errorf("not an eToro statement: missing %q sheet", req)
		}
	}

	st := &Statement{FileName: fileName, Warnings: []string{}}

	rangeStart, rangeEnd := readDateRange(f)
	if rangeEnd == "" {
		return nil, fmt.Errorf("could not read statement date range (Account Summary)")
	}
	primaryYear, _ := strconv.Atoi(rangeEnd[:4])
	full := strings.HasSuffix(rangeEnd, "-12-31")
	isYTD := !full || primaryYear == now.Year()

	yp := YearPerf{
		Year:       primaryYear,
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
		IsYTD:      isYTD,
	}

	// --- Closed Positions: per-type disc/copy realised P&L. ---
	assetAcc := map[string]*AssetPerf{}
	asset := func(t string) *AssetPerf {
		if assetAcc[t] == nil {
			assetAcc[t] = &AssetPerf{AssetType: t}
		}
		return assetAcc[t]
	}
	cp, err := f.GetRows(sheetClosedPos)
	if err != nil {
		return nil, fmt.Errorf("read Closed Positions: %w", err)
	}
	if len(cp) >= 2 {
		idx := headerIndex(cp[0])
		iType, iCopied := idx["Type"], idx["Copied From"]
		iClose := idx["Close Date"]
		iPU, iPE := idx["Profit(USD)"], idx["Profit(EUR)"]
		offYear := 0
		for _, row := range cp[1:] {
			typ := normAssetType(cell(row, iType))
			if typ == "" {
				continue
			}
			if y := ddmmyyyyYear(cell(row, iClose)); y != 0 && y != primaryYear {
				offYear++
			}
			pu := parseNum(cell(row, iPU))
			pe := parseNum(cell(row, iPE))
			a := asset(typ)
			a.TradeCount++
			a.RealisedPnLUSD += pu
			a.RealisedPnLEUR += pe
			if isCopy(cell(row, iCopied)) {
				a.RealisedCopyUSD += pu
				a.RealisedCopyEUR += pe
			} else {
				a.RealisedDiscUSD += pu
				a.RealisedDiscEUR += pe
			}
		}
		if offYear > 0 {
			st.Warnings = append(st.Warnings, fmt.Sprintf(
				"%d closed positions fall outside %d; attributed to %d anyway",
				offYear, primaryYear, primaryYear))
		}
	}

	// --- Dividends: per-type, by year. ---
	if have[sheetDividends] {
		dv, err := f.GetRows(sheetDividends)
		if err == nil && len(dv) >= 2 {
			idx := headerIndex(dv[0])
			iType := idx["Type"]
			iUSD := idx["Net Dividend Received (USD)"]
			iEUR := idx["Net Dividend Received (EUR)"]
			for _, row := range dv[1:] {
				typ := normAssetType(cell(row, iType))
				if typ == "" {
					continue
				}
				a := asset(typ)
				a.DividendsUSD += parseNum(cell(row, iUSD))
				a.DividendsEUR += parseNum(cell(row, iEUR))
			}
		}
	}

	// --- Financial Summary: authoritative headline + per-type fees. ---
	fs, err := f.GetRows(sheetFinancialSum)
	if err != nil {
		return nil, fmt.Errorf("read Financial Summary: %w", err)
	}
	for _, row := range fs {
		if len(row) < 3 {
			continue
		}
		name := strings.TrimSpace(row[0])
		usd := parseNum(cell(row, 1))
		eur := parseNum(cell(row, 2))
		if name == "" || strings.EqualFold(name, "Name") {
			continue
		}
		// Period net = sum of every numeric FS line.
		yp.NetUSD += usd
		yp.NetEUR += eur

		switch {
		case strings.Contains(name, "(Profit or Loss)") &&
			!strings.Contains(name, "Dividend"):
			// Realised trading P&L by wrapper/type. Authoritative headline.
			yp.RealisedPnLUSD += usd
			yp.RealisedPnLEUR += eur
			if t := fsTypeFromPnL(name); t != "" {
				a := asset(t)
				a.RealisedPnLUSD = usd // FS overrides the CP-derived total
				a.RealisedPnLEUR = eur
			}
		case strings.Contains(name, "Dividend"):
			yp.DividendsUSD += usd
			yp.DividendsEUR += eur
		case strings.HasPrefix(name, "Total Interest"):
			yp.InterestUSD += usd
			yp.InterestEUR += eur
		case strings.HasPrefix(name, "Spread fee on"), name == "SDRT Charge",
			strings.HasPrefix(name, "Fees ("):
			yp.FeesUSD += usd
			yp.FeesEUR += eur
			if t := fsTypeFromFee(name); t != "" {
				a := asset(t)
				a.FeesUSD += usd
				a.FeesEUR += eur
			}
		}
	}

	// Reconciliation: CP-derived realised P&L vs FS headline.
	var cpUSD, cpEUR float64
	for _, a := range assetAcc {
		cpUSD += a.RealisedDiscUSD + a.RealisedCopyUSD
		cpEUR += a.RealisedDiscEUR + a.RealisedCopyEUR
	}
	yp.ComputedPnLUSD = round2(cpUSD)
	yp.ComputedPnLEUR = round2(cpEUR)
	yp.ReconDeltaUSD = round2(yp.RealisedPnLUSD - cpUSD)
	yp.ReconDeltaEUR = round2(yp.RealisedPnLEUR - cpEUR)
	if abs(yp.ReconDeltaEUR) > 1.0 {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"realised P&L reconciliation delta €%.2f (Financial Summary vs closed positions)",
			yp.ReconDeltaEUR))
	}

	// Finalise asset list (rounded, sorted by a stable display order).
	order := map[string]int{"Stocks": 0, "ETFs": 1, "Crypto": 2, "CFDs": 3}
	for _, a := range assetAcc {
		roundAsset(a)
		yp.Assets = append(yp.Assets, *a)
	}
	sort.Slice(yp.Assets, func(i, j int) bool {
		oi, oj := order[yp.Assets[i].AssetType], order[yp.Assets[j].AssetType]
		if oi != oj {
			return oi < oj
		}
		return yp.Assets[i].AssetType < yp.Assets[j].AssetType
	})

	roundYear(&yp)
	st.Years = []YearPerf{yp}
	return st, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func headerIndex(header []string) map[string]int {
	m := map[string]int{}
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// parseNum tolerates eToro's blanks: "-", "", "N/A", thousands separators.
func parseNum(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || strings.EqualFold(s, "N/A") {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSuffix(s, "%")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// ddmmyyyyYear extracts the year from a "DD/MM/YYYY[ HH:MM:SS]" string.
func ddmmyyyyYear(s string) int {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return 0
	}
	parts := strings.Split(s[:10], "/")
	if len(parts) != 3 {
		return 0
	}
	y, _ := strconv.Atoi(parts[2])
	return y
}

// isCopy: a position is "copy" when Copied From names a portfolio; "-" or
// empty is discretionary (incl. discretionary CFDs). SC-17 R1.
func isCopy(copiedFrom string) bool {
	c := strings.TrimSpace(copiedFrom)
	return c != "" && c != "-"
}

// normAssetType maps eToro's per-row Type values onto our four buckets.
func normAssetType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "stocks", "stock":
		return "Stocks"
	case "etf", "etfs":
		return "ETFs"
	case "crypto":
		return "Crypto"
	case "cfd", "cfds":
		return "CFDs"
	default:
		return ""
	}
}

func fsTypeFromPnL(name string) string {
	switch {
	case strings.HasPrefix(name, "Stocks "):
		return "Stocks"
	case strings.HasPrefix(name, "ETFs "):
		return "ETFs"
	case strings.HasPrefix(name, "Crypto "):
		return "Crypto"
	case strings.HasPrefix(name, "CFDs "):
		return "CFDs"
	default:
		return ""
	}
}

func fsTypeFromFee(name string) string {
	switch {
	case name == "SDRT Charge", strings.Contains(name, "on stocks"):
		return "Stocks"
	case strings.Contains(name, "on ETFs"):
		return "ETFs"
	case strings.Contains(name, "on crypto"):
		return "Crypto"
	case strings.Contains(name, "on CFDs"):
		return "CFDs"
	default:
		return "" // general fees stay year-level only
	}
}

// readDateRange pulls Start/End Date from Account Summary, returns YYYY-MM-DD.
func readDateRange(f *excelize.File) (start, end string) {
	rows, err := f.GetRows(sheetAccountSummary)
	if err != nil {
		return "", ""
	}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		label := strings.TrimSpace(row[0])
		val := strings.TrimSpace(row[1])
		switch label {
		case "Start Date":
			start = isoFromDDMMYYYY(val)
		case "End Date":
			end = isoFromDDMMYYYY(val)
		}
	}
	return start, end
}

func isoFromDDMMYYYY(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 10 {
		return ""
	}
	parts := strings.Split(s[:10], "/")
	if len(parts) != 3 {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s", parts[2], parts[1], parts[0])
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func round2(f float64) float64 {
	return float64(int64(f*100+sign(f)*0.5)) / 100
}

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

func roundAsset(a *AssetPerf) {
	a.RealisedPnLUSD = round2(a.RealisedPnLUSD)
	a.RealisedPnLEUR = round2(a.RealisedPnLEUR)
	a.RealisedDiscUSD = round2(a.RealisedDiscUSD)
	a.RealisedDiscEUR = round2(a.RealisedDiscEUR)
	a.RealisedCopyUSD = round2(a.RealisedCopyUSD)
	a.RealisedCopyEUR = round2(a.RealisedCopyEUR)
	a.DividendsUSD = round2(a.DividendsUSD)
	a.DividendsEUR = round2(a.DividendsEUR)
	a.FeesUSD = round2(a.FeesUSD)
	a.FeesEUR = round2(a.FeesEUR)
}

func roundYear(y *YearPerf) {
	y.RealisedPnLUSD = round2(y.RealisedPnLUSD)
	y.RealisedPnLEUR = round2(y.RealisedPnLEUR)
	y.DividendsUSD = round2(y.DividendsUSD)
	y.DividendsEUR = round2(y.DividendsEUR)
	y.FeesUSD = round2(y.FeesUSD)
	y.FeesEUR = round2(y.FeesEUR)
	y.InterestUSD = round2(y.InterestUSD)
	y.InterestEUR = round2(y.InterestEUR)
	y.NetUSD = round2(y.NetUSD)
	y.NetEUR = round2(y.NetEUR)
}
