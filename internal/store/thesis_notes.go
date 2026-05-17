// Spec 11 — thesis_notes append-only observation log.
//
// Single source of truth for:
//   - InsertThesisNote / UpdateThesisNote / SoftDeleteThesisNote
//   - ListThesisNotes (filterable by target / ticker / factor / time range)
//   - StaleThesisCandidates — feeds the Summary D6 banner
//
// v1 allows UPDATE (typo correction) but never physically deletes — soft
// delete via deleted_at. Append-only invariant from Spec 9d/10 still
// applies in spirit: history rows stay.

package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const thesisNoteCols = `id, target_kind, target_id, ticker,
        observation_at, observation_text,
        framework_id, factor_id, factor_direction,
        source_url, source_kind,
        created_at, updated_at, deleted_at`

// ThesisNote mirrors the thesis_notes table.
type ThesisNote struct {
	ID              int64      `json:"id"`
	TargetKind      string     `json:"targetKind"` // "holding" | "watchlist"
	TargetID        int64      `json:"targetId"`
	Ticker          string     `json:"ticker"`
	ObservationAt   string     `json:"observationAt"` // ISO YYYY-MM-DD
	ObservationText string     `json:"observationText"`
	FrameworkID     *string    `json:"frameworkId,omitempty"`
	FactorID        *string    `json:"factorId,omitempty"`
	FactorDirection *string    `json:"factorDirection,omitempty"`
	SourceURL       *string    `json:"sourceUrl,omitempty"`
	SourceKind      *string    `json:"sourceKind,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

// ThesisNoteFilter narrows the listing.
// Zero / empty fields mean "no filter on this dimension".
type ThesisNoteFilter struct {
	TargetKind    string // "holding" | "watchlist" | ""
	TargetID      int64  // 0 = any
	Ticker        string // "" = any
	FrameworkID   string // "" = any
	FactorID      string // "" = any
	FromDate      string // ISO; "" = unbounded lower
	ToDate        string // ISO; "" = unbounded upper
	IncludeDeleted bool
	Limit         int    // 0 = no limit
}

// InsertThesisNote appends a new note. Ticker is uppercased.
func (s *Store) InsertThesisNote(ctx context.Context, n *ThesisNote) (int64, error) {
	n.Ticker = strings.ToUpper(strings.TrimSpace(n.Ticker))
	now := time.Now().UTC().Unix()
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO thesis_notes (
		  target_kind, target_id, ticker,
		  observation_at, observation_text,
		  framework_id, factor_id, factor_direction,
		  source_url, source_kind,
		  created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.TargetKind, n.TargetID, n.Ticker,
		n.ObservationAt, n.ObservationText,
		nullPtr(n.FrameworkID), nullPtr(n.FactorID), nullPtr(n.FactorDirection),
		nullPtr(n.SourceURL), nullPtr(n.SourceKind),
		now, now,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateThesisNote rewrites the text + tagging fields. observation_at,
// target, ticker stay frozen — those would be a different note.
func (s *Store) UpdateThesisNote(ctx context.Context, id int64, n *ThesisNote) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE thesis_notes
		   SET observation_text = ?,
		       framework_id      = ?,
		       factor_id         = ?,
		       factor_direction  = ?,
		       source_url        = ?,
		       source_kind       = ?,
		       updated_at        = strftime('%s','now')
		 WHERE id = ? AND deleted_at IS NULL`,
		n.ObservationText,
		nullPtr(n.FrameworkID), nullPtr(n.FactorID), nullPtr(n.FactorDirection),
		nullPtr(n.SourceURL), nullPtr(n.SourceKind),
		id,
	)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteThesisNote stamps deleted_at. Row stays in DB.
