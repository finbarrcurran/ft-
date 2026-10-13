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
// A statement may span several calendar years (SC-50): each closed position and dividend is
// attributed to the year of its OWN date; the Financial Summary's whole-period lines (fees,
// interest, refunds, other income) are shown in the End Date year as "Income & other".
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
	// SC-50: "Income & other" lines (whole-period, shown in the End Date year). They count in
	// account-level total return, never in per-trade statistics.
	RefundsUSD       float64 `json:"refundsUsd"`
	RefundsEUR       float64 `json:"refundsEur"`
	OtherIncomeUSD   float64 `json:"otherIncomeUsd"` // airdrops, staking, corporate actions, stocks lending, index adjustments
	OtherIncomeEUR   float64 `json:"otherIncomeEur"`
	UnclassifiedUSD  float64 `json:"unclassifiedUsd"`
	UnclassifiedEUR  float64 `json:"unclassifiedEur"`
	ReconNetDeltaUSD float64 `json:"reconNetDeltaUsd"`
	ReconNetDeltaEUR float64 `json:"reconNetDeltaEur"`
	NetUSD         float64     `json:"netUsd"`
	NetEUR         float64     `json:"netEur"`
	ComputedPnLUSD float64     `json:"computedPnlUsd"`
	ComputedPnLEUR float64     `json:"computedPnlEur"`
	ReconDeltaUSD  float64     `json:"reconDeltaUsd"`
	ReconDeltaEUR  float64     `json:"reconDeltaEur"`
	Assets         []AssetPerf `json:"assets"`
}

// ClosedPosition is one closed position, keyed by eToro's Position ID (SC-50). The key is what
// makes re-imports and overlapping statements idempotent.
type ClosedPosition struct {
	PositionID string  `json:"positionId"`
	CloseDate  string  `json:"closeDate"` // YYYY-MM-DD
	Year       int     `json:"year"`      // year of its OWN close date
	AssetType  string  `json:"assetType"`
	IsCopy     bool    `json:"isCopy"`
	ProfitUSD  float64 `json:"profitUsd"`
	ProfitEUR  float64 `json:"profitEur"`
	OpenDate   string  `json:"openDate"`  // YYYY-MM-DD, "" when unreadable (SC-51)
	AmountUSD  float64 `json:"amountUsd"` // invested amount, 0 when unreadable (SC-51)
}

// DividendRow is one Dividends-sheet row, deduped on Key.
type DividendRow struct {
	Key        string  `json:"key"`
	PositionID string  `json:"positionId"`
	PayDate    string  `json:"payDate"` // YYYY-MM-DD
	Year       int     `json:"year"`
	AssetType  string  `json:"assetType"`
	USD        float64 `json:"usd"`
	EUR        float64 `json:"eur"`
}

// PeriodLine is one Financial Summary line with its component. Component "" = UNCLASSIFIED.
type PeriodLine struct {
	Label     string  `json:"label"`
	USD       float64 `json:"usd"`
	EUR       float64 `json:"eur"`
	Component string  `json:"component"`
	AssetType string  `json:"assetType,omitempty"`
}

// Period is what the Financial Summary says about the WHOLE statement window: eToro reports
// these lines (fees, interest, refunds, ...) for the period, not per year or per position.
type Period struct {
	RangeStart string       `json:"rangeStart"`
	RangeEnd   string       `json:"rangeEnd"`
	Lines      []PeriodLine `json:"lines"`

	NetUSD, NetEUR                         float64 // sum of every Financial Summary line
	RealisedUSD, RealisedEUR               float64 // the "(Profit or Loss)" headline lines
	DividendsUSD, DividendsEUR             float64
	FeesUSD, FeesEUR                       float64
	InterestUSD, InterestEUR               float64
	RefundsUSD, RefundsEUR                 float64
	OtherIncomeUSD, OtherIncomeEUR         float64 // airdrops, staking, corporate actions, stocks lending, index adjustments
	UnclassifiedUSD, UnclassifiedEUR       float64
	ClosedSumUSD, ClosedSumEUR             float64 // sum over the statement's closed positions
	ReconPnLDeltaUSD, ReconPnLDeltaEUR     float64 // headline realised minus closed positions
	ReconNetDeltaUSD, ReconNetDeltaEUR     float64 // Net minus every classified component (= unclassified)
	DividendResidualUSD, DividendResidualEUR float64 // Financial Summary dividends not on the Dividends sheet (CFD dividends)
	FeesByType                             map[string][2]float64
}

