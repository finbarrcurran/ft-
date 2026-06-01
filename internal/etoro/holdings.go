// SC-17 Phase 2 — current-holdings reconstruction from an eToro statement.
//
// An eToro statement is a transaction record, not a portfolio snapshot, so
// "current holdings" are DERIVED (hence Phase 2 is propose-and-approve):
//
//   - A position is still OPEN at statement end if its Position ID appears as an
//     "Open Position" row in Account Activity but never appears in the Closed
//     Positions sheet. (Validated on real statements: 74 open IDs → 32 holdings.)
//   - Users scale in, so multiple open lots share an instrument (GLD = 20 lots,
//     SLV = 14). We AGGREGATE to one holding per instrument: sum units, sum the
//     USD amount invested, weighted-average price = invested / units.
//   - Account Activity has no ISIN column, so the ISIN is recovered by joining
//     the instrument's (normalized) ticker back to the Closed Positions sheet —
//     only the ~44-50% of holdings whose ticker also closed at least once on
//     this statement are ISIN-seedable (SC-17 R2).
//
// Two instrument formats (SC-17 R3, S-17b):
//   - Closed Positions.Action = "Company Name (TICKER)"
//   - Account Activity.Details = "TICKER/CURRENCY" (e.g. GLD/USD, RHM.DE/EUR,
//     4063.T/JPY, BHP.ASX/AUD, BAYN.de/EUR). Suffix casing varies (.de vs .DE,
//     .l vs .L) so both are normalized case-insensitively before matching.
//
// Routing axis (SC-17 R1): route by UNDERLYING, never by the CFD wrapper.
// Type=CFD is a display flag only — GLD/SLV/XAU CFDs reconcile against FT Stocks
// (asset-hedge); only crypto underlyings route to the Crypto tab.

package etoro

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

const sheetAccountActivity = "Account Activity"

// Holding is one reconstructed current position (multi-lot aggregated).
type Holding struct {
	Name        string  `json:"name"`        // company name (from Closed Positions; falls back to ticker)
	Ticker      string  `json:"ticker"`      // normalized, e.g. "RHM.DE", "GLD"
	Currency    string  `json:"currency"`    // eToro currency suffix hint ("USD", "EUR", "GBX"...)
	AssetType   string  `json:"assetType"`   // raw eToro Asset type ("Stocks","ETF","Crypto","CFD")
	Underlying  string  `json:"underlying"`  // routing target: "stock" | "crypto" (SC-17 R1)
	Wrapper     string  `json:"wrapper"`     // "cfd" | "cash" (display flag only, SC-17 R1)
	Units       float64 `json:"units"`       // summed across lots
	InvestedUSD float64 `json:"investedUsd"` // summed amount, USD
	AvgPriceUSD float64 `json:"avgPriceUsd"` // invested / units (USD per unit)
	Lots        int     `json:"lots"`        // number of open lots aggregated
	ISIN        string  `json:"isin"`        // "" if not seedable on this statement
}

// HoldingsResult is the reconstruction output for one upload.
type HoldingsResult struct {
	FileName string    `json:"fileName"`
	Holdings []Holding `json:"holdings"`
	Warnings []string  `json:"warnings"`
}

// cryptoTickers are the underlyings that route to the Crypto tab even when the
// eToro Asset type is "CFD" (SC-17 R1: BTC/ETH/SOL CFDs are crypto). Everything
// else — including GLD/SLV/XAU metal CFDs — routes to Stocks (asset-hedge).
var cryptoTickers = map[string]bool{
	"BTC": true, "ETH": true, "SOL": true, "XRP": true, "ADA": true,
	"AVAX": true, "DOT": true, "LINK": true, "MATIC": true, "POL": true,
	"DOGE": true, "LTC": true, "BCH": true, "BNB": true, "ARB": true,
	"OP": true, "SUI": true, "HBAR": true, "ATOM": true, "UNI": true,
	"AAVE": true, "TRX": true, "TON": true, "NEAR": true, "APT": true,
	"FIL": true, "ICP": true, "ETC": true, "XLM": true, "ALGO": true,
	"SHIB": true, "PEPE": true, "INJ": true, "RNDR": true, "IMX": true,
	"MKR": true, "GRT": true, "SAND": true, "MANA": true, "AXS": true,
	"EOS": true, "XTZ": true, "CRV": true, "COMP": true, "SNX": true,
	"DASH": true, "ZEC": true, "NEO": true, "MIOTA": true, "XMR": true,
}

var tickerParen = regexp.MustCompile(`\(([^()]+)\)\s*$`)

