// Spec 9f — Sector Rotation Tracker store helpers.
//
// Tables (migration 0019):
//   sector_universe         — 34-row locked taxonomy (Philosophy v1.1 §4)
//   sector_snapshots        — daily ETF close per sector + benchmarks
//   user_sector_ordering    — user's drag-reordered display
//   sector_rotation_digests — Friday weekly digest log
//
// stock_holdings + watchlist gained sector_universe_id columns; the
// reads of those tables already pull every column via SELECT * patterns,
// so the new field surfaces automatically once domain types are extended.

package store

import (
	"context"
	"database/sql"
	"time"
)

// SectorUniverseRow mirrors one row of the locked 34-row taxonomy.
type SectorUniverseRow struct {
	ID                  int64   `json:"id"`
	Code                string  `json:"code"`
	DisplayName         string  `json:"displayName"`
	ParentGICS          string  `json:"parentGics"`
	JordiStage          *int    `json:"jordiStage,omitempty"`
	RotationThesis      *string `json:"rotationThesis,omitempty"`
	ETFTickerPrimary    string  `json:"etfTickerPrimary"`
	ETFTickerSecondary  *string `json:"etfTickerSecondary,omitempty"`
	Active              bool    `json:"active"`
	DisplayOrderAuto    int     `json:"displayOrderAuto"`
	DisplayOrderUser    *int    `json:"displayOrderUser,omitempty"`
}

// ListSectorUniverse returns every row, joined with user_sector_ordering so
// the caller can decide which ordering to use. Sorted by display_order_auto
// for stable iteration order regardless of user overrides.
func (s *Store) ListSectorUniverse(ctx context.Context) ([]SectorUniverseRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT su.id, su.code, su.display_name, su.parent_gics,
		       su.jordi_stage, su.rotation_thesis,
		       su.etf_ticker_primary, su.etf_ticker_secondary,
		       su.active, su.display_order_auto,
		       uso.display_order_user
		  FROM sector_universe su
		  LEFT JOIN user_sector_ordering uso ON uso.sector_universe_id = su.id
		 WHERE su.active = 1
		 ORDER BY su.display_order_auto`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SectorUniverseRow{}
	for rows.Next() {
		var r SectorUniverseRow
		var thesis, etf2 sql.NullString
		var stage, userOrder sql.NullInt64
		var active int
		if err := rows.Scan(&r.ID, &r.Code, &r.DisplayName, &r.ParentGICS,
			&stage, &thesis,
			&r.ETFTickerPrimary, &etf2,
			&active, &r.DisplayOrderAuto,
			&userOrder); err != nil {
			return nil, err
		}
		r.Active = active != 0
		if stage.Valid {
			v := int(stage.Int64)
			r.JordiStage = &v
		}
		if thesis.Valid {
			r.RotationThesis = &thesis.String
		}
		if etf2.Valid {
			r.ETFTickerSecondary = &etf2.String
		}
		if userOrder.Valid {
			v := int(userOrder.Int64)
			r.DisplayOrderUser = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertSectorSnapshot is idempotent — UNIQUE(sector_universe_id, snapshot_date)
// makes re-runs of the ingestion safe. Returns nil on conflict.
func (s *Store) InsertSectorSnapshot(ctx context.Context, sectorID int64, date string,
	closePrimary float64, closeSecondary *float64,
	spy float64, vwrl *float64, source string,
) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sector_snapshots
		  (sector_universe_id, snapshot_date, close_primary, close_secondary,
		   benchmark_spy_close, benchmark_vwrl_close, source)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(sector_universe_id, snapshot_date) DO NOTHING`,
		sectorID, date, closePrimary, fpVal(closeSecondary),
		spy, fpVal(vwrl), source)
	return err
}

