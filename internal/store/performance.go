package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrAlreadyExists is returned by InsertClosedTrade when the
// source_audit_close_id UNIQUE constraint trips (idempotency on
// derivation re-runs).
var ErrAlreadyExists = errors.New("store: closed_trade already exists for this source audit row")

// ClosedTradeRow mirrors the closed_trades schema (store-side; the
// performance package has a richer struct with json tags).
type ClosedTradeRow struct {
	Ticker                string
	Kind                  string
	HoldingID             int64
	OpenedAt              time.Time
	SetupType             string
	RegimeEffective       string
	JordiScore            *int
	CowenScore            *int
	PercocoScore          *int
	ATRWeeklyAtEntry      float64
	VolTierAtEntry        string
	Support1AtEntry       float64
	Resistance1AtEntry    float64
	Resistance2AtEntry    float64
	EntryPrice            float64
	SLAtEntry             float64
	TP1AtEntry            float64
	TP2AtEntry            float64
	RMultipleTP1Planned   float64
	RMultipleTP2Planned   float64
	PositionSizeUnits     float64
	PositionSizeUSD       float64
	PerTradeRiskPct       float64
	PerTradeRiskUSD       float64
	PortfolioValueAtEntry float64
	ClosedAt              time.Time
	ExitReason            string
	ExitPriceAvg          float64
	HoldingPeriodDays     int
	RealizedPnLUSD        float64
	RealizedPnLPct        float64
	RealizedRMultiple     float64
	SourceAuditOpenID     int64
	SourceAuditCloseID    int64
	DerivedAt             time.Time
}

