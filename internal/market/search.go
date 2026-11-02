package market

import (
	"context"
	"net/url"

	"ft/internal/health"
)

// SymbolHit is one result of Yahoo's symbol search.
type SymbolHit struct {
	Symbol   string
	Name     string
	Exchange string
	Type     string
}

// SearchSymbols asks Yahoo's search endpoint which instruments match a company name (SC-53: the
// last step of resolving a name the speaker said). One call per unresolved name; no key needed.
func SearchSymbols(ctx context.Context, query string) ([]SymbolHit, error) {
	u := "https://query2.finance.yahoo.com/v1/finance/search?quotesCount=5&newsCount=0&q=" + url.QueryEscape(query)
	var out struct {
		Quotes []struct {
			Symbol    string `json:"symbol"`
			ShortName string `json:"shortname"`
			LongName  string `json:"longname"`
			Exchange  string `json:"exchange"`
			QuoteType string `json:"quoteType"`
		} `json:"quotes"`
	}
	err := httpGetJSON(ctx, u, &out)
	health.Record(ctx, "yahoo", err)
	if err != nil {
		return nil, err
	}
	hits := make([]SymbolHit, 0, len(out.Quotes))
	for _, q := range out.Quotes {
		name := q.LongName
		if name == "" {
			name = q.ShortName
		}
		hits = append(hits, SymbolHit{Symbol: q.Symbol, Name: name, Exchange: q.Exchange, Type: q.QuoteType})
	}
	return hits, nil
}