// ParseHoldings reconstructs current open holdings from an eToro statement.
func ParseHoldings(r io.Reader, fileName string) (*HoldingsResult, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	have := map[string]bool{}
	for _, s := range f.GetSheetList() {
		have[s] = true
	}
	for _, req := range []string{sheetAccountActivity, sheetClosedPos} {
		if !have[req] {
			return nil, fmt.Errorf("not an eToro statement: missing %q sheet", req)
		}
	}

	res := &HoldingsResult{FileName: fileName, Warnings: []string{}}

	// --- Closed Positions: closed-id set + ticker→ISIN map. ---
	cp, err := f.GetRows(sheetClosedPos)
	if err != nil {
		return nil, fmt.Errorf("read Closed Positions: %w", err)
	}
	closedIDs := map[string]bool{}
	tickerISIN := map[string]string{}
	tickerName := map[string]string{}
	if len(cp) >= 2 {
		ci := headerIndex(cp[0])
		iPID, iAction, iISIN := ci["Position ID"], ci["Action"], ci["ISIN"]
		for _, row := range cp[1:] {
			if pid := cell(row, iPID); pid != "" {
				closedIDs[pid] = true
			}
			act := cell(row, iAction)
			m := tickerParen.FindStringSubmatch(act)
			if m == nil {
				continue
			}
			tk := normTicker(m[1])
			// Company name = the Action text minus the trailing "(TICKER)".
			if name := strings.TrimSpace(tickerParen.ReplaceAllString(act, "")); name != "" {
				if _, seen := tickerName[tk]; !seen {
					tickerName[tk] = name
				}
			}
			if isin := cell(row, iISIN); isin != "" && isin != "-" {
				if _, seen := tickerISIN[tk]; !seen {
					tickerISIN[tk] = isin
				}
			}
		}
	}

	// --- Account Activity: still-open positions, aggregated by instrument. ---
	aa, err := f.GetRows(sheetAccountActivity)
	if err != nil {
		return nil, fmt.Errorf("read Account Activity: %w", err)
	}
	if len(aa) < 2 {
		return res, nil
	}
	ai := headerIndex(aa[0])
	iType, iDetails := ai["Type"], ai["Details"]
	iUnits, iAmount := ai["Units / Contracts"], ai["Amount"]
	iPID, iAsset := ai["Position ID"], ai["Asset type"]

	type agg struct {
		ticker, currency, assetType string
		units, invested            float64
		lots                       int
	}
	byInstrument := map[string]*agg{}
	skippedNoTicker := 0
	for _, row := range aa[1:] {
		if cell(row, iType) != "Open Position" {
			continue
		}
		pid := cell(row, iPID)
		if pid == "" || closedIDs[pid] {
			continue // closed before statement end (or unkeyed) → not currently open
		}
		ticker, currency := splitDetails(cell(row, iDetails))
		if ticker == "" {
			skippedNoTicker++
			continue
		}
		key := ticker
		a := byInstrument[key]
		if a == nil {
			a = &agg{ticker: ticker, currency: currency, assetType: cell(row, iAsset)}
			byInstrument[key] = a
		}
		a.units += parseNum(cell(row, iUnits))
		a.invested += parseNum(cell(row, iAmount))
		a.lots++
	}
	if skippedNoTicker > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"%d open positions had an unparseable instrument and were skipped", skippedNoTicker))
	}

	for _, a := range byInstrument {
		name := tickerName[a.ticker]
		if name == "" {
			name = a.ticker
		}
		h := Holding{
			Name:        name,
			Ticker:      a.ticker,
			Currency:    a.currency,
			AssetType:   a.assetType,
			Wrapper:     wrapperOf(a.assetType),
			Underlying:  underlyingOf(a.ticker, a.assetType),
			Units:       round6(a.units),
			InvestedUSD: round2(a.invested),
			Lots:        a.lots,
			ISIN:        tickerISIN[a.ticker],
		}
		if a.units > 0 {
			h.AvgPriceUSD = round4(a.invested / a.units)
		}
		res.Holdings = append(res.Holdings, h)
	}

	// Largest first — mirrors the Stocks tab's invested-desc ordering.
	sort.Slice(res.Holdings, func(i, j int) bool {
		if res.Holdings[i].InvestedUSD != res.Holdings[j].InvestedUSD {
			return res.Holdings[i].InvestedUSD > res.Holdings[j].InvestedUSD
		}
		return res.Holdings[i].Ticker < res.Holdings[j].Ticker
	})
	return res, nil
}

// splitDetails parses an Account Activity "TICKER/CURRENCY" detail into a
// normalized ticker and currency hint. Splits on the LAST slash so dotted
// suffixes survive (e.g. "RHM.DE/EUR" → "RHM.DE","EUR").
func splitDetails(d string) (ticker, currency string) {
	d = strings.TrimSpace(d)
	if d == "" {
		return "", ""
	}
	if i := strings.LastIndex(d, "/"); i >= 0 {
		return normTicker(d[:i]), strings.ToUpper(strings.TrimSpace(d[i+1:]))
	}
	return normTicker(d), ""
}

// NormalizeTicker is the exported form of normTicker, used by the reconciler to
// match eToro instruments against FT holdings on a common normalized key.
func NormalizeTicker(t string) string { return normTicker(t) }

// normTicker upper-cases an eToro ticker and its exchange suffix so casing
// variants collapse (SC-17 R3): "bayn.de" → "BAYN.DE", "iag.l" → "IAG.L".
func normTicker(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return ""
	}
	if i := strings.LastIndex(t, "."); i >= 0 {
		return strings.ToUpper(t[:i]) + "." + strings.ToUpper(t[i+1:])
	}
	return strings.ToUpper(t)
}

// underlyingOf routes by the instrument's underlying, not its wrapper (R1).
func underlyingOf(ticker, assetType string) string {
	if strings.EqualFold(strings.TrimSpace(assetType), "Crypto") {
		return "crypto"
	}
	base := ticker
	if i := strings.Index(base, "."); i >= 0 {
		base = base[:i]
	}
	if cryptoTickers[strings.ToUpper(base)] {
		return "crypto"
	}
	return "stock"
}

func wrapperOf(assetType string) string {
	if strings.EqualFold(strings.TrimSpace(assetType), "CFD") {
		return "cfd"
	}
	return "cash"
}

func round4(f float64) float64 { return float64(int64(f*1e4+sign(f)*0.5)) / 1e4 }
func round6(f float64) float64 { return float64(int64(f*1e6+sign(f)*0.5)) / 1e6 }
