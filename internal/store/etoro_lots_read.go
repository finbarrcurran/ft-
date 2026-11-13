package store

import (
	"context"
	"time"
)

// EtoroLot is one open own (non-copy) buy lot from the eToro sync (FT-080).
type EtoroLot struct {
	OpenedAt time.Time
	Units    float64
	OpenRate float64 // price per unit in the instrument's own currency (the same basis as stock_holdings.avg_open_price)
	AmountUSD float64
}

// ListEtoroOpenLots returns the open own buy lots for a ticker, oldest first. These, not the 2026-05-17 synthetic
// opening_position rows in `transactions` (keyed by holding id, which no longer lines up), are the source of truth
// for stock tax lots.
func (s *Store) ListEtoroOpenLots(ctx context.Context, ticker string) ([]EtoroLot, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT open_date, units, open_rate, amount_usd
		  FROM etoro_holdings_lots
		 WHERE ticker = ? AND mirror_id = 0 AND is_buy = 1 AND superseded_at IS NULL AND closed_at IS NULL
		 ORDER BY open_date`, ticker)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EtoroLot
	for rows.Next() {
		var l EtoroLot
		var d string
		if err := rows.Scan(&d, &l.Units, &l.OpenRate, &l.AmountUSD); err != nil {
			return nil, err
		}
		l.OpenedAt, _ = time.Parse(time.RFC3339Nano, d)
		out = append(out, l)
	}
	return out, rows.Err()
}

// DerivedFromEtoroLots builds the same DerivedPosition the FIFO path returns, from eToro lots. Unrealised P&L per lot
// is amount * (price / open rate - 1): the same formula the Stocks tab uses (invested * (price / avg - 1)), so the lots
// add up to the Stocks row and it holds for non-USD names (4063.T, RHM.DE, SU.PA) where price and rate are local.
func DerivedFromEtoroLots(lots []EtoroLot, currentPrice, realizedUSD float64) DerivedPosition {
	now := time.Now().UTC()
	out := DerivedPosition{RealizedPnLUSD: realizedUSD, TaxLots: []TaxLot{}}
	var costSum float64
	for _, l := range lots {
		t := TaxLot{OpenedAt: l.OpenedAt, QuantityOpen: l.Units, QuantityOrig: l.Units, PricePerUnit: l.OpenRate}
		if !l.OpenedAt.IsZero() {
			t.HoldingDays = int(now.Sub(l.OpenedAt).Hours() / 24)
		}
		if currentPrice > 0 && l.OpenRate > 0 {
			t.UnrealizedPnLUSD = l.AmountUSD * (currentPrice/l.OpenRate - 1)
		}
		out.Quantity += l.Units
		costSum += l.OpenRate * l.Units
		out.TotalInvestedUSD += l.AmountUSD
		out.TaxLots = append(out.TaxLots, t)
	}
	if out.Quantity > 0 {
		out.CostBasisAvgUSD = costSum / out.Quantity
	}
	return out
}
