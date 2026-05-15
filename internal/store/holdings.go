package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"time"
)

// --- Stock holdings --------------------------------------------------------

func (s *Store) ListStockHoldings(ctx context.Context, userID int64) ([]*domain.StockHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, name, ticker, category, sector,
		        invested_usd, avg_open_price, current_price,
		        rsi14, ma50, ma200, golden_cross,
		        support, resistance, analyst_target,
		        proposed_entry, technical_setup, analyst_rr_view,
		        stop_loss, take_profit, strategy_note,
		        daily_change_pct,
		        updated_at
		 FROM stock_holdings WHERE user_id = ?
		 ORDER BY invested_usd DESC, id`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.StockHolding
	for rows.Next() {
		h, err := scanStock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) GetStockHolding(ctx context.Context, userID, id int64) (*domain.StockHolding, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, name, ticker, category, sector,
		        invested_usd, avg_open_price, current_price,
		        rsi14, ma50, ma200, golden_cross,
		        support, resistance, analyst_target,
		        proposed_entry, technical_setup, analyst_rr_view,
		        stop_loss, take_profit, strategy_note,
		        daily_change_pct,
		        updated_at
		 FROM stock_holdings WHERE user_id = ? AND id = ?`, userID, id,
	)
	h, err := scanStock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

func (s *Store) InsertStockHolding(ctx context.Context, h *domain.StockHolding) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO stock_holdings (
		    user_id, name, ticker, category, sector,
		    invested_usd, avg_open_price, current_price,
		    rsi14, ma50, ma200, golden_cross,
		    support, resistance, analyst_target,
		    proposed_entry, technical_setup, analyst_rr_view,
		    stop_loss, take_profit, strategy_note,
		    updated_at
		 ) VALUES (?,?,?,?,?, ?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?,?, strftime('%s','now'))`,
		h.UserID, h.Name, strPtrToNull(h.Ticker), strPtrToNull(h.Category), strPtrToNull(h.Sector),
		h.InvestedUSD, fp(h.AvgOpenPrice), fp(h.CurrentPrice),
		fp(h.RSI14), fp(h.MA50), fp(h.MA200), bp(h.GoldenCross),
		fp(h.Support), fp(h.Resistance), fp(h.AnalystTarget),
		fp(h.ProposedEntry),
		stringFromPtrOrEmpty(h.TechnicalSetup),
		stringFromPtrOrEmpty(h.AnalystRRView),
		fp(h.StopLoss), fp(h.TakeProfit), h.StrategyNote,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteAllStockHoldings(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM stock_holdings WHERE user_id = ?`, userID)
	return err
}

// --- Crypto holdings -------------------------------------------------------

