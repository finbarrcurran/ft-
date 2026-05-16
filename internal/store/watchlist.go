package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"strings"
	"time"
)

const watchlistCols = `id, user_id, ticker, kind, company_name, sector,
        current_price, target_entry_low, target_entry_high,
        thesis_link, note, added_at, promoted_holding_id, deleted_at,
        support_1, support_2, resistance_1, resistance_2,
        atr_weekly, vol_tier_auto, setup_type,
        updated_at`

// ListWatchlist returns active (non-deleted) entries for a user, newest first.
func (s *Store) ListWatchlist(ctx context.Context, userID int64) ([]*domain.WatchlistEntry, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+watchlistCols+`
		 FROM watchlist WHERE user_id = ? AND deleted_at IS NULL
		 ORDER BY added_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.WatchlistEntry
	for rows.Next() {
		e, err := scanWatchlist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetWatchlistEntry returns one entry by id, regardless of deleted state
// (the promote flow soft-deletes then re-reads).
func (s *Store) GetWatchlistEntry(ctx context.Context, userID, id int64) (*domain.WatchlistEntry, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+watchlistCols+` FROM watchlist WHERE user_id = ? AND id = ?`, userID, id)
	e, err := scanWatchlist(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// CreateWatchlistEntry inserts a new row and returns it with id populated.
// Ticker is uppercased for stocks/crypto consistency.
func (s *Store) CreateWatchlistEntry(ctx context.Context, e *domain.WatchlistEntry) (*domain.WatchlistEntry, error) {
	e.Ticker = strings.ToUpper(strings.TrimSpace(e.Ticker))
	now := time.Now().UTC()
	if e.AddedAt.IsZero() {
		e.AddedAt = now
	}
	e.UpdatedAt = now

	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO watchlist (
			user_id, ticker, kind, company_name, sector,
			current_price, target_entry_low, target_entry_high,
			thesis_link, note, added_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.UserID, e.Ticker, e.Kind, e.CompanyName, e.Sector,
		e.CurrentPrice, e.TargetEntryLow, e.TargetEntryHigh,
		e.ThesisLink, e.Note, e.AddedAt.Unix(), e.UpdatedAt.Unix())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	return e, nil
}

// UpdateWatchlistEntry overwrites the editable fields. Adds an updated_at touch.
func (s *Store) UpdateWatchlistEntry(ctx context.Context, e *domain.WatchlistEntry) error {
	e.UpdatedAt = time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE watchlist
		   SET company_name = ?, sector = ?, current_price = ?,
		       target_entry_low = ?, target_entry_high = ?,
		       thesis_link = ?, note = ?, updated_at = ?
		 WHERE user_id = ? AND id = ?`,
		e.CompanyName, e.Sector, e.CurrentPrice,
		e.TargetEntryLow, e.TargetEntryHigh,
		e.ThesisLink, e.Note, e.UpdatedAt.Unix(),
		e.UserID, e.ID)
	return err
}

// SoftDeleteWatchlistEntry sets deleted_at = now.
func (s *Store) SoftDeleteWatchlistEntry(ctx context.Context, userID, id int64) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE watchlist SET deleted_at = ?, updated_at = ?
		 WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
		now.Unix(), now.Unix(), userID, id)
	return err
}

// SetPromotedHoldingID records the promote-to-holdings link on the watchlist
// entry and soft-deletes it. Called from the promote handler under a single
// transaction with the holding insert.
func (s *Store) SetPromotedHoldingID(ctx context.Context, tx *sql.Tx, userID, watchlistID, holdingID int64) error {
	now := time.Now().UTC().Unix()
	_, err := tx.ExecContext(ctx, `
		UPDATE watchlist
		   SET promoted_holding_id = ?, deleted_at = ?, updated_at = ?
		 WHERE user_id = ? AND id = ?`,
		holdingID, now, now, userID, watchlistID)
	return err
}

// ----- scan helper -------------------------------------------------------

type scanner interface {
	Scan(dest ...any) error
}

func scanWatchlist(r scanner) (*domain.WatchlistEntry, error) {
	var (
		e          domain.WatchlistEntry
		addedAt    int64
		updatedAt  int64
		deletedAt  sql.NullInt64
		promotedID sql.NullInt64
		// Spec 9c
		s1, s2, r1, r2 sql.NullFloat64
		atrW           sql.NullFloat64
		volAuto, setup sql.NullString
	)
	err := r.Scan(
		&e.ID, &e.UserID, &e.Ticker, &e.Kind, &e.CompanyName, &e.Sector,
		&e.CurrentPrice, &e.TargetEntryLow, &e.TargetEntryHigh,
		&e.ThesisLink, &e.Note, &addedAt, &promotedID, &deletedAt,
		&s1, &s2, &r1, &r2, &atrW, &volAuto, &setup,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}
	e.AddedAt = time.Unix(addedAt, 0).UTC()
	e.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if promotedID.Valid {
		v := promotedID.Int64
		e.PromotedHoldingID = &v
	}
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0).UTC()
		e.DeletedAt = &t
	}
	// Spec 9c
	if s1.Valid {
		v := s1.Float64
		e.Support1 = &v
	}
	if s2.Valid {
		v := s2.Float64
		e.Support2 = &v
	}
	if r1.Valid {
		v := r1.Float64
		e.Resistance1 = &v
	}
	if r2.Valid {
		v := r2.Float64
		e.Resistance2 = &v
	}
	if atrW.Valid {
		v := atrW.Float64
		e.ATRWeekly = &v
	}
	if volAuto.Valid && volAuto.String != "" {
		v := volAuto.String
		e.VolTierAuto = &v
	}
	if setup.Valid && setup.String != "" {
		v := setup.String
		e.SetupType = &v
	}
	return &e, nil
}
