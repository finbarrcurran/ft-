// Package cryptotheses — D26 cascade-acknowledgment workflow.
//
// When the cascade engine flags a thesis as `needs-review` (after a parent
// thesis band drop triggers `flagged_needs_review` action via
// `cascade_engine.CheckCascadeOnRescore`), the thesis sits in the
// `needs-review` state until a human acknowledges the cascade impact and
// re-locks. D25 explicitly carved out `needs-review` (PUT + /lock return 400);
// D26 closes that gap.
//
// Workflow:
//   1. POST /api/crypto/theses/{symbol}/{version}/acknowledge-cascade
//   2. Validate status='needs-review' (other states reject with 400)
//   3. Re-run PPG check using current stored pillar_scores_json (no re-score
//      — pillar scores unchanged by cascade flagging)
//   4. Append `event_rescore` history row with `event_reason='cascade_acknowledgment'`
//      and `triggered_by='user'` (follows LINK v0.5 §L.9.4 pattern)
//   5. Mark all unresolved cascade_events for this thesis as resolved with
//      timestamp + acknowledgment note
//   6. Transition status `needs-review` → `locked`
//
// If pillar score state has materially changed since flagging (PPG now fails
// when previously passing), reject — author must fork to v2 instead.
// Fork-to-v2 workflow is post-D26.

package cryptotheses

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AcknowledgeResult bundles the outcome of an acknowledge-cascade call.
type AcknowledgeResult struct {
	ThesisID                 int64  `json:"thesisId"`
	Symbol                   string `json:"symbol"`
	Version                  string `json:"version"`
	PreviousStatus           string `json:"previousStatus"`
	NewStatus                string `json:"newStatus"`
	CascadeEventsResolvedCount int   `json:"cascadeEventsResolvedCount"`
	HistoryRowID             int64  `json:"historyRowId"`
}

// ErrNotNeedsReview is returned when caller tries to acknowledge a thesis
// not in needs-review state.
var ErrNotNeedsReview = errors.New("thesis is not in needs-review state; only needs-review theses can be acknowledged")

// ErrPPGNowFails is returned when the stored pillar scores no longer pass
// the PPG check at acknowledgment time. Author must fork to v2 instead of
// re-locking.
var ErrPPGNowFails = errors.New("thesis pillar scores no longer pass PPG; fork to v2 required (D26 acknowledgment scope is for PPG-passing theses only)")

// AcknowledgeCascade transitions a needs-review thesis back to locked after
// human review of the triggering cascade events.
func (s *ThesisWriteService) AcknowledgeCascade(ctx context.Context, symbol, version, note string) (*AcknowledgeResult, error) {
	symbol = strings.ToUpper(symbol)

	// Pull current thesis state
	var id int64
	var status, pillarJSON, scorecard, currentBand string
	var totalScore int
	err := s.DB.QueryRowContext(ctx, `
		SELECT id, status, pillar_scores_json, scorecard_type, band, total_score
		  FROM crypto_theses
		 WHERE coin_symbol = ? AND version = ?`, symbol, version).Scan(
		&id, &status, &pillarJSON, &scorecard, &currentBand, &totalScore)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrThesisNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != "needs-review" {
		return nil, fmt.Errorf("%w (current status: %s)", ErrNotNeedsReview, status)
	}

	// Parse stored pillar scores (compound shape from D25 or legacy from seed fixtures)
	scores := ParsePillarScoresJSON(pillarJSON)
	if len(scores) == 0 {
		return nil, fmt.Errorf("could not parse pillar_scores_json for %s v%s", symbol, version)
	}

	// Re-verify PPG using authoritative ComputeRawAndFinalBand.
	// Important: we do NOT recompute pillar scores from sub-criteria here —
	// the cascade flagging did not change them. We only verify the stored
	// scores still pass PPG (defensive — handles edge case of data drift
	// between flagging and acknowledgment).
	sc := ScorecardType(scorecard)
	bandResult := ComputeRawAndFinalBand(AllPillarScores(scores), sc)
	// If PPG fails AND the recomputed final band is worse than what's
	// currently stored, the thesis is in a degraded state since flagging —
	// reject acknowledgment.
	if bandResult.PPGCapApplied && Band(currentBand).Rank() < bandResult.FinalBand.Rank() {
		return nil, fmt.Errorf("%w (recomputed final band %s worse than stored %s)",
			ErrPPGNowFails, bandResult.FinalBand, currentBand)
	}

	// Begin tx
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	ackNote := strings.TrimSpace(note)
	if ackNote == "" {
		ackNote = "cascade impact acknowledged via D26"
	}

	// Mark all unresolved cascade_events for this thesis as resolved.
	res, err := tx.ExecContext(ctx, `
		UPDATE cascade_events
		   SET resolved_at = ?, resolution_note = ?
		 WHERE affected_thesis_id = ? AND resolved_at IS NULL`,
		now, ackNote, id)
	if err != nil {
		return nil, fmt.Errorf("resolve cascade events: %w", err)
	}
	eventsResolved, _ := res.RowsAffected()

	// Append event_rescore history row capturing the acknowledgment.
	// Follows LINK v0.5 §L.9.4 pattern (event_rescore + event_reason).
	historyRes, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_thesis_history
		  (thesis_id, event_type, event_reason, pillar_scores_json, total_score, band, delta, recommended_action, triggered_by)
		VALUES (?, 'event_rescore', ?, ?, ?, ?, 0, 'none', 'user')`,
		id,
		fmt.Sprintf("cascade_acknowledgment: %s", ackNote),
		pillarJSON, totalScore, currentBand)
	if err != nil {
		return nil, fmt.Errorf("history insert: %w", err)
	}
	historyID, _ := historyRes.LastInsertId()

	// Transition status needs-review → locked.
	if _, err = tx.ExecContext(ctx, `
		UPDATE crypto_theses
		   SET status = 'locked',
		       last_reviewed_at = ?,
		       updated_at = ?
		 WHERE id = ? AND status = 'needs-review'`,
		now, now, id); err != nil {
		return nil, fmt.Errorf("status transition: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &AcknowledgeResult{
		ThesisID:                   id,
		Symbol:                     symbol,
		Version:                    version,
		PreviousStatus:             "needs-review",
		NewStatus:                  "locked",
		CascadeEventsResolvedCount: int(eventsResolved),
		HistoryRowID:               historyID,
	}, nil
}
