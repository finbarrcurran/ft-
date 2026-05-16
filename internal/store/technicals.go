package store

import "context"

// Spec 9c — store-layer helpers for technicals data that the refresh /
// daily cron writes (not the user). Kept separate from the main holdings
// CRUD to keep concerns clear.

// SetStockTechnicals writes ATR-weekly + vol_tier_auto for a stock
// holding. Caller already computed both. Touches updated_at to keep
// the cache-bust hash consistent.
func (s *Store) SetStockTechnicals(ctx context.Context, id int64, atrWeekly float64, volTierAuto string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings
		    SET atr_weekly = ?, vol_tier_auto = ?, updated_at = strftime('%s','now')
		  WHERE id = ?`,
		atrWeekly, volTierAuto, id)
	return err
}

func (s *Store) SetCryptoTechnicals(ctx context.Context, id int64, atrWeekly float64, volTierAuto string) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings
		    SET atr_weekly = ?, vol_tier_auto = ?, updated_at = strftime('%s','now')
		  WHERE id = ?`,
		atrWeekly, volTierAuto, id)
	return err
}

// MarkTP1Hit advances a stock holding from pre_tp1 → post_tp1 and stamps
// tp1_hit_at. Idempotent (no-op when already past pre_tp1).
func (s *Store) MarkStockTP1Hit(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings
		    SET stage = 'post_tp1', tp1_hit_at = strftime('%s','now'), updated_at = strftime('%s','now')
		  WHERE user_id = ? AND id = ? AND stage = 'pre_tp1'`,
		userID, id)
	return err
}

func (s *Store) MarkStockTP2Hit(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings
		    SET stage = 'runner', tp2_hit_at = strftime('%s','now'), updated_at = strftime('%s','now')
		  WHERE user_id = ? AND id = ? AND stage = 'post_tp1'`,
		userID, id)
	return err
}

func (s *Store) MarkCryptoTP1Hit(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings
		    SET stage = 'post_tp1', tp1_hit_at = strftime('%s','now'), updated_at = strftime('%s','now')
		  WHERE user_id = ? AND id = ? AND stage = 'pre_tp1'`,
		userID, id)
	return err
}

func (s *Store) MarkCryptoTP2Hit(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings
		    SET stage = 'runner', tp2_hit_at = strftime('%s','now'), updated_at = strftime('%s','now')
		  WHERE user_id = ? AND id = ? AND stage = 'post_tp1'`,
		userID, id)
	return err
}

// ----- daily_bars storage (Spec 9c) ------------------------------------

// UpsertDailyBar inserts or replaces one OHLC bar.
func (s *Store) UpsertDailyBar(ctx context.Context, ticker, kind, date string, open, high, low, close, volume float64) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO daily_bars (ticker, kind, date, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ticker, kind, date) DO UPDATE SET
		  open = excluded.open,
		  high = excluded.high,
		  low = excluded.low,
		  close = excluded.close,
		  volume = excluded.volume`,
		ticker, kind, date, open, high, low, close, volume)
	return err
}

// BulkInsertDailyBars wraps many UpsertDailyBar calls in a transaction.
// Pass an iterator that yields (date, open, high, low, close, volume).
func (s *Store) BulkInsertDailyBars(ctx context.Context, ticker, kind string, bars []DailyBarRow) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO daily_bars (ticker, kind, date, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(ticker, kind, date) DO UPDATE SET
		  open = excluded.open,
		  high = excluded.high,
		  low = excluded.low,
		  close = excluded.close,
		  volume = excluded.volume`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.ExecContext(ctx, ticker, kind, b.Date, b.Open, b.High, b.Low, b.Close, b.Volume); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DailyBarRow is the input shape for BulkInsertDailyBars.
type DailyBarRow struct {
	Date                     string // ISO YYYY-MM-DD
	Open, High, Low, Close   float64
	Volume                   float64
}

// GetDailyBars returns OHLC rows for a ticker, ordered ascending by date.
// Empty slice on no data (no error).
func (s *Store) GetDailyBars(ctx context.Context, ticker, kind string) ([]DailyBarRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT date, open, high, low, close, COALESCE(volume, 0)
		  FROM daily_bars WHERE ticker = ? AND kind = ?
		  ORDER BY date ASC`, ticker, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyBarRow
	for rows.Next() {
		var b DailyBarRow
		if err := rows.Scan(&b.Date, &b.Open, &b.High, &b.Low, &b.Close, &b.Volume); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ----- sr_candidates storage (Spec 9c) ---------------------------------

// SRCandidateRow is one row of sr_candidates.
type SRCandidateRow struct {
	Ticker      string
	Kind        string
	LevelType   string // "support" | "resistance"
	Price       float64
	Touches     int
	LastTouchAt string // ISO YYYY-MM-DD
	Score       float64
}

// ReplaceSRCandidates wipes + repopulates S/R candidates for one ticker
// in a single transaction. Called nightly.
func (s *Store) ReplaceSRCandidates(ctx context.Context, ticker, kind string, rows []SRCandidateRow) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sr_candidates WHERE ticker = ? AND kind = ?`, ticker, kind); err != nil {
		return err
	}
	// INSERT OR IGNORE: the (ticker, kind, level_type, price) PK can collide
	// when the 52w-high/low structural levels happen to land on an existing
	// cluster centroid. First-write wins (clusters land first, structural
	// after — clusters generally have more signal so this is the right
	// preference).
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO sr_candidates (ticker, kind, level_type, price, touches, last_touch_at, score, computed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%s','now'))`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.Ticker, r.Kind, r.LevelType, r.Price, r.Touches, r.LastTouchAt, r.Score); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetSRCandidates returns rows for a (ticker, kind) ordered by score desc.
func (s *Store) GetSRCandidates(ctx context.Context, ticker, kind string) ([]SRCandidateRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, kind, level_type, price, touches, last_touch_at, score
		  FROM sr_candidates WHERE ticker = ? AND kind = ?
		  ORDER BY level_type, score DESC`, ticker, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SRCandidateRow
	for rows.Next() {
		var r SRCandidateRow
		if err := rows.Scan(&r.Ticker, &r.Kind, &r.LevelType, &r.Price, &r.Touches, &r.LastTouchAt, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ----- portfolio_value_history (Spec 9c) -------------------------------

// UpsertPortfolioValue records one daily snapshot.
func (s *Store) UpsertPortfolioValue(ctx context.Context, date string, totalUSD, stocksUSD, cryptoUSD float64) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at)
		VALUES (?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(date) DO UPDATE SET
		  total_value_usd = excluded.total_value_usd,
		  stocks_value_usd = excluded.stocks_value_usd,
		  crypto_value_usd = excluded.crypto_value_usd,
		  computed_at = excluded.computed_at`,
		date, totalUSD, stocksUSD, cryptoUSD)
	return err
}

// PortfolioValuePoint is one snapshot row.
type PortfolioValuePoint struct {
	Date     string
	Total    float64
	Stocks   float64
	Crypto   float64
}

// GetPortfolioValueHistory returns rows in ASC order by date.
func (s *Store) GetPortfolioValueHistory(ctx context.Context, limit int) ([]PortfolioValuePoint, error) {
	if limit <= 0 {
		limit = 365
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT date, total_value_usd, stocks_value_usd, crypto_value_usd
		  FROM portfolio_value_history
		  ORDER BY date DESC
		  LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortfolioValuePoint
	for rows.Next() {
		var p PortfolioValuePoint
		if err := rows.Scan(&p.Date, &p.Total, &p.Stocks, &p.Crypto); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to ASC for chart-friendly order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
