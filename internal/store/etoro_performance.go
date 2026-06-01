package store

import (
	"context"
	"time"
)

// SC-17 Phase 1 — eToro statement performance history.
//
// Re-uploading a statement that covers year Y supersedes the prior rows for
// that (user, year) and inserts fresh ones (append/supersede), so the live
// view always reflects the latest upload per year without losing audit trail.

// EtoroAssetRow is one (year, asset_type) breakdown.
type EtoroAssetRow struct {
	AssetType       string  `json:"assetType"`
	RealisedPnLUSD  float64 `json:"realisedPnlUsd"`
	RealisedPnLEUR  float64 `json:"realisedPnlEur"`
	RealisedDiscUSD float64 `json:"realisedDiscUsd"`
	RealisedDiscEUR float64 `json:"realisedDiscEur"`
	RealisedCopyUSD float64 `json:"realisedCopyUsd"`
	RealisedCopyEUR float64 `json:"realisedCopyEur"`
	DividendsUSD    float64 `json:"dividendsUsd"`
	DividendsEUR    float64 `json:"dividendsEur"`
	FeesUSD         float64 `json:"feesUsd"`
	FeesEUR         float64 `json:"feesEur"`
	TradeCount      int     `json:"tradeCount"`
}

// EtoroYearRow is the per-year authoritative summary plus its asset breakdown.
type EtoroYearRow struct {
	Year           int             `json:"year"`
	RangeStart     string          `json:"rangeStart"`
	RangeEnd       string          `json:"rangeEnd"`
	IsYTD          bool            `json:"isYtd"`
	RealisedPnLUSD float64         `json:"realisedPnlUsd"`
	RealisedPnLEUR float64         `json:"realisedPnlEur"`
	DividendsUSD   float64         `json:"dividendsUsd"`
	DividendsEUR   float64         `json:"dividendsEur"`
	FeesUSD        float64         `json:"feesUsd"`
	FeesEUR        float64         `json:"feesEur"`
	InterestUSD    float64         `json:"interestUsd"`
	InterestEUR    float64         `json:"interestEur"`
	NetUSD         float64         `json:"netUsd"`
	NetEUR         float64         `json:"netEur"`
	ComputedPnLUSD float64         `json:"computedPnlUsd"`
	ComputedPnLEUR float64         `json:"computedPnlEur"`
	ReconDeltaUSD  float64         `json:"reconDeltaUsd"`
	ReconDeltaEUR  float64         `json:"reconDeltaEur"`
	SourceFile     string          `json:"sourceFile"`
	ImportedAt     time.Time       `json:"importedAt"`
	Assets         []EtoroAssetRow `json:"assets"`
}