// ListRecentSnapshots returns the snapshots for one sector within the last
// `daysBack` calendar days, oldest first. Used by metrics computation.
func (s *Store) ListRecentSnapshots(ctx context.Context, sectorID int64, daysBack int) ([]SectorSnapshot, error) {
	if daysBack <= 0 {
		daysBack = 400
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -daysBack).Format("2006-01-02")
	rows, err := s.DB.QueryContext(ctx, `
		SELECT snapshot_date, close_primary, benchmark_spy_close
		  FROM sector_snapshots
		 WHERE sector_universe_id = ? AND snapshot_date >= ?
		 ORDER BY snapshot_date ASC`, sectorID, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SectorSnapshot{}
	for rows.Next() {
		var sn SectorSnapshot
		sn.SectorID = sectorID
		if err := rows.Scan(&sn.Date, &sn.ClosePrimary, &sn.BenchmarkSPY); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// SectorSnapshot is the trimmed shape needed by metrics computation.
type SectorSnapshot struct {
	SectorID     int64
	Date         string
	ClosePrimary float64
	BenchmarkSPY float64
}

// SetUserSectorOrdering replaces the user's manual order. Caller passes
// a slice of (sectorID, position) pairs. Atomic via a single transaction.
func (s *Store) SetUserSectorOrdering(ctx context.Context, pairs []struct {
	SectorID int64
	Position int
}) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_sector_ordering`); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_sector_ordering (sector_universe_id, display_order_user)
		VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range pairs {
		if _, err := stmt.ExecContext(ctx, p.SectorID, p.Position); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearUserSectorOrdering wipes the user's manual order — "reset to
// auto-ranking" button in 9f D5.
func (s *Store) ClearUserSectorOrdering(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_sector_ordering`)
	return err
}

// HoldingsCountBySector returns sector_universe_id → count of active
// stock_holdings rows tagged to that sector. Used by the rotation table's
// Holdings column.
func (s *Store) HoldingsCountBySector(ctx context.Context, userID int64) (map[int64]int, error) {
	out := map[int64]int{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT sector_universe_id, COUNT(*) FROM stock_holdings
		 WHERE user_id = ? AND deleted_at IS NULL AND sector_universe_id IS NOT NULL
		 GROUP BY sector_universe_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid int64
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			return nil, err
		}
		out[sid] = n
	}
	return out, rows.Err()
}

// WatchlistCountBySector — analogous count from watchlist (active rows
// only). Summed into the rotation table's Holdings column alongside
// holdings count.
func (s *Store) WatchlistCountBySector(ctx context.Context, userID int64) (map[int64]int, error) {
	out := map[int64]int{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT sector_universe_id, COUNT(*) FROM watchlist
		 WHERE user_id = ? AND deleted_at IS NULL AND sector_universe_id IS NOT NULL
		 GROUP BY sector_universe_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid int64
		var n int
		if err := rows.Scan(&sid, &n); err != nil {
			return nil, err
		}
		out[sid] = n
	}
	return out, rows.Err()
}

// InsertSectorRotationDigest stores a Friday digest. UNIQUE(week_ending)
// makes re-runs idempotent.
func (s *Store) InsertSectorRotationDigest(ctx context.Context, weekEnding, markdown string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sector_rotation_digests (week_ending, markdown)
		VALUES (?, ?)
		ON CONFLICT(week_ending) DO UPDATE SET markdown = excluded.markdown`,
		weekEnding, markdown)
	return err
}

// ListSectorRotationDigests returns the latest N digests, newest first.
func (s *Store) ListSectorRotationDigests(ctx context.Context, limit int) ([]SectorRotationDigest, error) {
	if limit <= 0 || limit > 52 {
		limit = 8
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, week_ending, markdown, created_at
		  FROM sector_rotation_digests
		 ORDER BY week_ending DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SectorRotationDigest{}
	for rows.Next() {
		var d SectorRotationDigest
		var createdAt int64
		if err := rows.Scan(&d.ID, &d.WeekEnding, &d.Markdown, &createdAt); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// SectorRotationDigest mirrors one row of sector_rotation_digests.
type SectorRotationDigest struct {
	ID         int64     `json:"id"`
	WeekEnding string    `json:"weekEnding"`
	Markdown   string    `json:"markdown"`
	CreatedAt  time.Time `json:"createdAt"`
}

// UpdateStockHoldingSector lets the Edit modal change just the sector tag
// without writing a full UPDATE through UpdateStockHolding. Cheaper for
// the common single-field tag-correction flow.
func (s *Store) UpdateStockHoldingSector(ctx context.Context, userID, holdingID int64, sectorID *int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET sector_universe_id = ?,
		       updated_at = strftime('%s','now')
		 WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
		nullInt(sectorID), userID, holdingID)
	return err
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
