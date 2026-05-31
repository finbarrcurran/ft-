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
	Ticker          string
	Score           int
	MaxScore        int
	Adapter         string
	SubType         string
	LockedDate      string // ISO YYYY-MM-DD, empty if unknown
	GitHubURL       string // canonical blob URL for click-through
	EarningsUrgency string // theses_index.earnings_urgency — 'none'|'amber'|'red'|'revision_needed' (SC-02 freshness)
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
	             COALESCE(github_url, ''), COALESCE(earnings_urgency, 'none')
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
		if err := rows.Scan(&r.Ticker, &score, &maxScore, &r.Adapter, &r.SubType, &r.LockedDate, &r.GitHubURL, &r.EarningsUrgency); err != nil {
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

// CryptoThesisScoreRow is the compact shape returned by
// CryptoThesisScoresBySymbol — the crypto analogue of ThesisScoreRow,
// sourced from crypto_theses (Spec 9k) rather than theses_index. SC-10.
//
// Scale is carried explicitly via MaxScore (12 for BTC monetary_12, 18 for
// alt_18) so the UI can render "13/18 — Accumulate" without re-deriving it.
type CryptoThesisScoreRow struct {
	Symbol        string
	Score         int
	MaxScore      int
	Band          string // 'strong'|'accumulate'|'hold'|'trim'|'exit'
	ScorecardType string // 'alt_18' | 'monetary_12'
	LockedDate    string // ISO YYYY-MM-DD derived from locked_at, empty if unknown
	GitHubURL     string // canonical blob URL when present
}

// CryptoThesisScoresBySymbol returns one CryptoThesisScoreRow per coin
// symbol that has a locked thesis in crypto_theses. Used by handleListCrypto
// to overlay the locked-thesis score onto the Crypto tab's Score column —
// the crypto holdings previously (mis)used ThesisScoresByTicker, which only
// reads theses_index (stocks) and so always missed crypto symbols, leaving
// the column blank. SC-10.
//
// Case-insensitive symbol match. Returns an empty map (not nil) when no
// matches.
func (s *Store) CryptoThesisScoresBySymbol(ctx context.Context, symbols []string) (map[string]*CryptoThesisScoreRow, error) {
	out := map[string]*CryptoThesisScoreRow{}
	if len(symbols) == 0 {
		return out, nil
	}
	seen := map[string]bool{}
	args := make([]any, 0, len(symbols))
	placeholders := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		ss := strings.ToUpper(strings.TrimSpace(sym))
		if ss == "" || seen[ss] {
			continue
		}
		seen[ss] = true
		args = append(args, ss)
		placeholders = append(placeholders, "?")
	}
	if len(args) == 0 {
		return out, nil
	}
	// Latest locked version per symbol. crypto_theses can hold multiple
	// versions (v1, v2…); take the row with the highest id among locked
	// rows for each symbol so the Score column tracks the current lock.
	q := `SELECT t.coin_symbol, t.total_score, t.max_score, t.band,
	             t.scorecard_type,
	             COALESCE(strftime('%Y-%m-%d', t.locked_at, 'unixepoch'), ''),
	             COALESCE(t.github_url, '')
	        FROM crypto_theses t
	        JOIN (
	             SELECT coin_symbol, MAX(id) AS max_id
	               FROM crypto_theses
	              WHERE status = 'locked'
	           GROUP BY coin_symbol
	        ) latest ON latest.max_id = t.id
	       WHERE UPPER(t.coin_symbol) IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r CryptoThesisScoreRow
		if err := rows.Scan(&r.Symbol, &r.Score, &r.MaxScore, &r.Band, &r.ScorecardType, &r.LockedDate, &r.GitHubURL); err != nil {
			return nil, err
		}
		out[strings.ToUpper(r.Symbol)] = &r
	}
	return out, rows.Err()
}
