package store

import (
	"context"
	"strings"
)

// ThesisScoreRow is the compact shape returned by ThesisScoresByTicker —
// just enough for the Stocks/ETFs tab to overlay locked-thesis scores on
// the per-holding Score column without re-fetching the full theses_index
// row. v1.19B.
type ThesisScoreRow struct {
	Ticker     string
	Score      int
	MaxScore   int
	Adapter    string
	SubType    string
	LockedDate string // ISO YYYY-MM-DD, empty if unknown
	GitHubURL  string // canonical blob URL for click-through
}

// ThesisScoresByTicker returns one ThesisScoreRow per ticker that has a
// non-superseded locked thesis in theses_index. Used by handleListStocks
// (and similar) to overlay the locked-thesis score onto each holding so
// the Score column on the Stocks tab tracks the lock state automatically.
//
// Case-insensitive ticker match: theses_index stores tickers in their
// canonical form (e.g. "RHM.DE", "4063.T"), and we accept whatever case
// the holding row provides.
//
// Returns an empty map (not nil) when no matches.
func (s *Store) ThesisScoresByTicker(ctx context.Context, tickers []string) (map[string]*ThesisScoreRow, error) {
	out := map[string]*ThesisScoreRow{}
	if len(tickers) == 0 {
		return out, nil
	}
	// De-dup + uppercase normalise.
	seen := map[string]bool{}
	args := make([]any, 0, len(tickers))
	placeholders := make([]string, 0, len(tickers))
	for _, t := range tickers {
		tt := strings.ToUpper(strings.TrimSpace(t))
		if tt == "" || seen[tt] {
			continue
		}
		seen[tt] = true
		args = append(args, tt)
		placeholders = append(placeholders, "?")
	}
	if len(args) == 0 {
		return out, nil
	}
	q := `SELECT ticker, score, max_score, adapter,
	             COALESCE(sub_type, ''), COALESCE(locked_date, ''),
	             COALESCE(github_url, '')
	        FROM theses_index
	       WHERE status != 'superseded'
	         AND UPPER(ticker) IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ThesisScoreRow
		var score, maxScore *int
		if err := rows.Scan(&r.Ticker, &score, &maxScore, &r.Adapter, &r.SubType, &r.LockedDate, &r.GitHubURL); err != nil {
			return nil, err
		}
		if score != nil {
			r.Score = *score
		}
		if maxScore != nil {
			r.MaxScore = *maxScore
		}
		// Key by uppercase for case-insensitive lookup.
		out[strings.ToUpper(r.Ticker)] = &r
	}
	return out, rows.Err()
}
