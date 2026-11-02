package videoitems

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The gate (SC-53 B): every ticker, number and year in an item must appear in the passage attached
// to it (15 s before the cited start to 45 s after the cited end, at most 200 words). A value that is
// missing is classed by where the transcript does have it:
//
//	grounded      every value is in the passage
//	wrong_place   a value is in the transcript within 120 s of the passage (cited at the wrong place)
//	far           a value is in the transcript but more than 120 s away
//	invented      a value is nowhere in the transcript (or the item cites no usable span)
//	name_mapping  a company name does not map to the ticker given for it
//
// Failing items are stored with their flag and shown; only invented and name_mapping items stay out
// of any signal (Fin 2026-11-01). Numbers that only a chart can show are "from chart, unverified".
const (
	ClassGrounded    = "grounded"
	ClassWrongPlace  = "wrong_place"
	ClassFar         = "far"
	ClassInvented    = "invented"
	ClassNameMapping = "name_mapping"

	nearSec = 120
)

// Issue is one failed check.
type Issue struct {
	Type   string `json:"type"`            // year, number, ticker, name, name_map, no_reference
	Detail string `json:"detail"`          // the value, with a hint
	Scope  string `json:"scope,omitempty"` // invented, near, far
}

// Verdict is the gate's result for one item.
type Verdict struct {
	Class           string   `json:"class"`
	Issues          []Issue  `json:"issues,omitempty"`
	ChartUnverified []string `json:"chartUnverified,omitempty"`
	SignalOK        bool     `json:"signalOk"`
}

var stopTickers = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields(`AI US USD GDP CPI PCE FOMC ETF ETFS IPO CEO CFO ATH YTD QE QT EPS PE PEG VIX NAV SEC FED UK EU GPU CPU SAAS LLM API
		OK TV PM AM DJIA PPI ISM NFP BLS IMF OPEC WTI DCF ROI ROE PMI HFT LP LPS OTC IV VS OR AND THE FOR TO IN ON OF AT BY AS IS IT IF NO SO UP II III
		HY OAS GFC MA EMA SMA RSI MACD TTM YOY QOQ MOM ROC LBO AUM CDS REIT REITS SPAC OEM ARR TAM CAPEX OPEX EBITDA GAAP IRS DOJ FTC ESG CTA CTAS PPO LTCM
		TBD NA ASAP FYI MIT NYSE DOW NASDAQ`) {
		m[s] = true
	}
	return m
}()

