package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"strconv"
	"strings"
	"time"
)

const watchlistCols = `id, user_id, ticker, kind, company_name, sector,
        current_price, target_entry_low, target_entry_high,
        thesis_link, note, added_at, promoted_holding_id, deleted_at,
        support_1, support_2, resistance_1, resistance_2,
        atr_weekly, vol_tier_auto, setup_type,
        forecast_low, forecast_mean, forecast_high,
        forecast_median, forecast_analyst_count, forecast_source, forecast_fetched_at,
        sector_universe_id,
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
			thesis_link, note, added_at, updated_at,
			sector_universe_id
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.UserID, e.Ticker, e.Kind, e.CompanyName, e.Sector,
		e.CurrentPrice, e.TargetEntryLow, e.TargetEntryHigh,
		e.ThesisLink, e.Note, e.AddedAt.Unix(), e.UpdatedAt.Unix(),
		nullInt(e.SectorUniverseID))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	e.ID = id
	return e, nil
}

// DemoteHoldingToWatchlist creates a watchlist entry from a stock holding
// being removed by an import (v1.7.6). Carries forward identity and the
// pieces of context that are expensive to re-enter manually: ticker, name,
// sector, sector_universe_id, current_price, thesis_link. Sets a
// descriptive note so the watchlist row's origin is obvious later.
//
// Idempotent: if an ACTIVE (non-soft-deleted) watchlist row already exists
// for this user+ticker, nothing happens and (nil, nil) is returned. This
// means re-importing the same "missing ticker" twice won't pile up
// duplicates.
//
// Used from handleImportApply before the slam-replace deletes the holding.
// The same logic applies to crypto holdings (kind="crypto").
func (s *Store) DemoteHoldingToWatchlist(ctx context.Context, userID int64, kind, ticker, companyName string, sector *string, sectorUniverseID *int64, currentPrice *float64, thesisLink *string, investedUSD float64) (*domain.WatchlistEntry, error) {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return nil, nil
	}
	// Skip if an active watchlist row already exists for this ticker.
	var existing int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM watchlist
		  WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		userID, ticker).Scan(&existing); err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, nil // already watched — don't duplicate
	}
	now := time.Now().UTC()
	noteStr := "Auto-moved from holdings on " + now.Format("2006-01-02") +
		" — removed via import"
	if investedUSD > 0 {
		noteStr += " (was invested $" + ftoa2(investedUSD) + ")"
	}
	e := &domain.WatchlistEntry{
		UserID:           userID,
		Ticker:           ticker,
		Kind:             kind,
		CompanyName:      ptrStr(companyName),
		Sector:           sector,
		SectorUniverseID: sectorUniverseID,
		CurrentPrice:     currentPrice,
		ThesisLink:       thesisLink,
		Note:             &noteStr,
		AddedAt:          now,
	}
	return s.CreateWatchlistEntry(ctx, e)
}

// ftoa2 formats a float to 2 decimals without trailing zeros.
func ftoa2(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	return s
}

func ptrStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
		// Spec 12 D4a
		fLow, fMean, fHigh sql.NullFloat64
		fFetched           sql.NullInt64
		// SC-31
		fMedian       sql.NullFloat64
		fAnalystCount sql.NullInt64
		fSource       sql.NullString
		// Spec 9f D1
		sectorUniverseID sql.NullInt64
	)
	err := r.Scan(
		&e.ID, &e.UserID, &e.Ticker, &e.Kind, &e.CompanyName, &e.Sector,
		&e.CurrentPrice, &e.TargetEntryLow, &e.TargetEntryHigh,
		&e.ThesisLink, &e.Note, &addedAt, &promotedID, &deletedAt,
		&s1, &s2, &r1, &r2, &atrW, &volAuto, &setup,
		&fLow, &fMean, &fHigh,
		&fMedian, &fAnalystCount, &fSource, &fFetched,
		&sectorUniverseID,
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
	// Spec 12 D4a
	if fLow.Valid {
		v := fLow.Float64
		e.ForecastLow = &v
	}
	if fMean.Valid {
		v := fMean.Float64
		e.ForecastMean = &v
	}
	if fHigh.Valid {
		v := fHigh.Float64
		e.ForecastHigh = &v
	}
	if fMedian.Valid {
		v := fMedian.Float64
		e.ForecastMedian = &v
	}
	if fAnalystCount.Valid {
		n := int(fAnalystCount.Int64)
		e.ForecastAnalystCount = &n
	}
	if fSource.Valid && fSource.String != "" {
		e.ForecastSource = fSource.String
	} else {
		e.ForecastSource = "yahoo"
	}
	if fFetched.Valid {
		t := time.Unix(fFetched.Int64, 0).UTC()
		e.ForecastFetchedAt = &t
	}
	// Spec 9f D1
	if sectorUniverseID.Valid {
		v := sectorUniverseID.Int64
		e.SectorUniverseID = &v
	}
	return &e, nil
}
