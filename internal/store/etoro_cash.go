package store

import (
	"context"
	"time"
)

// EtoroCash is the latest stored value of one eToro cash component (FT-142).
type EtoroCash struct {
	Component    string  `json:"component"`
	AmountNative float64 `json:"amountNative"`
	Currency     string  `json:"currency"`
	USD          float64 `json:"usd"`
	FxToUSD      float64 `json:"fxToUsd"`
	Source       string  `json:"source"`
	AsOf         int64   `json:"asOf"`
}

// SetEtoroCash replaces the latest value of a component.
func (s *Store) SetEtoroCash(ctx context.Context, c EtoroCash) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO etoro_cash (component, amount_native, currency, usd, fx_to_usd, source, as_of)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(component) DO UPDATE SET amount_native = excluded.amount_native,
		currency = excluded.currency, usd = excluded.usd, fx_to_usd = excluded.fx_to_usd, source = excluded.source, as_of = excluded.as_of`,
		c.Component, c.AmountNative, c.Currency, c.USD, c.FxToUSD, c.Source, c.AsOf)
	return err
}

// GetEtoroCash returns the stored components by name.
func (s *Store) GetEtoroCash(ctx context.Context) (map[string]EtoroCash, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT component, amount_native, currency, usd, fx_to_usd, source, as_of FROM etoro_cash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]EtoroCash{}
	for rows.Next() {
		var c EtoroCash
		if err := rows.Scan(&c.Component, &c.AmountNative, &c.Currency, &c.USD, &c.FxToUSD, &c.Source, &c.AsOf); err != nil {
			return nil, err
		}
		out[c.Component] = c
	}
	return out, rows.Err()
}

// LogEtoroCashPull records the outcome of one cash pull (FT-142 follow-up).
func (s *Store) LogEtoroCashPull(ctx context.Context, component, source, status string, usd *float64, errText string) {
	var u, e any
	if usd != nil {
		u = *usd
	}
	if errText != "" {
		if len(errText) > 240 {
			errText = errText[:240]
		}
		e = errText
	}
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO etoro_cash_pulls (pulled_at, component, source, status, usd, error) VALUES (?,?,?,?,?,?)`,
		time.Now().Unix(), component, source, status, u, e)
}

// SetPortfolioCashSource stores the day's cash and where it came from.
func (s *Store) SetPortfolioCashSource(ctx context.Context, date string, cashUSD float64, source string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE portfolio_value_history SET cash_value_usd = ?, cash_source = ? WHERE date = ?`, cashUSD, source, date)
	return err
}

// CashBackfillPlan is what a backfill would do to portfolio_value_history.
type CashBackfillPlan struct {
	HistoryRows, Matched, Unmatched, Changes int
	FirstDate, LastDate                      string
	Updates                                  map[string]float64 // date -> cash USD to write
}

// PlanCashBackfill compares eToro's end-of-day cash (date -> USD) with the stored history rows. Only dates that
// already have a history row are written (a row is never created), and only where the value differs.
func (s *Store) PlanCashBackfill(ctx context.Context, cash map[string]float64) (CashBackfillPlan, error) {
	p := CashBackfillPlan{Updates: map[string]float64{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT date, cash_value_usd FROM portfolio_value_history ORDER BY date`)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var cur *float64
		if err := rows.Scan(&d, &cur); err != nil {
			return p, err
		}
		p.HistoryRows++
		v, ok := cash[d]
		if !ok {
			p.Unmatched++
			continue
		}
		p.Matched++
		if p.FirstDate == "" || d < p.FirstDate {
			p.FirstDate = d
		}
		if d > p.LastDate {
			p.LastDate = d
		}
		if cur == nil || *cur != v {
			p.Changes++
			p.Updates[d] = v
		}
	}
	return p, rows.Err()
}

// ApplyCashBackfill writes a plan in one transaction, labelling the source.
func (s *Store) ApplyCashBackfill(ctx context.Context, p CashBackfillPlan, source string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for d, v := range p.Updates {
		if _, err := tx.ExecContext(ctx, `UPDATE portfolio_value_history SET cash_value_usd = ?, cash_source = ? WHERE date = ?`, v, source, d); err != nil {
			return err
		}
	}
	return tx.Commit()
}
