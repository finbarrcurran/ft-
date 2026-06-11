// Package nexus implements SC-36 (AI Nexus tab) — ingestion of the Visser
// "AI Macro Nexus" weekly sheets (Technical / Exhaustion / Fundamentals) plus
// the universe seed. The compute engines + UI land in later phases.
package nexus

import (
	"bytes"
	"encoding/json"
	"fmt"
	"ft/internal/domain"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Kind constants for the three sheet types.
const (
	KindTechnical    = "technical"
	KindExhaustion   = "exhaustion"
	KindFundamentals = "fundamentals"
)

// ParsedFile is the result of parsing one uploaded xlsx.
type ParsedFile struct {
	Kind         string
	AsOf         string // YYYY-MM-DD
	Technical    []domain.NexusTechnical
	Exhaustion   []domain.NexusExhaustion
	Fundamentals []domain.NexusFundamentals
}

// Rows returns the row count for whichever kind was parsed.
func (p *ParsedFile) Rows() int {
	switch p.Kind {
	case KindTechnical:
		return len(p.Technical)
	case KindExhaustion:
		return len(p.Exhaustion)
	case KindFundamentals:
		return len(p.Fundamentals)
	}
	return 0
}

// Parse auto-detects the sheet type and parses it. asOfHint supplies the as_of
// for Technical sheets (which — unlike Exhaustion/Fundamentals — carry no date
// anywhere inside the file); it is ignored for the self-dating kinds.
func Parse(data []byte, asOfHint string) (*ParsedFile, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	have := map[string]bool{}
	for _, s := range f.GetSheetList() {
		have[s] = true
	}
	switch {
	case have["Signal Sheet"]:
		return parseTechnical(f, asOfHint)
	case have["Exhaustion Scores"]:
		return parseExhaustion(f)
	case have["Fundamentals"]:
		return parseFundamentals(f)
	}
	return nil, fmt.Errorf("unrecognised nexus file — no Signal Sheet / Exhaustion Scores / Fundamentals sheet")
}

// headerIndex finds the header row (first row containing a cell == "Ticker")
// and returns header→column-index plus the data rows that follow it.
func headerIndex(rows [][]string) (map[string]int, [][]string, error) {
	for i, r := range rows {
		for _, c := range r {
			if strings.TrimSpace(c) == "Ticker" {
				idx := map[string]int{}
				for j, h := range r {
					idx[strings.TrimSpace(h)] = j
				}
				return idx, rows[i+1:], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("header row (with a 'Ticker' column) not found")
}

func cell(r []string, idx map[string]int, name string) string {
	j, ok := idx[name]
	if !ok || j >= len(r) {
		return ""
	}
	return strings.TrimSpace(r[j])
}

func rowMapJSON(r []string, idx map[string]int) string {
	m := make(map[string]string, len(idx))
	for h, j := range idx {
		if h == "" {
			continue
		}
		if j < len(r) {
			m[h] = strings.TrimSpace(r[j])
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// --- value parsing ---------------------------------------------------------

func pf(s string) *float64 {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("$", "", ",", "", "%", "", "x", "", "X", "").Replace(s)
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "—" || s == "-" || strings.EqualFold(s, "n/a") || strings.EqualFold(s, "na") {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func pi(s string) *int {
	f := pf(s)
	if f == nil {
		return nil
	}
	v := int(*f)
	return &v
}

func ps(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "—" {
		return nil
	}
	return &s
}

var dateLayouts = []string{
	"2006-01-02", "2006-01-02 00:00:00", "2006/01/02",
	"1/2/2006", "01/02/2006", "1/2/06", "01/02/06",
	"January 2, 2006", "Jan 2, 2006", "2 January 2006", "January 02, 2006",
}

// normDate normalises a date string (or an Excel serial) to YYYY-MM-DD.
func normDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	// Excel serial day number (days since 1899-12-30).
	if n, err := strconv.ParseFloat(s, 64); err == nil && n > 20000 && n < 80000 {
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		return base.AddDate(0, 0, int(n)).Format("2006-01-02"), true
	}
	return "", false
}

// dateFromTitle pulls "Month DD, YYYY" out of a title cell like
// "AI Macro Nexus Fundamentals - June 05, 2026".
func dateFromTitle(title string) (string, bool) {
	if i := strings.LastIndex(title, "-"); i >= 0 {
		if d, ok := normDate(strings.TrimSpace(title[i+1:])); ok {
			return d, true
		}
	}
	return normDate(title)
}

func normBand(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	w := strings.Title(strings.ToLower(strings.Fields(s)[0]))
	return &w
}

// --- per-kind parsers ------------------------------------------------------

func parseTechnical(f *excelize.File, asOfHint string) (*ParsedFile, error) {
	asOf, ok := normDate(asOfHint)
	if !ok {
		return nil, fmt.Errorf("technical sheet carries no internal date — an as_of is required (got %q)", asOfHint)
	}
	rows, err := f.GetRows("Signal Sheet")
	if err != nil {
		return nil, err
	}
	idx, data, err := headerIndex(rows)
	if err != nil {
		return nil, err
	}
	out := &ParsedFile{Kind: KindTechnical, AsOf: asOf}
	for _, r := range data {
		tk := cell(r, idx, "Ticker")
		if tk == "" {
			continue
		}
		out.Technical = append(out.Technical, domain.NexusTechnical{
			Ticker: tk, AsOf: asOf, Source: "upload",
			Price:      pf(cell(r, idx, "Price")),
			TrendScore: pi(cell(r, idx, "Score")),
			SetupLabel: ps(cell(r, idx, "Setup")),
			RSI14:      pf(cell(r, idx, "RSI")),
			Ret1W:      pf(cell(r, idx, "1W %")),
			Ret1M:      pf(cell(r, idx, "1M %")),
			Ret3M:      pf(cell(r, idx, "3M %")),
			Vs20D:      pf(cell(r, idx, "vs 20D")),
			Vs50D:      pf(cell(r, idx, "vs 50D")),
			Vs200D:     pf(cell(r, idx, "vs 200D")),
			Slope50D:   pf(cell(r, idx, "50D Slope")),
			Slope200D:  pf(cell(r, idx, "200D Slope")),
			Dist52WHi:  pf(cell(r, idx, "Dist 52W Hi")),
			ATRPct:     pf(cell(r, idx, "ATR %")),
			VolRatio:   pf(cell(r, idx, "Vol Ratio")),
			RSSpy:      pf(cell(r, idx, "RS SPY")),
			RSQqq:      pf(cell(r, idx, "RS QQQ")),
			RSRank:     pi(cell(r, idx, "RS Rank")),
			MondayNote: ps(cell(r, idx, "Monday Note")),
			Components: rowMapJSON(r, idx),
		})
	}
	if len(out.Technical) == 0 {
		return nil, fmt.Errorf("technical: no data rows parsed")
	}
	return out, nil
}

func parseExhaustion(f *excelize.File) (*ParsedFile, error) {
	rows, err := f.GetRows("Exhaustion Scores")
	if err != nil {
		return nil, err
	}
	idx, data, err := headerIndex(rows)
	if err != nil {
		return nil, err
	}
	out := &ParsedFile{Kind: KindExhaustion}
	for _, r := range data {
		tk := cell(r, idx, "Ticker")
		if tk == "" {
			continue
		}
		if out.AsOf == "" {
			if d, ok := normDate(cell(r, idx, "As Of")); ok {
				out.AsOf = d
			}
		}
		out.Exhaustion = append(out.Exhaustion, domain.NexusExhaustion{
			Ticker: tk, Source: "upload",
			Price:        pf(cell(r, idx, "Price")),
			ExhScore:     pf(cell(r, idx, "Exh Score")),
			Band:         normBand(cell(r, idx, "Band")),
			RSI14:        pf(cell(r, idx, "RSI14")),
			RSI5:         pf(cell(r, idx, "RSI5")),
			WilliamsR:    pf(cell(r, idx, "Will %R")),
			Pos20D:       pf(cell(r, idx, "20D Pos")),
			Ext20DATR:    pf(cell(r, idx, "20D Ext ATR")),
			Ext50DATR:    pf(cell(r, idx, "50D Ext ATR")),
			RetVol1M:     pf(cell(r, idx, "1M Ret/Vol")),
			Imp5DATR:     pf(cell(r, idx, "5D Imp ATR")),
			VolRatio:     pf(cell(r, idx, "Vol Ratio")),
			ATRExpansion: pf(cell(r, idx, "ATR Exp")),
			TDSetup:      pi(cell(r, idx, "TD Setup")),
			TDCountdown:  pi(cell(r, idx, "TD Cntdn")),
			TDScore:      pf(cell(r, idx, "TD Score")),
			ATRPct:       pf(cell(r, idx, "ATR %")),
			Ret1M:        pf(cell(r, idx, "1M %")),
			Ret5D:        pf(cell(r, idx, "5D %")),
			DataWtPct:    pf(cell(r, idx, "Data Wt %")),
			Components:   rowMapJSON(r, idx),
		})
	}
	if out.AsOf == "" {
		return nil, fmt.Errorf("exhaustion: no parseable 'As Of' date")
	}
	for i := range out.Exhaustion {
		out.Exhaustion[i].AsOf = out.AsOf
	}
	if len(out.Exhaustion) == 0 {
		return nil, fmt.Errorf("exhaustion: no data rows parsed")
	}
	return out, nil
}

func parseFundamentals(f *excelize.File) (*ParsedFile, error) {
	title, _ := f.GetCellValue("Fundamentals", "A1")
	asOf, ok := dateFromTitle(title)
	if !ok {
		return nil, fmt.Errorf("fundamentals: could not parse as_of from title cell")
	}
	rows, err := f.GetRows("Fundamentals")
	if err != nil {
		return nil, err
	}
	idx, data, err := headerIndex(rows)
	if err != nil {
		return nil, err
	}
	out := &ParsedFile{Kind: KindFundamentals, AsOf: asOf}
	for _, r := range data {
		tk := cell(r, idx, "Ticker")
		if tk == "" {
			continue
		}
		cfy, _ := normDate(cell(r, idx, "Current FY"))
		nfy, _ := normDate(cell(r, idx, "Next FY"))
		out.Fundamentals = append(out.Fundamentals, domain.NexusFundamentals{
			Ticker: tk, AsOf: asOf, Source: "upload",
			MarketCap:       pf(cell(r, idx, "Market Cap")),
			FwdPE:           pf(cell(r, idx, "Forward P/E")),
			NextFYEPSGrowth: pf(cell(r, idx, "Next-Year EPS Growth")),
			FwdPEG:          pf(cell(r, idx, "Forward PEG")),
			Price:           pf(cell(r, idx, "Price")),
			CurrentFYEPS:    pf(cell(r, idx, "Current FY EPS")),
			NextFYEPS:       pf(cell(r, idx, "Next FY EPS")),
			CurrentFYEnd:    ps(cfy),
			NextFYEnd:       ps(nfy),
			DataStatus:      ps(cell(r, idx, "Data Status")),
		})
	}
	if len(out.Fundamentals) == 0 {
		return nil, fmt.Errorf("fundamentals: no data rows parsed")
	}
	return out, nil
}