func (s *Store) ListCryptoHoldings(ctx context.Context, userID int64) ([]*domain.CryptoHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, user_id, name, symbol, classification, is_core, category, wallet,
		        quantity_held, quantity_staked,
		        avg_buy_eur, cost_basis_eur, current_price_eur, current_value_eur,
		        avg_buy_usd, cost_basis_usd, current_price_usd, current_value_usd,
		        rsi14, change_7d_pct, change_30d_pct, daily_change_pct, strategy_note,
		        updated_at
		 FROM crypto_holdings WHERE user_id = ?
		 ORDER BY current_value_usd DESC NULLS LAST, id`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.CryptoHolding
	for rows.Next() {
		h, err := scanCrypto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) InsertCryptoHolding(ctx context.Context, h *domain.CryptoHolding) (int64, error) {
	isCore := 0
	if h.IsCore || h.Classification == "core" {
		isCore = 1
	}
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO crypto_holdings (
		    user_id, name, symbol, classification, is_core, category, wallet,
		    quantity_held, quantity_staked,
		    avg_buy_eur, cost_basis_eur, current_price_eur, current_value_eur,
		    avg_buy_usd, cost_basis_usd, current_price_usd, current_value_usd,
		    rsi14, change_7d_pct, change_30d_pct, strategy_note,
		    updated_at
		 ) VALUES (?,?,?,?,?,?,?, ?,?, ?,?,?,?, ?,?,?,?, ?,?,?,?, strftime('%s','now'))`,
		h.UserID, h.Name, h.Symbol, h.Classification, isCore, strPtrToNull(h.Category), strPtrToNull(h.Wallet),
		h.QuantityHeld, h.QuantityStaked,
		fp(h.AvgBuyEUR), fp(h.CostBasisEUR), fp(h.CurrentPriceEUR), fp(h.CurrentValueEUR),
		fp(h.AvgBuyUSD), fp(h.CostBasisUSD), fp(h.CurrentPriceUSD), fp(h.CurrentValueUSD),
		fp(h.RSI14), fp(h.Change7dPct), fp(h.Change30dPct), h.StrategyNote,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteAllCryptoHoldings(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM crypto_holdings WHERE user_id = ?`, userID)
	return err
}

// --- Scanners --------------------------------------------------------------

// Scannable is anything with a Scan method. Used so we can reuse scanStock /
// scanCrypto across QueryRow and Query (rows) calls.
type Scannable interface {
	Scan(dest ...any) error
}

func scanStock(r Scannable) (*domain.StockHolding, error) {
	var h domain.StockHolding
	var ticker, category, sector, techSetup, rrView sql.NullString
	var avgOpen, currentPrice sql.NullFloat64
	var rsi, ma50, ma200 sql.NullFloat64
	var goldenCross sql.NullInt64
	var support, resistance, analystTarget sql.NullFloat64
	var proposedEntry, stopLoss, takeProfit sql.NullFloat64
	var dailyChange sql.NullFloat64
	var updatedAt int64
	if err := r.Scan(
		&h.ID, &h.UserID, &h.Name, &ticker, &category, &sector,
		&h.InvestedUSD, &avgOpen, &currentPrice,
		&rsi, &ma50, &ma200, &goldenCross,
		&support, &resistance, &analystTarget,
		&proposedEntry, &techSetup, &rrView,
		&stopLoss, &takeProfit, &h.StrategyNote,
		&dailyChange,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	h.Ticker = nsToPtr(ticker)
	h.Category = nsToPtr(category)
	h.Sector = nsToPtr(sector)
	h.AvgOpenPrice = nfToPtr(avgOpen)
	h.CurrentPrice = nfToPtr(currentPrice)
	h.RSI14 = nfToPtr(rsi)
	h.MA50 = nfToPtr(ma50)
	h.MA200 = nfToPtr(ma200)
	h.GoldenCross = nbToPtr(goldenCross)
	h.Support = nfToPtr(support)
	h.Resistance = nfToPtr(resistance)
	h.AnalystTarget = nfToPtr(analystTarget)
	h.ProposedEntry = nfToPtr(proposedEntry)
	h.TechnicalSetup = nsToPtrNonEmpty(techSetup)
	h.AnalystRRView = nsToPtrNonEmpty(rrView)
	h.StopLoss = nfToPtr(stopLoss)
	h.TakeProfit = nfToPtr(takeProfit)
	h.DailyChangePct = nfToPtr(dailyChange)
	h.UpdatedAt = time.Unix(updatedAt, 0)
	return &h, nil
}

func scanCrypto(r Scannable) (*domain.CryptoHolding, error) {
	var h domain.CryptoHolding
	var category, wallet sql.NullString
	var isCore int64
	var avgEur, costEur, priceEur, valueEur sql.NullFloat64
	var avgUsd, costUsd, priceUsd, valueUsd sql.NullFloat64
	var rsi, c7, c30, daily sql.NullFloat64
	var updatedAt int64
	if err := r.Scan(
		&h.ID, &h.UserID, &h.Name, &h.Symbol, &h.Classification, &isCore, &category, &wallet,
		&h.QuantityHeld, &h.QuantityStaked,
		&avgEur, &costEur, &priceEur, &valueEur,
		&avgUsd, &costUsd, &priceUsd, &valueUsd,
		&rsi, &c7, &c30, &daily, &h.StrategyNote,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	h.IsCore = isCore != 0
	h.Category = nsToPtr(category)
	h.Wallet = nsToPtr(wallet)
	h.AvgBuyEUR = nfToPtr(avgEur)
	h.CostBasisEUR = nfToPtr(costEur)
	h.CurrentPriceEUR = nfToPtr(priceEur)
	h.CurrentValueEUR = nfToPtr(valueEur)
	h.AvgBuyUSD = nfToPtr(avgUsd)
	h.CostBasisUSD = nfToPtr(costUsd)
	h.CurrentPriceUSD = nfToPtr(priceUsd)
	h.CurrentValueUSD = nfToPtr(valueUsd)
	h.RSI14 = nfToPtr(rsi)
	h.Change7dPct = nfToPtr(c7)
	h.Change30dPct = nfToPtr(c30)
	h.DailyChangePct = nfToPtr(daily)
	h.UpdatedAt = time.Unix(updatedAt, 0)
	return &h, nil
}

// --- Conversion helpers ----------------------------------------------------

func nsToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

// nsToPtrNonEmpty returns nil for both NULL and empty string. Used for enum-ish
// fields where empty == unset (TechnicalSetup, AnalystRRView).
func nsToPtrNonEmpty(n sql.NullString) *string {
	if !n.Valid || n.String == "" {
		return nil
	}
	v := n.String
	return &v
}

func nfToPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}

func nbToPtr(n sql.NullInt64) *bool {
	if !n.Valid {
		return nil
	}
	v := n.Int64 != 0
	return &v
}

// strPtrToNull renders a *string for sql.Exec as either the string value or
// SQL NULL (nil interface).
func strPtrToNull(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func fp(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func bp(p *bool) any {
	if p == nil {
		return nil
	}
	if *p {
		return 1
	}
	return 0
}

func stringFromPtrOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