// UpsertEtoroPerformanceYear supersedes prior live rows for (user, year) and
// inserts the new year row + its asset rows, transactionally.
func (s *Store) UpsertEtoroPerformanceYear(ctx context.Context, userID int64, y EtoroYearRow, sourceFile string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()

	if _, err := tx.ExecContext(ctx,
		`UPDATE etoro_performance SET superseded = 1
		  WHERE user_id = ? AND year = ? AND superseded = 0`, userID, y.Year); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE etoro_performance_year SET superseded = 1
		  WHERE user_id = ? AND year = ? AND superseded = 0`, userID, y.Year); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO etoro_performance_year (
			user_id, year, range_start, range_end, is_ytd,
			realised_pnl_usd, realised_pnl_eur, dividends_usd, dividends_eur,
			fees_usd, fees_eur, interest_usd, interest_eur, net_usd, net_eur,
			computed_pnl_usd, computed_pnl_eur, recon_delta_usd, recon_delta_eur,
			superseded, source_file, imported_at
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?,?,?,?, ?,?,?,?, 0,?,?)`,
		userID, y.Year, y.RangeStart, y.RangeEnd, boolToInt(y.IsYTD),
		y.RealisedPnLUSD, y.RealisedPnLEUR, y.DividendsUSD, y.DividendsEUR,
		y.FeesUSD, y.FeesEUR, y.InterestUSD, y.InterestEUR, y.NetUSD, y.NetEUR,
		y.ComputedPnLUSD, y.ComputedPnLEUR, y.ReconDeltaUSD, y.ReconDeltaEUR,
		sourceFile, now,
	); err != nil {
		return err
	}

	for _, a := range y.Assets {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO etoro_performance (
				user_id, year, asset_type,
				realised_pnl_usd, realised_pnl_eur,
				realised_disc_usd, realised_disc_eur,
				realised_copy_usd, realised_copy_eur,
				dividends_usd, dividends_eur, fees_usd, fees_eur,
				trade_count, superseded, source_file, imported_at
			) VALUES (?,?,?, ?,?, ?,?, ?,?, ?,?,?,?, ?,0,?,?)`,
			userID, y.Year, a.AssetType,
			a.RealisedPnLUSD, a.RealisedPnLEUR,
			a.RealisedDiscUSD, a.RealisedDiscEUR,
			a.RealisedCopyUSD, a.RealisedCopyEUR,
			a.DividendsUSD, a.DividendsEUR, a.FeesUSD, a.FeesEUR,
			a.TradeCount, sourceFile, now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListEtoroPerformance returns the live (non-superseded) year rows for a user,
// newest year first, each with its asset breakdown.
func (s *Store) ListEtoroPerformance(ctx context.Context, userID int64) ([]EtoroYearRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT year, range_start, range_end, is_ytd,
		       realised_pnl_usd, realised_pnl_eur, dividends_usd, dividends_eur,
		       fees_usd, fees_eur, interest_usd, interest_eur, net_usd, net_eur,
		       computed_pnl_usd, computed_pnl_eur, recon_delta_usd, recon_delta_eur,
		       source_file, imported_at
		  FROM etoro_performance_year
		 WHERE user_id = ? AND superseded = 0
		 ORDER BY year DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EtoroYearRow{}
	for rows.Next() {
		var y EtoroYearRow
		var isYTD, importedAt int64
		if err := rows.Scan(
			&y.Year, &y.RangeStart, &y.RangeEnd, &isYTD,
			&y.RealisedPnLUSD, &y.RealisedPnLEUR, &y.DividendsUSD, &y.DividendsEUR,
			&y.FeesUSD, &y.FeesEUR, &y.InterestUSD, &y.InterestEUR, &y.NetUSD, &y.NetEUR,
			&y.ComputedPnLUSD, &y.ComputedPnLEUR, &y.ReconDeltaUSD, &y.ReconDeltaEUR,
			&y.SourceFile, &importedAt,
		); err != nil {
			return nil, err
		}
		y.IsYTD = isYTD != 0
		y.ImportedAt = time.Unix(importedAt, 0).UTC()
		y.Assets = []EtoroAssetRow{}
		out = append(out, y)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Attach asset breakdowns.
	for i := range out {
		arows, err := s.DB.QueryContext(ctx, `
			SELECT asset_type,
			       realised_pnl_usd, realised_pnl_eur,
			       realised_disc_usd, realised_disc_eur,
			       realised_copy_usd, realised_copy_eur,
			       dividends_usd, dividends_eur, fees_usd, fees_eur, trade_count
			  FROM etoro_performance
			 WHERE user_id = ? AND year = ? AND superseded = 0
			 ORDER BY id ASC`, userID, out[i].Year)
		if err != nil {
			return nil, err
		}
		for arows.Next() {
			var a EtoroAssetRow
			if err := arows.Scan(
				&a.AssetType,
				&a.RealisedPnLUSD, &a.RealisedPnLEUR,
				&a.RealisedDiscUSD, &a.RealisedDiscEUR,
				&a.RealisedCopyUSD, &a.RealisedCopyEUR,
				&a.DividendsUSD, &a.DividendsEUR, &a.FeesUSD, &a.FeesEUR, &a.TradeCount,
			); err != nil {
				arows.Close()
				return nil, err
			}
			out[i].Assets = append(out[i].Assets, a)
		}
		arows.Close()
		if err := arows.Err(); err != nil {
			return nil, err
		}
	}

	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