// Statement is the parsed-and-computed result of one upload.
type Statement struct {
	FileName  string           `json:"fileName"`
	Years     []YearPerf       `json:"years"`
	Warnings  []string         `json:"warnings"`
	Period    Period           `json:"period"`
	Positions []ClosedPosition `json:"-"`
	Dividends []DividendRow    `json:"-"`
}

const (
	sheetAccountSummary = "Account Summary"
	sheetClosedPos      = "Closed Positions"
	sheetDividends      = "Dividends"
	sheetFinancialSum   = "Financial Summary"
)

// Financial Summary components. Anything non-zero that matches none of these is surfaced as
// "Unclassified: <label> $x" (never silently inside Net). extraComponents lets a new label be
// classified without touching the rules (and lets the tests prove the delta closes once a
// label is classified).
const (
	compRealised = "realised"
	compDividend = "dividends"
	compInterest = "interest"
	compRefunds  = "refunds"
	compOther    = "other_income"
	compFees     = "fees"
)

var extraComponents = map[string]string{
	"Income from Refunds":          compRefunds,
	"Income from Airdrops":         compOther,
	"Income from Staking":          compOther,
	"Income from Corporate Actions": compOther,
	"Stocks Lending":               compOther,
	"Index adjustments":            compOther,
}

func classifyFS(name string) (component, assetType string) {
	if c, ok := extraComponents[name]; ok {
		return c, ""
	}
	switch {
	case strings.Contains(name, "(Profit or Loss)") && !strings.Contains(name, "Dividend"):
		return compRealised, fsTypeFromPnL(name)
	case strings.Contains(name, "Dividend"):
		return compDividend, ""
	case strings.HasPrefix(name, "Total Interest"):
		return compInterest, ""
	case strings.HasPrefix(name, "Spread fee on"), name == "SDRT Charge", strings.HasPrefix(name, "Fees ("):
		return compFees, fsTypeFromFee(name)
	}
	return "", ""
}

