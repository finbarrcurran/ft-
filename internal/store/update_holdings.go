package store

import (
	"context"
)

// UpdateStockMarketData writes market-derived fields back to a stock_holdings
// row. RSI / MA50 / MA200 / dailyChange may each be nil (leave unchanged).
// currentPrice is always written.
func (s *Store) UpdateStockMarketData(
	ctx context.Context,
	userID int64,
	ticker string,
	currentPrice float64,
	rsi14, ma50, ma200, dailyChangePct *float64,
) error {
	// Build a partial UPDATE — only set fields we actually received.
	q := `UPDATE stock_holdings SET current_price = ?, updated_at = strftime('%s','now')`
	args := []any{currentPrice}
	if rsi14 != nil {
		q += `, rsi14 = ?`
		args = append(args, *rsi14)
	}
	if ma50 != nil {
		q += `, ma50 = ?`
		args = append(args, *ma50)
	}
	if ma200 != nil {
		q += `, ma200 = ?`
		args = append(args, *ma200)
	}
	// Recompute golden_cross when both MAs are present.
	if ma50 != nil && ma200 != nil {
		gc := 0
		if *ma50 > *ma200 {
			gc = 1
		}
		q += `, golden_cross = ?`
		args = append(args, gc)
	}
	if dailyChangePct != nil {
		q += `, daily_change_pct = ?`
		args = append(args, *dailyChangePct)
	}
	q += ` WHERE user_id = ? AND ticker = ?`
	args = append(args, userID, ticker)

	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// UpdateCryptoMarketData writes prices + EUR↔USD-derived values + momentum
// fields back to a crypto_holdings row, keyed by symbol.
func (s *Store) UpdateCryptoMarketData(
	ctx context.Context,
	userID int64,
	symbol string,
	priceUSD, priceEUR float64,
	change7dPct, change30dPct, dailyChangePct *float64,
) error {
	// Read current quantities so we can recompute values.
	var qtyHeld, qtyStaked float64
	if err := s.DB.QueryRowContext(ctx,
		`SELECT quantity_held, quantity_staked FROM crypto_holdings
		 WHERE user_id = ? AND symbol = ?`, userID, symbol,
	).Scan(&qtyHeld, &qtyStaked); err != nil {
		return err
	}
	total := qtyHeld + qtyStaked
	valueUSD := priceUSD * total
	valueEUR := priceEUR * total

	q := `UPDATE crypto_holdings
	      SET current_price_usd = ?, current_price_eur = ?,
	          current_value_usd = ?, current_value_eur = ?,
	          updated_at = strftime('%s','now')`
	args := []any{priceUSD, priceEUR, valueUSD, valueEUR}
	if change7dPct != nil {
		q += `, change_7d_pct = ?`
		args = append(args, *change7dPct)
	}
	if change30dPct != nil {
		q += `, change_30d_pct = ?`
		args = append(args, *change30dPct)
	}
	if dailyChangePct != nil {
		q += `, daily_change_pct = ?`
		args = append(args, *dailyChangePct)
	}
	q += ` WHERE user_id = ? AND symbol = ?`
	args = append(args, userID, symbol)

	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}