func (s *Store) SoftDeleteThesisNote(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE thesis_notes SET deleted_at = strftime('%s','now')
		 WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetThesisNote returns one note by id.
func (s *Store) GetThesisNote(ctx context.Context, id int64) (*ThesisNote, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+thesisNoteCols+` FROM thesis_notes WHERE id = ?`, id)
	n, err := scanThesisNote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

// ListThesisNotes returns notes matching the filter, newest observation
// first, ties broken by created_at DESC.
func (s *Store) ListThesisNotes(ctx context.Context, f ThesisNoteFilter) ([]*ThesisNote, error) {
	q := `SELECT ` + thesisNoteCols + ` FROM thesis_notes WHERE 1=1`
	args := []any{}
	if !f.IncludeDeleted {
		q += ` AND deleted_at IS NULL`
	}
	if f.TargetKind != "" {
		q += ` AND target_kind = ?`
		args = append(args, f.TargetKind)
	}
	if f.TargetID != 0 {
		q += ` AND target_id = ?`
		args = append(args, f.TargetID)
	}
	if f.Ticker != "" {
		q += ` AND ticker = ?`
		args = append(args, strings.ToUpper(f.Ticker))
	}
	if f.FrameworkID != "" {
		q += ` AND framework_id = ?`
		args = append(args, f.FrameworkID)
	}
	if f.FactorID != "" {
		q += ` AND factor_id = ?`
		args = append(args, f.FactorID)
	}
	if f.FromDate != "" {
		q += ` AND observation_at >= ?`
		args = append(args, f.FromDate)
	}
	if f.ToDate != "" {
		q += ` AND observation_at <= ?`
		args = append(args, f.ToDate)
	}
	q += ` ORDER BY observation_at DESC, created_at DESC`
	if f.Limit > 0 {
		q += ` LIMIT ?`
		args = append(args, f.Limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ThesisNote
	for rows.Next() {
		n, err := scanThesisNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// StaleHoldingCandidate is one row of the Spec 11 D6 staleness check.
type StaleHoldingCandidate struct {
	HoldingKind     string `json:"holdingKind"`
	HoldingID       int64  `json:"holdingId"`
	Ticker          string `json:"ticker"`
	Name            string `json:"name"`
	LastObservation string `json:"lastObservation,omitempty"` // ISO; empty if never
	DaysSince       int    `json:"daysSince"`                 // -1 = never
}

// StaleThesisCandidates returns active holdings whose most recent
// thesis_notes observation is older than the cutoff AND whose most recent
// framework_scores row is also older than the cutoff. Per Spec 11 D6.
//
// Holdings with no notes and no scores are also returned (DaysSince=-1
// surfaces in UI as "no note yet").
func (s *Store) StaleThesisCandidates(ctx context.Context, userID int64, cutoffDays int) ([]StaleHoldingCandidate, error) {
	if cutoffDays <= 0 {
		cutoffDays = 90
	}
	now := time.Now().UTC()
	cutoffISO := now.AddDate(0, 0, -cutoffDays).Format("2006-01-02")
	// Latest note per (kind,id), latest score per (kind,id).
	// note observation_at is ISO date; framework_scores.scored_at is INTEGER unix.
	q := `
		SELECT 'stock' AS holding_kind, h.id, COALESCE(h.ticker, h.name) AS ticker, h.name,
		       (SELECT MAX(observation_at) FROM thesis_notes
		         WHERE target_kind='holding' AND target_id=h.id AND deleted_at IS NULL) AS last_note,
		       (SELECT MAX(scored_at) FROM framework_scores
		         WHERE target_kind='holding' AND target_id=h.id AND user_id=?) AS last_score_unix
		  FROM stock_holdings h
		 WHERE h.user_id = ? AND h.deleted_at IS NULL
		UNION ALL
		SELECT 'crypto', h.id, h.symbol, h.name,
		       (SELECT MAX(observation_at) FROM thesis_notes
		         WHERE target_kind='holding' AND target_id=h.id AND deleted_at IS NULL),
		       (SELECT MAX(scored_at) FROM framework_scores
		         WHERE target_kind='holding' AND target_id=h.id AND user_id=?)
		  FROM crypto_holdings h
		 WHERE h.user_id = ? AND h.deleted_at IS NULL`
	rows, err := s.DB.QueryContext(ctx, q, userID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaleHoldingCandidate{}
	for rows.Next() {
		var c StaleHoldingCandidate
		var lastNote sql.NullString
		var lastScoreUnix sql.NullInt64
		if err := rows.Scan(&c.HoldingKind, &c.HoldingID, &c.Ticker, &c.Name, &lastNote, &lastScoreUnix); err != nil {
			return nil, err
		}
		// Convert score unix → ISO date for comparison with note ISO date.
		latestISO := ""
		if lastNote.Valid {
			latestISO = lastNote.String
		}
		if lastScoreUnix.Valid {
			scoreISO := time.Unix(lastScoreUnix.Int64, 0).UTC().Format("2006-01-02")
			if scoreISO > latestISO {
				latestISO = scoreISO
			}
		}
		// Apply cutoff: skip rows fresher than cutoff.
		if latestISO == "" {
			c.LastObservation = ""
			c.DaysSince = -1
			out = append(out, c)
			continue
		}
		if latestISO >= cutoffISO {
			continue
		}
		if t, err := time.Parse("2006-01-02", latestISO); err == nil {
			c.LastObservation = latestISO
			c.DaysSince = int(now.Sub(t).Hours() / 24)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// FactorContradictions returns the most recent 'contradicts'-direction
// notes for one holding/watchlist target, grouped by (framework_id,
// factor_id) so the score screen can highlight the relevant question.
// Returns at most one note per factor — the newest one.
func (s *Store) FactorContradictions(ctx context.Context, targetKind string, targetID int64) ([]*ThesisNote, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+thesisNoteCols+` FROM thesis_notes t1
		 WHERE t1.target_kind = ? AND t1.target_id = ?
		   AND t1.deleted_at IS NULL
		   AND t1.factor_direction = 'contradicts'
		   AND t1.framework_id IS NOT NULL
		   AND t1.factor_id IS NOT NULL
		   AND t1.observation_at = (
		     SELECT MAX(observation_at) FROM thesis_notes t2
		      WHERE t2.target_kind = t1.target_kind
		        AND t2.target_id = t1.target_id
		        AND t2.framework_id = t1.framework_id
		        AND t2.factor_id = t1.factor_id
		        AND t2.factor_direction = 'contradicts'
		        AND t2.deleted_at IS NULL
		   )
		 ORDER BY observation_at DESC, id DESC`,
		targetKind, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ThesisNote
	for rows.Next() {
		n, err := scanThesisNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ----- scanning ---------------------------------------------------------

type rowScanner interface{ Scan(dest ...any) error }

func scanThesisNote(r rowScanner) (*ThesisNote, error) {
	var n ThesisNote
	var fw, fid, fdir, surl, skind sql.NullString
	var createdAt, updatedAt int64
	var deletedAt sql.NullInt64
	if err := r.Scan(
		&n.ID, &n.TargetKind, &n.TargetID, &n.Ticker,
		&n.ObservationAt, &n.ObservationText,
		&fw, &fid, &fdir,
		&surl, &skind,
		&createdAt, &updatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	if fw.Valid {
		v := fw.String
		n.FrameworkID = &v
	}
	if fid.Valid {
		v := fid.String
		n.FactorID = &v
	}
	if fdir.Valid {
		v := fdir.String
		n.FactorDirection = &v
	}
	if surl.Valid {
		v := surl.String
		n.SourceURL = &v
	}
	if skind.Valid {
		v := skind.String
		n.SourceKind = &v
	}
	n.CreatedAt = time.Unix(createdAt, 0).UTC()
	n.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0).UTC()
		n.DeletedAt = &t
	}
	return &n, nil
}

// nullPtr returns nil if the pointer or the dereffed value is empty.
func nullPtr(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