// Parse reads an eToro statement and computes the performance history. `now` is used to flag
// YTD (partial) years. A multi-year statement produces one YearPerf per year (SC-50): each
// closed position and dividend lands in the year of its OWN date; the Financial Summary's
// whole-period lines (fees, interest, refunds, other income) go to the End Date year.
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
	endYear, _ := strconv.Atoi(rangeEnd[:4])
	startYear := endYear
	if len(rangeStart) >= 4 {
		startYear, _ = strconv.Atoi(rangeStart[:4])
	}

	// --- Closed Positions → per-position rows. ---
	cp, err := f.GetRows(sheetClosedPos)
	if err != nil {
		return nil, fmt.Errorf("read Closed Positions: %w", err)
	}
	outside, undated, noID := 0, 0, 0
	if len(cp) >= 2 {
		idx := headerIndex(cp[0])
		iID, iType, iCopied, iClose := idx["Position ID"], idx["Type"], idx["Copied From"], idx["Close Date"]
		iPU, iPE := idx["Profit(USD)"], idx["Profit(EUR)"]
		iOpen, iAmt := idx["Open Date"], idx["Amount"]
		_, hasOpen := idx["Open Date"]
		_, hasAmt := idx["Amount"]
		_, hasID := idx["Position ID"]
		for n, row := range cp[1:] {
			typ := normAssetType(cell(row, iType))
			if typ == "" {
				continue
			}
			cd := isoFromDDMMYYYY(cell(row, iClose))
			year := 0
			if cd != "" {
				year, _ = strconv.Atoi(cd[:4])
				if (rangeStart != "" && cd < rangeStart) || cd > rangeEnd {
					outside++
				}
			}
			if year == 0 {
				year = endYear
				undated++
			}
			openDate, amount := "", 0.0
			if hasOpen {
				openDate = isoFromDDMMYYYY(cell(row, iOpen))
			}
			if hasAmt {
				amount = parseNum(cell(row, iAmt))
			}
			id := ""
			if hasID {
				id = cell(row, iID)
			}
			if id == "" {
				noID++
				id = fmt.Sprintf("noid:%s:%s:%s:%d", cd, typ, cell(row, iPU), n)
			}
			st.Positions = append(st.Positions, ClosedPosition{
				PositionID: id, CloseDate: cd, Year: year, AssetType: typ,
				IsCopy: isCopy(cell(row, iCopied)), ProfitUSD: parseNum(cell(row, iPU)), ProfitEUR: parseNum(cell(row, iPE)),
				OpenDate: openDate, AmountUSD: amount,
			})
		}
	}
	if outside > 0 {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"%d closed positions have a close date outside this statement's own window (%s to %s); they are counted in the year of their close date", outside, rangeStart, rangeEnd))
	}
	if undated > 0 {
		st.Warnings = append(st.Warnings, fmt.Sprintf("%d closed positions had no readable close date and were attributed to %d", undated, endYear))
	}
	if noID > 0 {
		st.Warnings = append(st.Warnings, fmt.Sprintf("%d closed positions had no Position ID; they cannot be de-duplicated against another statement", noID))
	}

	// --- Dividends sheet → per-row, dated. ---
	if have[sheetDividends] {
		dv, err := f.GetRows(sheetDividends)
		if err == nil && len(dv) >= 2 {
			idx := headerIndex(dv[0])
			iType, iDate := idx["Type"], idx["Date of Payment"]
			iUSD, iEUR := idx["Net Dividend Received (USD)"], idx["Net Dividend Received (EUR)"]
			iID, iName := idx["Position ID"], idx["Instrument Name"]
			for n, row := range dv[1:] {
				typ := normAssetType(cell(row, iType))
				if typ == "" {
					continue
				}
				pd := isoFromDDMMYYYY(cell(row, iDate))
				year := endYear
				if pd != "" {
					year, _ = strconv.Atoi(pd[:4])
				}
				usd, eur := parseNum(cell(row, iUSD)), parseNum(cell(row, iEUR))
				key := fmt.Sprintf("%s|%s|%s|%.4f", cell(row, iID), pd, cell(row, iName), usd)
				if cell(row, iID) == "" && cell(row, iName) == "" {
					key = fmt.Sprintf("row:%d|%s|%.4f", n, pd, usd)
				}
				st.Dividends = append(st.Dividends, DividendRow{
					Key: key, PositionID: cell(row, iID), PayDate: pd, Year: year, AssetType: typ, USD: usd, EUR: eur,
				})
			}
		}
	}

	// --- Financial Summary → the whole-period lines. ---
	fs, err := f.GetRows(sheetFinancialSum)
	if err != nil {
		return nil, fmt.Errorf("read Financial Summary: %w", err)
	}
	p := &st.Period
	p.RangeStart, p.RangeEnd = rangeStart, rangeEnd
	p.FeesByType = map[string][2]float64{}
	for _, row := range fs {
		if len(row) < 3 {
			continue
		}
		name := strings.TrimSpace(row[0])
		usd, eur := parseNum(cell(row, 1)), parseNum(cell(row, 2))
		if name == "" || strings.EqualFold(name, "Name") {
			continue
		}
		comp, typ := classifyFS(name)
		p.Lines = append(p.Lines, PeriodLine{Label: name, USD: usd, EUR: eur, Component: comp, AssetType: typ})
		p.NetUSD += usd
		p.NetEUR += eur
		switch comp {
		case compRealised:
			p.RealisedUSD += usd
			p.RealisedEUR += eur
		case compDividend:
			p.DividendsUSD += usd
			p.DividendsEUR += eur
		case compInterest:
			p.InterestUSD += usd
			p.InterestEUR += eur
		case compRefunds:
			p.RefundsUSD += usd
			p.RefundsEUR += eur
		case compOther:
			p.OtherIncomeUSD += usd
			p.OtherIncomeEUR += eur
		case compFees:
			p.FeesUSD += usd
			p.FeesEUR += eur
			if typ != "" {
				v := p.FeesByType[typ]
				p.FeesByType[typ] = [2]float64{v[0] + usd, v[1] + eur}
			}
		default:
			// Unclassified: a non-zero line the rules do not know. It stays visible.
			p.UnclassifiedUSD += usd
			p.UnclassifiedEUR += eur
			if usd != 0 || eur != 0 {
				st.Warnings = append(st.Warnings, fmt.Sprintf("Unclassified: %s %s", name, signedMoney(usd, eur)))
			}
		}
	}
	for _, c := range st.Positions {
		p.ClosedSumUSD += c.ProfitUSD
		p.ClosedSumEUR += c.ProfitEUR
	}
	var divRowsUSD, divRowsEUR float64
	for _, d := range st.Dividends {
		divRowsUSD += d.USD
		divRowsEUR += d.EUR
	}
	p.DividendResidualUSD, p.DividendResidualEUR = round2(p.DividendsUSD-divRowsUSD), round2(p.DividendsEUR-divRowsEUR)
	p.ReconPnLDeltaUSD = round2(p.RealisedUSD - p.ClosedSumUSD)
	p.ReconPnLDeltaEUR = round2(p.RealisedEUR - p.ClosedSumEUR)
	classifiedUSD := p.RealisedUSD + p.DividendsUSD + p.FeesUSD + p.InterestUSD + p.RefundsUSD + p.OtherIncomeUSD
	classifiedEUR := p.RealisedEUR + p.DividendsEUR + p.FeesEUR + p.InterestEUR + p.RefundsEUR + p.OtherIncomeEUR
	p.ReconNetDeltaUSD = round2(p.NetUSD - classifiedUSD)
	p.ReconNetDeltaEUR = round2(p.NetEUR - classifiedEUR)

	// Two-currency realised-P&L reconciliation: warn per currency, naming the lines.
	if abs(p.ReconPnLDeltaUSD) > 1.0 || abs(p.ReconPnLDeltaEUR) > 1.0 {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"realised P&L does not reconcile: Financial Summary '(Profit or Loss)' lines $%.2f / €%.2f vs closed positions $%.2f / €%.2f (delta $%.2f USD, €%.2f EUR)",
			p.RealisedUSD, p.RealisedEUR, p.ClosedSumUSD, p.ClosedSumEUR, p.ReconPnLDeltaUSD, p.ReconPnLDeltaEUR))
	}

	// --- Years. ---
	yearSet := map[int]bool{endYear: true}
	for _, c := range st.Positions {
		yearSet[c.Year] = true
	}
	for _, d := range st.Dividends {
		yearSet[d.Year] = true
	}
	years := make([]int, 0, len(yearSet))
	for y := range yearSet {
		years = append(years, y)
	}
	sort.Ints(years)
	singleYear := len(years) == 1 && startYear == endYear

	if !singleYear {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"This statement spans %d–%d: closed positions and dividends are attributed to the year of their own date. The Financial Summary reports fees, interest, refunds and other income for the whole period (%s to %s) and eToro does not split them by year, so they are shown in %d under \"Income & other\".",
			years[0], years[len(years)-1], rangeStart, rangeEnd, endYear))
	}

	order := map[string]int{"Stocks": 0, "ETFs": 1, "Crypto": 2, "CFDs": 3}
	for _, y := range years {
		yp := YearPerf{Year: y}
		yp.RangeStart, yp.RangeEnd = rangeStart, rangeEnd
		if rangeStart < fmt.Sprintf("%d-01-01", y) {
			yp.RangeStart = fmt.Sprintf("%d-01-01", y)
		}
		if rangeEnd > fmt.Sprintf("%d-12-31", y) {
			yp.RangeEnd = fmt.Sprintf("%d-12-31", y)
		}
		yp.IsYTD = !strings.HasSuffix(yp.RangeEnd, "-12-31") || y == now.Year()

		assetAcc := map[string]*AssetPerf{}
		asset := func(t string) *AssetPerf {
			if assetAcc[t] == nil {
				assetAcc[t] = &AssetPerf{AssetType: t}
			}
			return assetAcc[t]
		}
		var cpUSD, cpEUR float64
		for _, c := range st.Positions {
			if c.Year != y {
				continue
			}
			a := asset(c.AssetType)
			a.TradeCount++
			a.RealisedPnLUSD += c.ProfitUSD
			a.RealisedPnLEUR += c.ProfitEUR
			cpUSD += c.ProfitUSD
			cpEUR += c.ProfitEUR
			if c.IsCopy {
				a.RealisedCopyUSD += c.ProfitUSD
				a.RealisedCopyEUR += c.ProfitEUR
			} else {
				a.RealisedDiscUSD += c.ProfitUSD
				a.RealisedDiscEUR += c.ProfitEUR
			}
		}
		for _, d := range st.Dividends {
			if d.Year != y {
				continue
			}
			a := asset(d.AssetType)
			a.DividendsUSD += d.USD
			a.DividendsEUR += d.EUR
			yp.DividendsUSD += d.USD
			yp.DividendsEUR += d.EUR
		}
		yp.ComputedPnLUSD, yp.ComputedPnLEUR = round2(cpUSD), round2(cpEUR)
		yp.RealisedPnLUSD, yp.RealisedPnLEUR = cpUSD, cpEUR

		if y == endYear {
			// Whole-period lines live in the End Date year ("Income & other").
			yp.FeesUSD, yp.FeesEUR = p.FeesUSD, p.FeesEUR
			yp.InterestUSD, yp.InterestEUR = p.InterestUSD, p.InterestEUR
			yp.RefundsUSD, yp.RefundsEUR = p.RefundsUSD, p.RefundsEUR
			yp.OtherIncomeUSD, yp.OtherIncomeEUR = p.OtherIncomeUSD, p.OtherIncomeEUR
			yp.UnclassifiedUSD, yp.UnclassifiedEUR = p.UnclassifiedUSD, p.UnclassifiedEUR
			yp.ReconDeltaUSD, yp.ReconDeltaEUR = p.ReconPnLDeltaUSD, p.ReconPnLDeltaEUR
			yp.ReconNetDeltaUSD, yp.ReconNetDeltaEUR = p.ReconNetDeltaUSD, p.ReconNetDeltaEUR
			for t, v := range p.FeesByType {
				a := asset(t)
				a.FeesUSD += v[0]
				a.FeesEUR += v[1]
			}
		}
		if singleYear {
			// Compatibility with single-year statements: the Financial Summary is authoritative.
			yp.RealisedPnLUSD, yp.RealisedPnLEUR = p.RealisedUSD, p.RealisedEUR
			yp.DividendsUSD, yp.DividendsEUR = p.DividendsUSD, p.DividendsEUR
			for _, l := range p.Lines {
				if l.Component == compRealised && l.AssetType != "" {
					a := asset(l.AssetType)
					a.RealisedPnLUSD, a.RealisedPnLEUR = l.USD, l.EUR
				}
			}
			yp.NetUSD, yp.NetEUR = p.NetUSD, p.NetEUR
		} else {
			if y == endYear {
				// Dividends the Dividends sheet does not list (CFD dividends are only in the Financial
				// Summary) go to the End Date year so the years add up to the statement.
				var rowsUSD, rowsEUR float64
				for _, d := range st.Dividends {
					rowsUSD += d.USD
					rowsEUR += d.EUR
				}
				if rUSD, rEUR := p.DividendsUSD-rowsUSD, p.DividendsEUR-rowsEUR; abs(rUSD) > 0.004 || abs(rEUR) > 0.004 {
					a := asset("CFDs")
					a.DividendsUSD += rUSD
					a.DividendsEUR += rEUR
					yp.DividendsUSD += rUSD
					yp.DividendsEUR += rEUR
				}
			}
			yp.NetUSD = cpUSD + yp.DividendsUSD
			yp.NetEUR = cpEUR + yp.DividendsEUR
			if y == endYear {
				yp.NetUSD += p.FeesUSD + p.InterestUSD + p.RefundsUSD + p.OtherIncomeUSD + p.UnclassifiedUSD
				yp.NetEUR += p.FeesEUR + p.InterestEUR + p.RefundsEUR + p.OtherIncomeEUR + p.UnclassifiedEUR
			}
		}
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
		st.Years = append(st.Years, yp)
	}
	p.round()
	return st, nil
}

