// Package videoitems is SC-53: the v5 item extraction for a video digest (every claim, prediction,
// level and indicator the speaker states), the grounding gate that checks each item against the
// transcript, the ticker resolver, and the direction flag. Pure logic: no database, no network
// (the resolver takes a lookup function). The orchestration lives in internal/videodigest/items.go.
package videoitems

import (
	_ "embed"
	"regexp"
	"sort"
	"strings"
)

//go:embed ticker_aliases.tsv
var aliasTSV string

//go:embed name_kinds.tsv
var kindsTSV string

// NameKind is how a spoken name that is not a company is classed (SC-53 follow-up, v1.95.2).
type NameKind struct {
	Kind   string // proxy | not_listed
	Ticker string // proxy only; display only
}

var nameKinds = func() map[string]NameKind {
	m := map[string]NameKind{}
	for _, line := range strings.Split(kindsTSV, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		k := NameKind{Kind: strings.TrimSpace(f[1])}
		if len(f) > 2 {
			k.Ticker = strings.TrimSpace(f[2])
		}
		m[normSpoken(f[0])] = k
	}
	return m
}()

// KindOf looks a spoken name up in name_kinds.tsv ("total two" finds "total 2").
func KindOf(name string) (NameKind, bool) { k, ok := nameKinds[normSpoken(name)]; return k, ok }

// Aliases maps a ticker to the lower-case company names that ground it. One line per name in
// ticker_aliases.tsv (ticker<TAB>name<TAB>source); a new name is a one-line change.
type Aliases struct {
	byTicker map[string][]string
	byName   map[string][]string // normalised name -> tickers
	names    []string            // every normalised name, for fuzzy matching
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9& ]+`)

// NormName lower-cases, drops punctuation and collapses spaces.
func NormName(s string) string {
	s = nonAlnum.ReplaceAllString(strings.ToLower(s), " ")
	return strings.Join(strings.Fields(s), " ")
}

// ParseAliases reads the TSV form. Lines starting with # and blank lines are skipped.
func ParseAliases(tsv string) *Aliases {
	a := &Aliases{byTicker: map[string][]string{}, byName: map[string][]string{}}
	seen := map[string]bool{}
	for _, line := range strings.Split(tsv, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		tk := strings.ToUpper(strings.TrimSpace(f[0]))
		n := NormName(f[1])
		if tk == "" || n == "" || seen[tk+"\t"+n] {
			continue
		}
		seen[tk+"\t"+n] = true
		a.byTicker[tk] = append(a.byTicker[tk], n)
		a.byName[n] = append(a.byName[n], tk)
	}
	for n := range a.byName {
		a.names = append(a.names, n)
	}
	sort.Strings(a.names)
	return a
}

// Default is the embedded alias file.
var Default = ParseAliases(aliasTSV)

// Names returns the names known for a ticker.
func (a *Aliases) Names(ticker string) []string { return a.byTicker[strings.ToUpper(ticker)] }

// Has reports whether the ticker is in the file at all.
func (a *Aliases) Has(ticker string) bool { _, ok := a.byTicker[strings.ToUpper(ticker)]; return ok }

// Tickers returns the tickers a normalised name maps to.
func (a *Aliases) Tickers(name string) []string { return a.byName[NormName(name)] }

// Hits returns every ticker whose name appears (as whole words, optionally plural) in the text.
func (a *Aliases) Hits(text string) map[string]bool {
	low := " " + NormName(text) + " "
	hits := map[string]bool{}
	for _, n := range a.names {
		if strings.Contains(low, " "+n+" ") || strings.Contains(low, " "+n+"s ") {
			for _, tk := range a.byName[n] {
				hits[tk] = true
			}
		}
	}
	return hits
}

// Fuzzy returns the tickers of the single closest alias name within the edit distance (ASR
// misspellings such as "lummentum" for "lumentum"). Names shorter than 6 letters are never matched
// fuzzily (too many false matches). ok is false when nothing is close or two different tickers tie.
func (a *Aliases) Fuzzy(name string, maxDist int) (tickers []string, matched string, dist int, ok bool) {
	q := NormName(name)
	if len(q) < 6 {
		return nil, "", 0, false
	}
	best := maxDist + 1
	var bestNames []string
	for _, n := range a.names {
		if len(n) < 6 || abs(len(n)-len(q)) > maxDist {
			continue
		}
		d := editDistance(q, n, maxDist)
		if d < best {
			best, bestNames = d, []string{n}
		} else if d == best {
			bestNames = append(bestNames, n)
		}
	}
	if best > maxDist || len(bestNames) == 0 {
		return nil, "", 0, false
	}
	set := map[string]bool{}
	for _, n := range bestNames {
		for _, tk := range a.byName[n] {
			set[tk] = true
		}
	}
	if len(set) != 1 {
		return nil, "", best, false
	}
	for tk := range set {
		tickers = append(tickers, tk)
	}
	return tickers, bestNames[0], best, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// editDistance is the Levenshtein distance, stopping early above limit (returns limit+1).
func editDistance(a, b string, limit int) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		minRow := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if cur[j] < minRow {
				minRow = cur[j]
			}
		}
		if minRow > limit {
			return limit + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
