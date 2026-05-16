package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"ft/internal/domain"
	"strings"
	"time"
)

const fsCols = `id, user_id, target_kind, target_id, framework_id, scored_at,
        total_score, max_score, passes, scores_json, tags_json, reviewer_note`

// InsertFrameworkScore appends a new row. Framework_scores is append-only —
// re-scoring an entry creates a new row.
func (s *Store) InsertFrameworkScore(ctx context.Context, fs *domain.FrameworkScore) (*domain.FrameworkScore, error) {
	if fs.ScoredAt.IsZero() {
		fs.ScoredAt = time.Now().UTC()
	}
	passes := 0
	if fs.Passes {
		passes = 1
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO framework_scores (
			user_id, target_kind, target_id, framework_id, scored_at,
			total_score, max_score, passes, scores_json, tags_json, reviewer_note
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		fs.UserID, fs.TargetKind, fs.TargetID, fs.FrameworkID, fs.ScoredAt.Unix(),
		fs.TotalScore, fs.MaxScore, passes, fs.ScoresJSON, fs.TagsJSON, fs.ReviewerNote)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	fs.ID = id
	return fs, nil
}

// InsertFrameworkScoreTx is the tx-aware variant used by the promote flow.
func (s *Store) InsertFrameworkScoreTx(ctx context.Context, tx *sql.Tx, fs *domain.FrameworkScore) error {
	if fs.ScoredAt.IsZero() {
		fs.ScoredAt = time.Now().UTC()
	}
	passes := 0
	if fs.Passes {
		passes = 1
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO framework_scores (
			user_id, target_kind, target_id, framework_id, scored_at,
			total_score, max_score, passes, scores_json, tags_json, reviewer_note
		) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		fs.UserID, fs.TargetKind, fs.TargetID, fs.FrameworkID, fs.ScoredAt.Unix(),
		fs.TotalScore, fs.MaxScore, passes, fs.ScoresJSON, fs.TagsJSON, fs.ReviewerNote)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	fs.ID = id
	return nil
}

// LatestFrameworkScore returns the most recent score for a target, or
// ErrNotFound if never scored.
func (s *Store) LatestFrameworkScore(ctx context.Context, userID int64, targetKind string, targetID int64) (*domain.FrameworkScore, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+fsCols+`
		  FROM framework_scores
		 WHERE user_id = ? AND target_kind = ? AND target_id = ?
		 ORDER BY scored_at DESC, id DESC
		 LIMIT 1`, userID, targetKind, targetID)
	fs, err := scanFrameworkScore(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return fs, err
}

// LatestFrameworkScoresMany is the batch variant used by the holdings list
// handler — pulls the latest score per target in one trip.
func (s *Store) LatestFrameworkScoresMany(ctx context.Context, userID int64, targetKind string, targetIDs []int64) (map[int64]*domain.FrameworkScore, error) {
	out := map[int64]*domain.FrameworkScore{}
	if len(targetIDs) == 0 {
		return out, nil
	}
	// Build IN (?,?,...) list.
	placeholders := make([]string, len(targetIDs))
	args := make([]any, 0, len(targetIDs)+2)
	args = append(args, userID, targetKind)
	for i, id := range targetIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	// We want the latest per target. SQLite supports correlated subqueries fine.
	q := fmt.Sprintf(`
		SELECT %s FROM framework_scores fs
		 WHERE fs.user_id = ? AND fs.target_kind = ? AND fs.target_id IN (%s)
		   AND fs.scored_at = (
		     SELECT MAX(scored_at) FROM framework_scores
		      WHERE user_id = fs.user_id AND target_kind = fs.target_kind AND target_id = fs.target_id
		   )`, fsCols, strings.Join(placeholders, ","))
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		fs, err := scanFrameworkScore(rows)
		if err != nil {
			return nil, err
		}
		out[fs.TargetID] = fs
	}
	return out, rows.Err()
}

// HistoryFrameworkScores returns all scores for a target, newest first. Used
// by the "score history" panel on the 8-question screen.
func (s *Store) HistoryFrameworkScores(ctx context.Context, userID int64, targetKind string, targetID int64, limit int) ([]*domain.FrameworkScore, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+fsCols+`
		  FROM framework_scores
		 WHERE user_id = ? AND target_kind = ? AND target_id = ?
		 ORDER BY scored_at DESC, id DESC
		 LIMIT ?`, userID, targetKind, targetID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.FrameworkScore
	for rows.Next() {
		fs, err := scanFrameworkScore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fs)
	}
	return out, rows.Err()
}

func scanFrameworkScore(r scanner) (*domain.FrameworkScore, error) {
	var (
		fs       domain.FrameworkScore
		scoredAt int64
		passes   int
		tagsJSON sql.NullString
		revNote  sql.NullString
	)
	err := r.Scan(
		&fs.ID, &fs.UserID, &fs.TargetKind, &fs.TargetID, &fs.FrameworkID,
		&scoredAt, &fs.TotalScore, &fs.MaxScore, &passes, &fs.ScoresJSON,
		&tagsJSON, &revNote,
	)
	if err != nil {
		return nil, err
	}
	fs.ScoredAt = time.Unix(scoredAt, 0).UTC()
	fs.Passes = passes == 1
	if tagsJSON.Valid {
		v := tagsJSON.String
		fs.TagsJSON = &v
	}
	if revNote.Valid {
		v := revNote.String
		fs.ReviewerNote = &v
	}
	return &fs, nil
}
