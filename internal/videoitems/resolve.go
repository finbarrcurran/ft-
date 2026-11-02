package videoitems

import (
	"context"
	"sort"
	"strings"
)

// Candidate is one result of a symbol search.
type Candidate struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange,omitempty"`
}

// Lookup searches for a symbol by company name (Yahoo's search in production). It may be nil.
type Lookup func(ctx context.Context, query string) ([]Candidate, error)

// Resolution is what the resolver found for a name the speaker said.
type Resolution struct {
	Name        string      `json:"name"`
	Ticker      string      `json:"ticker,omitempty"`
	Status      string      `json:"status"` // resolved | unresolved
	Source      string      `json:"source,omitempty"`
	Matched     string      `json:"matched,omitempty"`
	ModelTicker string      `json:"modelTicker,omitempty"` // what the model proposed; unverified
	Conflict    bool        `json:"conflict,omitempty"`    // the lookup disagrees with the model's ticker
	Candidates  []Candidate `json:"candidates,omitempty"`
}

// Resolver turns a name as spoken into a ticker: the alias file first, then a fuzzy match (edit
// distance 2, for ASR spellings such as "Lummentum"), then the symbol search. Anything else stays
// unresolved and is shown as such, never guessed.
type Resolver struct {
	A      *Aliases
	Lookup Lookup
}

func isFuturesOrCurrency(sym string) bool {
	return strings.Contains(sym, "=") || strings.Contains(sym, "^")
}

func pick(tickers []string, model string) string {
	sort.Strings(tickers)
	for _, t := range tickers {
		if strings.EqualFold(t, model) {
			return t
		}
	}
	return tickers[0]
}

// Resolve resolves one name; modelTicker is the model's own proposal (may be empty).
func (r *Resolver) Resolve(ctx context.Context, name, modelTicker string) Resolution {
	a := r.A
	if a == nil {
		a = Default
	}
	res := Resolution{Name: name, Status: "unresolved", ModelTicker: strings.ToUpper(strings.TrimSpace(modelTicker))}
	set := func(t, src, matched string) Resolution {
		res.Ticker, res.Status, res.Source, res.Matched = t, "resolved", src, matched
		res.Conflict = res.ModelTicker != "" && !strings.EqualFold(res.ModelTicker, t)
		return res
	}
	n := NormName(name)
	if n == "" {
		return res
	}
	if ts := a.Tickers(n); len(ts) > 0 {
		return set(pick(append([]string(nil), ts...), res.ModelTicker), "alias", n)
	}
	if up := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(name), " ", "")); len(up) <= 6 && a.Has(up) {
		return set(up, "symbol", up)
	}
	if ts, matched, _, ok := a.Fuzzy(n, 2); ok {
		return set(ts[0], "fuzzy", matched)
	}
	if r.Lookup != nil {
		if cands, err := r.Lookup(ctx, name); err == nil {
			for _, c := range cands {
				if isFuturesOrCurrency(c.Symbol) {
					continue
				}
				res.Candidates = append(res.Candidates, c)
			}
			if len(res.Candidates) > 5 {
				res.Candidates = res.Candidates[:5]
			}
			if len(res.Candidates) > 0 {
				c := res.Candidates[0]
				cn := NormName(c.Name)
				if cn != "" && (strings.Contains(cn, n) || strings.Contains(n, cn) || firstWordMatches(cn, n)) {
					return set(strings.ToUpper(c.Symbol), "search", c.Name)
				}
			}
		}
	}
	return res
}

func firstWordMatches(a, b string) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	return len(fa) > 0 && len(fb) > 0 && len(fa[0]) >= 4 && fa[0] == fb[0]
}
