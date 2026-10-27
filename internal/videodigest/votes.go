package videodigest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// SC-52 — majority vote for the newest Jordi digest only (the one behind the login card). The
// regime state of a single extraction is not perfectly stable, so the newest digest is extracted
// `runs` times and the majority state is kept; a split forces confidence to "low" and is shown on
// the card as "2 of 3 runs". Never applied to the backfill (it costs about $0.03 per extra run).

// caution orders the states for tie-breaks: the more cautious wins.
var caution = map[string]int{"stable": 1, "shifting": 2, "defensive": 3}

// VoteResult is what a vote found.
type VoteResult struct {
	Runs, Agree int
	State       string
	Counts      map[string]int
	Split       bool
	Changed     bool // the stored proposal was replaced by another run's
	CostUSD     float64
}

type voteRun struct {
	state, conf, quote, match, narrative string
	ts                                   *int
}

func derefS(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// VoteNewest extracts videoID again (runs-1) times without storing those runs, tallies the states
// together with the stored digest's own, and updates the stored proposal to the majority.
func (s *Service) VoteNewest(ctx context.Context, videoID string, runs int) (VoteResult, error) {
	var res VoteResult
	if runs < 2 {
		return res, nil
	}
	var id int64
	var st, cf, q, mt, nar sql.NullString
	var ts sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, proposed_regime, proposed_confidence, proposed_quote, proposed_quote_match, regime_narrative, proposed_quote_ts
		FROM video_digests WHERE video_id = ?`, videoID).Scan(&id, &st, &cf, &q, &mt, &nar, &ts)
	if err != nil {
		return res, fmt.Errorf("vote: no stored digest for %s: %w", videoID, err)
	}
	all := []voteRun{{state: st.String, conf: cf.String, quote: q.String, match: mt.String, narrative: nar.String}}
	if ts.Valid {
		t := int(ts.Int64)
		all[0].ts = &t
	}
	for i := 1; i < runs; i++ {
		pv, err := s.Preview(ctx, videoID, true)
		if err != nil {
			return res, fmt.Errorf("vote run %d: %w", i+1, err)
		}
		res.CostUSD += pv.CostUSD
		ex := pv.Extraction
		all = append(all, voteRun{state: derefS(ex.ProposedRegime), conf: derefS(ex.ProposedConfidence), quote: derefS(ex.ProposedQuote),
			match: derefS(ex.ProposedQuoteMatch), narrative: derefS(ex.RegimeNarrative), ts: ex.ProposedQuoteTs})
	}
	res.Runs = runs
	res.Counts = map[string]int{}
	for _, r := range all {
		if r.state != "" {
			res.Counts[r.state]++
		}
	}
	best, bestN := "", 0
	for stt, n := range res.Counts {
		if n > bestN || (n == bestN && caution[stt] > caution[best]) {
			best, bestN = stt, n
		}
	}
	if best == "" { // no run proposed a state: nothing to vote on
		return res, nil
	}
	res.State, res.Agree = best, bestN
	res.Split = bestN < runs
	// Representative run: the stored one if it voted for the majority, else the first run that did.
	var rep voteRun
	for i, r := range all {
		if r.state == best {
			rep = r
			res.Changed = i != 0
			break
		}
	}
	conf := rep.conf
	if res.Split {
		conf = "low"
	}
	var tsArg any
	if rep.ts != nil {
		tsArg = *rep.ts
	}
	if res.Changed {
		_, err = s.DB.ExecContext(ctx, `UPDATE video_digests SET proposed_regime = ?, proposed_confidence = ?, proposed_quote = ?, proposed_quote_ts = ?,
			proposed_quote_match = ?, regime_narrative = COALESCE(NULLIF(?, ''), regime_narrative), proposal_runs = ?, proposal_agree = ? WHERE id = ?`,
			best, conf, nullable(rep.quote), tsArg, nullable(rep.match), rep.narrative, runs, bestN, id)
	} else {
		_, err = s.DB.ExecContext(ctx, `UPDATE video_digests SET proposed_confidence = ?, proposal_runs = ?, proposal_agree = ? WHERE id = ?`, conf, runs, bestN, id)
	}
	if err != nil {
		return res, err
	}
	slog.Info("videodigest: newest Jordi digest voted", "video", videoID, "runs", runs, "agree", bestN, "state", best, "split", res.Split, "changed", res.Changed)
	return res, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// isNewestJordi reports whether videoID's digest is the newest Jordi digest by publication date.
func (s *Service) isNewestJordi(ctx context.Context, videoID string) bool {
	var vid string
	err := s.DB.QueryRowContext(ctx, `SELECT video_id FROM video_digests WHERE source = 'jordi' ORDER BY published_at DESC, id DESC LIMIT 1`).Scan(&vid)
	return err == nil && vid == videoID
}
