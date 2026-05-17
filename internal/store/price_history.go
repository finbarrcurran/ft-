package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// price_history feeds Spec 3 D8 sparklines. It's a sliding 30-day window per
// (ticker, kind) populated by the daily 04:00 UTC cron. Rendered server-side
// to inline SVG via the sparkline package.
//
// PRIMARY KEY (ticker, kind, date) means daily upserts are idempotent — re-
// running the same backfill job is safe.

// PricePoint is one row of price_history. `Date` is ISO 'YYYY-MM-DD'.
type PricePoint struct {
	Date  string
	Close float64
}

// InsertPriceHistoryBatch upserts (ticker, kind, date, close) rows. The unique
// constraint on (ticker, kind, date) means this is idempotent.
//
// `kind` must be "stock" or "crypto"; callers enforce this.
func (s *Store) InsertPriceHistoryBatch(ctx context.Context, ticker, kind string, points []PricePoint) error {
	if len(points) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO price_history (ticker, kind, date, close)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(ticker, kind, date) DO UPDATE SET close = excluded.close
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range points {
		if p.Close <= 0 {
			continue // Yahoo sometimes returns 0/NaN on half-days; skip rather than poison the chart.
		}
		if _, err := stmt.ExecContext(ctx, ticker, kind, p.Date, p.Close); err != nil {
			return fmt.Errorf("insert %s %s %s: %w", ticker, kind, p.Date, err)
		}
	}
	return tx.Commit()
}

// GetSparklineCloses returns up to `days` most-recent closes for a ticker,
// chronologically ascending (oldest first). Used by the sparkline renderer.
func (s *Store) GetSparklineCloses(ctx context.Context, ticker, kind string, days int) ([]float64, error) {
	if days <= 0 {
		days = 30
	}
	// SELECT DESC then reverse in Go — keeps the LIMIT clause cheap.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT close FROM price_history
		WHERE ticker = ? AND kind = ?
		ORDER BY date DESC
		LIMIT ?`, ticker, kind, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rev []float64
	for rows.Next() {
		var c float64
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		rev = append(rev, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse in place: caller wants chronological ascending.
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

// GetAllSparklineCloses batch-fetches series for many tickers at once. Returns
// `map[ticker][]close` (chronological ascending). Tickers with no rows are
// absent from the map — caller treats that as "no data, render dash".
func (s *Store) GetAllSparklineCloses(ctx context.Context, kind string, tickers []string, days int) (map[string][]float64, error) {
	out := map[string][]float64{}
	if len(tickers) == 0 {
		return out, nil
	}
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days-1).Format("2006-01-02")

	// Build IN (?,?,...) placeholders.
	args := make([]any, 0, len(tickers)+2)
	args = append(args, kind, cutoff)
	placeholders := make([]string, len(tickers))
	for i, t := range tickers {
		placeholders[i] = "?"
		args = append(args, t)
	}

	q := fmt.Sprintf(`
		SELECT ticker, close FROM price_history
		WHERE kind = ? AND date >= ? AND ticker IN (%s)
		ORDER BY ticker, date ASC`, strings.Join(placeholders, ","))

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var t string
		var c float64
		if err := rows.Scan(&t, &c); err != nil {
			return nil, err
		}
		out[t] = append(out[t], c)
	}
	return out, rows.Err()
}

// PrunePriceHistory deletes rows older than `days` days. Run at end of the
// daily cron so the table stays bounded (~36 holdings × 30 days = ~1100 rows).
func (s *Store) PrunePriceHistory(ctx context.Context, days int) (int64, error) {
	if days <= 0 {
		days = 35 // small buffer beyond the sparkline window
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM price_history WHERE date < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SetStockVolatility12m updates volatility_12m_pct on a stock_holdings row by
// ticker. Spec 12 D5e. Called by the daily cron after the closes-fetch.
func (s *Store) SetStockVolatility12m(ctx context.Context, userID int64, ticker string, pct float64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings SET volatility_12m_pct = ?
		 WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		pct, userID, ticker)
	return err
}

// SetCryptoVolatility12m — crypto equivalent, matched by symbol.
func (s *Store) SetCryptoVolatility12m(ctx context.Context, userID int64, symbol string, pct float64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings SET volatility_12m_pct = ?
		 WHERE user_id = ? AND symbol = ? AND deleted_at IS NULL`,
		pct, userID, symbol)
	return err
}

// SetStockForecast writes Yahoo's Bear/Base/Bull consensus targets onto
// stock_holdings by ticker. Spec 12 D4a. nil values are skipped — pass
// the helper-friendly fp() wrapper if a target is unknown.
func (s *Store) SetStockForecast(ctx context.Context, userID int64, ticker string, low, mean, high *float64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings SET
		   forecast_low = ?, forecast_mean = ?, forecast_high = ?,
		   forecast_fetched_at = strftime('%s','now')
		 WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		fpVal(low), fpVal(mean), fpVal(high), userID, ticker)
	return err
}

// SetWatchlistForecast — same shape for watchlist entries.
func (s *Store) SetWatchlistForecast(ctx context.Context, userID int64, ticker string, low, mean, high *float64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE watchlist SET
		   forecast_low = ?, forecast_mean = ?, forecast_high = ?,
		   forecast_fetched_at = strftime('%s','now')
		 WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		fpVal(low), fpVal(mean), fpVal(high), userID, ticker)
	return err
}

// fpVal is a small NULL-friendly *float64 → any converter used by the
// forecast setters above.
func fpVal(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// SetStockBeta writes the beta value for a stock by ticker. Beta seeds the
// Spec 3 D11 suggested SL/TP columns. (Spec 3 D2/D11.)
func (s *Store) SetStockBeta(ctx context.Context, userID int64, ticker string, beta float64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET beta = ?, updated_at = ?
		 WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		beta, time.Now().Unix(), userID, ticker)
	return err
}

// SetCalendarDates writes earnings_date + ex_dividend_date for a stock by
// ticker. Pass empty string to clear a field. (Spec 3 D10.)
func (s *Store) SetCalendarDates(ctx context.Context, userID int64, ticker, earningsDate, exDivDate string) error {
	var ed, xd any
	if earningsDate != "" {
		ed = earningsDate
	}
	if exDivDate != "" {
		xd = exDivDate
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET earnings_date = ?, ex_dividend_date = ?, updated_at = ?
		 WHERE user_id = ? AND ticker = ? AND deleted_at IS NULL`,
		ed, xd, time.Now().Unix(), userID, ticker)
	return err
}
