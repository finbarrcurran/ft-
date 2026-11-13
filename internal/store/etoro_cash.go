package store

import "context"

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