// signedMoney formats "$1.23 / €1.10" for warnings.
func signedMoney(usd, eur float64) string {
	return fmt.Sprintf("$%.2f / €%.2f", usd, eur)
}

func (p *Period) round() {
	for _, f := range []*float64{&p.NetUSD, &p.NetEUR, &p.RealisedUSD, &p.RealisedEUR, &p.DividendsUSD, &p.DividendsEUR,
		&p.FeesUSD, &p.FeesEUR, &p.InterestUSD, &p.InterestEUR, &p.RefundsUSD, &p.RefundsEUR,
		&p.OtherIncomeUSD, &p.OtherIncomeEUR, &p.UnclassifiedUSD, &p.UnclassifiedEUR, &p.ClosedSumUSD, &p.ClosedSumEUR} {
		*f = round2(*f)
	}
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

// parseNum tolerates eToro's blanks ("-", "", "N/A"), thousands separators and — importantly —
// accounting-style negatives: the statement footnote says "Values shown in brackets indicate
// negative amounts", and numeric cells formatted that way come back from the spreadsheet reader
// as "(11.50)". Before 2026-10-13 those parsed as 0, so every loss in the Closed Positions
// sheet was silently dropped (found by testing against a real multi-year statement: 984 of
// 1,735 rows were negative).
func parseNum(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || strings.EqualFold(s, "N/A") {
		return 0
	}
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg = true
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	s = strings.ReplaceAll(s, "\u2212", "-") // unicode minus
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimPrefix(s, "$")
	s = strings.TrimPrefix(s, "€")
	s = strings.TrimSuffix(s, "%")
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	if neg {
		return -f
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
	y.RefundsUSD, y.RefundsEUR = round2(y.RefundsUSD), round2(y.RefundsEUR)
	y.OtherIncomeUSD, y.OtherIncomeEUR = round2(y.OtherIncomeUSD), round2(y.OtherIncomeEUR)
	y.UnclassifiedUSD, y.UnclassifiedEUR = round2(y.UnclassifiedUSD), round2(y.UnclassifiedEUR)
	y.ReconNetDeltaUSD, y.ReconNetDeltaEUR = round2(y.ReconNetDeltaUSD), round2(y.ReconNetDeltaEUR)
}