// InsertClosedTrade appends one row. UNIQUE on source_audit_close_id; if
// the row already exists, returns ErrAlreadyExists (caller treats as a
// successful idempotent re-run).
func (s *Store) InsertClosedTrade(ctx context.Context, r ClosedTradeRow) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO closed_trades (
			ticker, kind, holding_id, opened_at,
			setup_type, regime_effective, jordi_score, cowen_score, percoco_score,
			atr_weekly_at_entry, vol_tier_at_entry,
			support_1_at_entry, resistance_1_at_entry, resistance_2_at_entry,
			entry_price, sl_at_entry, tp1_at_entry, tp2_at_entry,
			r_multiple_tp1_planned, r_multiple_tp2_planned,
			position_size_units, position_size_usd, per_trade_risk_pct, per_trade_risk_usd,
			portfolio_value_at_entry,
			closed_at, exit_reason, exit_price_avg, holding_period_days,
			realized_pnl_usd, realized_pnl_pct, realized_r_multiple,
			source_audit_open_id, source_audit_close_id, derived_at
		) VALUES (?,?,?,?, ?,?,?,?,?, ?,?, ?,?,?, ?,?,?,?, ?,?, ?,?,?,?, ?, ?,?,?,?, ?,?,?, ?,?,?)`,
		strings.ToUpper(r.Ticker), r.Kind, r.HoldingID, r.OpenedAt.Unix(),
		nullIfBlank(r.SetupType), nullIfBlank(r.RegimeEffective),
		intPtrToNull(r.JordiScore), intPtrToNull(r.CowenScore), intPtrToNull(r.PercocoScore),
		r.ATRWeeklyAtEntry, nullIfBlank(r.VolTierAtEntry),
		r.Support1AtEntry, r.Resistance1AtEntry, r.Resistance2AtEntry,
		r.EntryPrice, r.SLAtEntry, r.TP1AtEntry, r.TP2AtEntry,
		r.RMultipleTP1Planned, r.RMultipleTP2Planned,
		r.PositionSizeUnits, r.PositionSizeUSD, r.PerTradeRiskPct, r.PerTradeRiskUSD,
		r.PortfolioValueAtEntry,
		r.ClosedAt.Unix(), r.ExitReason, r.ExitPriceAvg, r.HoldingPeriodDays,
		r.RealizedPnLUSD, r.RealizedPnLPct, r.RealizedRMultiple,
		r.SourceAuditOpenID, r.SourceAuditCloseID, r.DerivedAt.Unix(),
	)
	if err != nil {
		// SQLite UNIQUE constraint violation comes back as a generic error
		// with the constraint name in the message. Best-effort detection.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") &&
			strings.Contains(err.Error(), "source_audit_close_id") {
			return ErrAlreadyExists
		}
		return err
	}
	return nil
}

// ListClosedTrades returns all closed_trades for the user, newest first.
// Skips superseded rows by default.
func (s *Store) ListClosedTrades(ctx context.Context, limit int) ([]*ClosedTradeRow, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, ticker, kind, holding_id, opened_at,
		       COALESCE(setup_type,''), COALESCE(regime_effective,''),
		       jordi_score, cowen_score, percoco_score,
		       atr_weekly_at_entry, COALESCE(vol_tier_at_entry,''),
		       support_1_at_entry, resistance_1_at_entry, resistance_2_at_entry,
		       entry_price, sl_at_entry, tp1_at_entry, tp2_at_entry,
		       r_multiple_tp1_planned, r_multiple_tp2_planned,
		       position_size_units, position_size_usd, per_trade_risk_pct, per_trade_risk_usd,
		       portfolio_value_at_entry,
		       closed_at, exit_reason, exit_price_avg, holding_period_days,
		       realized_pnl_usd, realized_pnl_pct, realized_r_multiple,
		       source_audit_open_id, source_audit_close_id, derived_at
		  FROM closed_trades
		 WHERE superseded_at IS NULL
		 ORDER BY closed_at DESC, id DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*ClosedTradeRow{}
	for rows.Next() {
		var (
			r                              ClosedTradeRow
			openedAt, closedAt, derivedAt  int64
			id                             int64
			jordi, cowen, percoco          sql.NullInt64
		)
		_ = id
		if err := rows.Scan(
			&id, &r.Ticker, &r.Kind, &r.HoldingID, &openedAt,
			&r.SetupType, &r.RegimeEffective,
			&jordi, &cowen, &percoco,
			&r.ATRWeeklyAtEntry, &r.VolTierAtEntry,
			&r.Support1AtEntry, &r.Resistance1AtEntry, &r.Resistance2AtEntry,
			&r.EntryPrice, &r.SLAtEntry, &r.TP1AtEntry, &r.TP2AtEntry,
			&r.RMultipleTP1Planned, &r.RMultipleTP2Planned,
			&r.PositionSizeUnits, &r.PositionSizeUSD, &r.PerTradeRiskPct, &r.PerTradeRiskUSD,
			&r.PortfolioValueAtEntry,
			&closedAt, &r.ExitReason, &r.ExitPriceAvg, &r.HoldingPeriodDays,
			&r.RealizedPnLUSD, &r.RealizedPnLPct, &r.RealizedRMultiple,
			&r.SourceAuditOpenID, &r.SourceAuditCloseID, &derivedAt,
		); err != nil {
			return nil, err
		}
		r.OpenedAt = time.Unix(openedAt, 0).UTC()
		r.ClosedAt = time.Unix(closedAt, 0).UTC()
		r.DerivedAt = time.Unix(derivedAt, 0).UTC()
		if jordi.Valid {
			v := int(jordi.Int64)
			r.JordiScore = &v
		}
		if cowen.Valid {
			v := int(cowen.Int64)
			r.CowenScore = &v
		}
		if percoco.Valid {
			v := int(percoco.Int64)
			r.PercocoScore = &v
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// UpsertPerformanceSnapshot writes / replaces one (date, cohort_key, window) row.
type PerformanceSnapshotRow struct {
	Date                 string
	CohortKey            string
	Window               string
	TradeCount           int
	WinCount             int
	WinRate              float64
	AvgWinnerR           float64
	AvgLoserR            float64
	ExpectancyR          float64
	TotalRealizedPnLUSD  float64
	AvgHoldingPeriodDays float64
	MaxDrawdownPct       float64
}

func (s *Store) UpsertPerformanceSnapshot(ctx context.Context, r PerformanceSnapshotRow) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO performance_snapshots
		  (snapshot_date, cohort_key, window, trade_count, win_count, win_rate,
		   avg_winner_r, avg_loser_r, expectancy_r, total_realized_pnl_usd,
		   avg_holding_period_days, max_drawdown_pct, computed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,strftime('%s','now'))
		ON CONFLICT(snapshot_date, cohort_key, window) DO UPDATE SET
		  trade_count             = excluded.trade_count,
		  win_count               = excluded.win_count,
		  win_rate                = excluded.win_rate,
		  avg_winner_r            = excluded.avg_winner_r,
		  avg_loser_r             = excluded.avg_loser_r,
		  expectancy_r            = excluded.expectancy_r,
		  total_realized_pnl_usd  = excluded.total_realized_pnl_usd,
		  avg_holding_period_days = excluded.avg_holding_period_days,
		  max_drawdown_pct        = excluded.max_drawdown_pct,
		  computed_at             = excluded.computed_at`,
		r.Date, r.CohortKey, r.Window, r.TradeCount, r.WinCount, r.WinRate,
		r.AvgWinnerR, r.AvgLoserR, r.ExpectancyR, r.TotalRealizedPnLUSD,
		r.AvgHoldingPeriodDays, r.MaxDrawdownPct)
	return err
}

// GetLatestPerformanceSnapshots returns rows for the latest snapshot_date
// across all cohorts (one batch). Used by the Performance tab.
func (s *Store) GetLatestPerformanceSnapshots(ctx context.Context) ([]PerformanceSnapshotRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT snapshot_date, cohort_key, window, trade_count, win_count, win_rate,
		       avg_winner_r, avg_loser_r, expectancy_r, total_realized_pnl_usd,
		       COALESCE(avg_holding_period_days, 0), COALESCE(max_drawdown_pct, 0)
		  FROM performance_snapshots
		 WHERE snapshot_date = (SELECT MAX(snapshot_date) FROM performance_snapshots)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PerformanceSnapshotRow{}
	for rows.Next() {
		var r PerformanceSnapshotRow
		if err := rows.Scan(&r.Date, &r.CohortKey, &r.Window, &r.TradeCount, &r.WinCount, &r.WinRate,
			&r.AvgWinnerR, &r.AvgLoserR, &r.ExpectancyR, &r.TotalRealizedPnLUSD,
			&r.AvgHoldingPeriodDays, &r.MaxDrawdownPct); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// nullIfBlank / intPtrToNull: local conversion helpers so this file
// doesn't reach into other helpers and pin imports.
func nullIfBlank(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func intPtrToNull(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