var (
	nameNumsRe   = regexp.MustCompile(`(?i)S&P ?500|SPX ?500|S&P500|\b(20|50|100|200)[- ]?(day|week|d|w)\b|\bnasdaq[- ]?100\b|\brussell[- ]?2000\b`)
	textTickerRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,4}(?:\.[A-Z]{1,2})?\b`)
	yearRe       = regexp.MustCompile(`^(19|20)\d\d$`)
	symbolPart   = regexp.MustCompile(`^[A-Za-z0-9.]{1,6}$`)
	allDigitsRe  = regexp.MustCompile(`^\d+$`)
	tokenSplit   = regexp.MustCompile(`[^a-z0-9]+`)
)

// Gate checks items against one transcript.
type Gate struct {
	A        *Aliases
	words    []Word
	valTimes map[float64][]int
	symTimes map[string][]int
}

// NewGate indexes the transcript words once.
func NewGate(a *Aliases, words []Word) *Gate {
	if a == nil {
		a = Default
	}
	g := &Gate{A: a, words: words, valTimes: map[float64][]int{}, symTimes: map[string][]int{}}
	for _, w := range words {
		for _, v := range NumValues(w.W) {
			g.valTimes[v] = append(g.valTimes[v], w.T)
		}
		if tok := tokenSplit.ReplaceAllString(strings.ToLower(w.W), ""); tok != "" {
			g.symTimes[tok] = append(g.symTimes[tok], w.T)
		}
	}
	return g
}

func scopeOf(times []int, start, end int) string {
	if len(times) == 0 {
		return "invented"
	}
	best := 1 << 30
	for _, t := range times {
		d := 0
		if t < start {
			d = start - t
		} else if t > end {
			d = t - end
		}
		if d < best {
			best = d
		}
	}
	if best <= nearSec {
		return "near"
	}
	return "far"
}

func windowTokens(win string) string {
	return " " + strings.Join(strings.Fields(tokenSplit.ReplaceAllString(strings.ToLower(win), " ")), " ") + " "
}

// fieldTickers reads the ticker field before any parenthesis and splits it on "/", "," and ";".
func fieldTickers(it Item) (tickers []string, claimed string) {
	f, _ := it.Fields["ticker"].(string)
	if strings.TrimSpace(f) == "" {
		return nil, ""
	}
	head := f
	if i := strings.Index(f, "("); i >= 0 {
		head, claimed = f[:i], strings.TrimRight(f[i+1:], ") ")
	}
	for _, part := range regexp.MustCompile(`[/,;]| and | or `).Split(head, -1) {
		part = strings.TrimSpace(part)
		if symbolPart.MatchString(part) {
			tickers = append(tickers, strings.ToUpper(part))
		} else if part != "" {
			claimed = strings.TrimSpace(claimed + " " + part)
		}
	}
	return tickers, claimed
}

// Check runs the gate for one item.
func (g *Gate) Check(it Item) Verdict {
	if it.Ref == nil {
		return Verdict{Class: ClassInvented, Issues: []Issue{{Type: "no_reference", Detail: "no usable cited span", Scope: "invented"}}}
	}
	start, end := it.Ref.Start, it.Ref.End
	if end-start > 45 {
		end = start + 45
	}
	win := Window(g.words, start, end)
	wtok := windowTokens(win)
	wvals := NumValues(win)
	var wYears []int
	for _, v := range wvals {
		if v >= 1800 && v <= 2100 && v == float64(int(v)) {
			wYears = append(wYears, int(v))
		}
	}
	scan := it
	if it.Block == "tickers_named" && nameSpoken(it, win) { // the ticker is the lookup of a spoken name: its letters are not searched for
		scan = Item{Block: it.Block, Ref: it.Ref, Fields: map[string]any{}}
		for k, v := range it.Fields {
			if k != "ticker" {
				scan.Fields[k] = v
			}
		}
	}
	txt := nameNumsRe.ReplaceAllString(ItemText(scan), " ")
	fieldTk, claimed := fieldTickers(it)
	tickers := map[string]bool{}
	for _, t := range fieldTk {
		// in tickers_named the ticker is the model's lookup of the spoken name, not spoken itself: the name is
		// checked instead, and the ticker only when the alias file knows it (a mismatch is then a real flag)
		if it.Block != "tickers_named" || (g.A.Has(t) && !nameSpoken(it, win)) {
			tickers[t] = true
		}
	}
	for _, t := range textTickerRe.FindAllString(txt, -1) {
		if !stopTickers[t] && !allDigitsRe.MatchString(t) {
			tickers[t] = true
		}
	}
	// numbers only a chart can show (from_chart items, in the reading)
	chartNums := map[string]bool{}
	if fc, _ := it.Fields["from_chart"].(bool); fc {
		if reading, ok := it.Fields["reading"].(string); ok {
			other := []string{}
			for k, v := range it.Fields {
				if s, ok := v.(string); ok && k != "reading" {
					other = append(other, s)
				}
			}
			otherSet := map[string]bool{}
			for _, n := range ItemNumbers(strings.Join(other, " ")) {
				otherSet[n] = true
			}
			for _, n := range ItemNumbers(reading) {
				if !otherSet[n] {
					chartNums[n] = true
				}
			}
		}
	}
	var issues []Issue
	var chartUnverified []string
	for _, m := range ItemNumbers(txt) {
		part := false
		for t := range tickers {
			if strings.Contains(t, m) || strings.HasPrefix(t, m) {
				part = true
			}
		}
		if part {
			continue
		}
		vals := NumValues(m)
		ok := false
		for _, v := range vals {
			if hasValue(wvals, v) {
				ok = true
			}
		}
		isYear := yearRe.MatchString(m)
		if !ok && isYear { // "73" in the passage matches 1973 when a full year within a century is there too
			y := atoi(m)
			if hasValue(wvals, float64(y%100)) {
				for _, w := range wYears {
					if abs(w-y) <= 99 {
						ok = true
					}
				}
			}
		}
		if ok {
			continue
		}
		if chartNums[m] && !isYear {
			chartUnverified = append(chartUnverified, m)
			continue
		}
		var times []int
		for _, v := range vals {
			times = append(times, g.valTimes[v]...)
		}
		t := "number"
		if isYear {
			t = "year"
		}
		issues = append(issues, Issue{Type: t, Detail: m, Scope: scopeOf(times, start, end)})
	}
	hits := g.A.Hits(win)
	var tks []string
	for t := range tickers {
		tks = append(tks, t)
	}
	sort.Strings(tks)
	for _, tk := range tks {
		base := tokenSplit.ReplaceAllString(strings.ToLower(strings.SplitN(tk, ".", 2)[0]), "")
		if strings.Contains(wtok, " "+base+" ") || hits[tk] {
			continue
		}
		times := append([]int(nil), g.symTimes[base]...)
		for _, n := range g.A.Names(tk) {
			if f := strings.Fields(n); len(f) == 1 {
				times = append(times, g.symTimes[tokenSplit.ReplaceAllString(f[0], "")]...)
			}
		}
		detail := tk
		if len(times) == 0 && len(hits) > 0 {
			var hs []string
			for h := range hits {
				hs = append(hs, h)
			}
			sort.Strings(hs)
			if len(hs) > 3 {
				hs = hs[:3]
			}
			detail += " (passage names " + strings.Join(hs, ", ") + ")"
		}
		issues = append(issues, Issue{Type: "ticker", Detail: detail, Scope: scopeOf(times, start, end)})
	}
	// the name as spoken must be in the passage, and must map to the ticker given for it
	if name, _ := it.Fields["name"].(string); it.Block == "tickers_named" && strings.TrimSpace(name) != "" {
		nn := NormName(name)
		if nn != "" && !strings.Contains(" "+NormName(win)+" ", " "+nn+" ") && !strings.Contains(" "+NormName(win)+" ", " "+nn+"s ") &&
			!fuzzyIn(nn, NormName(win)) && !compactIn(nn, NormName(win)) {
			var times []int
			for _, tok := range strings.Fields(nn) {
				if len(tok) >= 4 {
					tok = tokenSplit.ReplaceAllString(tok, "")
					times = append(times, g.symTimes[tok]...)
					if len(times) == 0 && len(tok) >= 6 { // ASR spelling ("lummentum")
						for k, ts := range g.symTimes {
							if len(k) >= 6 && abs(len(k)-len(tok)) <= 2 && editDistance(tok, k, 2) <= 2 {
								times = append(times, ts...)
							}
						}
					}
					break
				}
			}
			issues = append(issues, Issue{Type: "name", Detail: name, Scope: scopeOf(times, start, end)})
		}
		if len(fieldTk) > 0 {
			want := g.A.Tickers(name)
			if len(want) == 0 {
				if t, _, _, ok := g.A.Fuzzy(name, 2); ok {
					want = t
				}
			}
			if len(want) > 0 {
				for _, tk := range fieldTk {
					found := false
					for _, w := range want {
						if w == tk {
							found = true
						}
					}
					if !found {
						issues = append(issues, Issue{Type: "name_map", Detail: fmt.Sprintf("%s given for %q, which maps to %s", tk, name, strings.Join(want, ", "))})
					}
				}
			}
		}
	}
	if claimed != "" { // the older "TICKER (Company)" shape
		if named := g.A.Hits(claimed); len(named) > 0 {
			for _, tk := range fieldTk {
				if !named[tk] {
					issues = append(issues, Issue{Type: "name_map", Detail: fmt.Sprintf("%s given for %q", tk, claimed)})
				}
			}
		}
	}
	return finish(issues, chartUnverified)
}

func finish(issues []Issue, chart []string) Verdict {
	v := Verdict{Issues: issues, ChartUnverified: chart, Class: ClassGrounded}
	rank := 0
	for _, is := range issues {
		r := 0
		switch {
		case is.Type == "name_map":
			r = 4
		case is.Scope == "invented":
			r = 3
		case is.Scope == "far":
			r = 2
		case is.Scope == "near":
			r = 1
		}
		if r > rank {
			rank = r
		}
	}
	switch rank {
	case 4:
		v.Class = ClassNameMapping
	case 3:
		v.Class = ClassInvented
	case 2:
		v.Class = ClassFar
	case 1:
		v.Class = ClassWrongPlace
	}
	v.SignalOK = v.Class != ClassInvented && v.Class != ClassNameMapping
	return v
}

// fuzzyIn reports whether a single-word name of six or more letters has a word within edit distance 2
// in the text (ASR misspellings such as "lummentum" for "lumentum").
func fuzzyIn(name, text string) bool {
	if strings.Contains(name, " ") || len(name) < 6 {
		return false
	}
	for _, w := range strings.Fields(text) {
		if len(w) >= 6 && abs(len(w)-len(name)) <= 2 && editDistance(name, w, 2) <= 2 {
			return true
		}
	}
	return false
}

// compactIn matches a name with the spaces removed on both sides, and a spoken word that begins with
// the name ("robinhood" for "robin hood", "open router" for "openrouter", "cityroup" for "citi").
func compactIn(name, text string) bool {
	n := strings.ReplaceAll(name, " ", "")
	if len(n) < 4 {
		return false
	}
	t := strings.ReplaceAll(text, " ", "")
	if strings.Contains(t, n) {
		return len(n) >= 6 || strings.Contains(" "+text+" ", " "+name)
	}
	return false
}

// nameSpoken reports whether a tickers_named item's company name is in the passage (the ticker is then
// the model's lookup of a spoken name, not something to find in the passage).
func nameSpoken(it Item, win string) bool {
	name, _ := it.Fields["name"].(string)
	nn, w := NormName(name), NormName(win)
	if nn == "" {
		return false
	}
	return strings.Contains(" "+w+" ", " "+nn+" ") || strings.Contains(" "+w+" ", " "+nn+"s ") || fuzzyIn(nn, w) || compactIn(nn, w)
}
