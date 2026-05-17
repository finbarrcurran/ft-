package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"time"
)

// All "list" queries filter `WHERE deleted_at IS NULL` so soft-deleted rows
// don't appear in the dashboard. The Settings → Deleted Holdings panel uses
// the explicit ListDeletedXxx variants.

// --- Stock holdings --------------------------------------------------------

const stockSelectCols = `id, user_id, name, ticker, category, sector,
        invested_usd, avg_open_price, current_price,
        rsi14, ma50, ma200, golden_cross,
        support, resistance, analyst_target,
        proposed_entry, technical_setup, analyst_rr_view,
        stop_loss, take_profit, strategy_note,
        daily_change_pct,
        note, beta, earnings_date, ex_dividend_date, deleted_at,
        exchange_override,
        support_1, support_2, resistance_1, resistance_2,
        atr_weekly, vol_tier_auto, setup_type, stage,
        tp1_hit_at, tp2_hit_at, time_stop_review_at,
        thesis_link, realized_pnl_usd,
        updated_at`

func (s *Store) ListStockHoldings(ctx context.Context, userID int64) ([]*domain.StockHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+stockSelectCols+`
		 FROM stock_holdings WHERE user_id = ? AND deleted_at IS NULL
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

// ListDeletedStockHoldings returns soft-deleted stock rows for the Settings
// → Deleted Holdings restore panel.
func (s *Store) ListDeletedStockHoldings(ctx context.Context, userID int64) ([]*domain.StockHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+stockSelectCols+`
		 FROM stock_holdings WHERE user_id = ? AND deleted_at IS NOT NULL
		 ORDER BY deleted_at DESC, id`, userID,
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
		`SELECT `+stockSelectCols+`
		 FROM stock_holdings WHERE user_id = ? AND id = ?`, userID, id,
	)
	h, err := scanStock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

func (s *Store) InsertStockHolding(ctx context.Context, h *domain.StockHolding) (int64, error) {
	return execInsertStock(ctx, s.DB, h)
}

// InsertStockHoldingTx is the tx-aware variant used by the promote-to-holdings
// flow in Spec 4 D6.
func (s *Store) InsertStockHoldingTx(ctx context.Context, tx *sql.Tx, h *domain.StockHolding) (int64, error) {
	return execInsertStock(ctx, tx, h)
}

// execInsertStock takes anything that implements ExecContext so the same SQL
// can be used from either *sql.DB or *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func execInsertStock(ctx context.Context, e execer, h *domain.StockHolding) (int64, error) {
	res, err := e.ExecContext(ctx,
		`INSERT INTO stock_holdings (
		    user_id, name, ticker, category, sector,
		    invested_usd, avg_open_price, current_price,
		    rsi14, ma50, ma200, golden_cross,
		    support, resistance, analyst_target,
		    proposed_entry, technical_setup, analyst_rr_view,
		    stop_loss, take_profit, strategy_note,
		    note, beta, earnings_date, ex_dividend_date,
		    updated_at
		 ) VALUES (?,?,?,?,?, ?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?,?, ?,?,?,?, strftime('%s','now'))`,
		h.UserID, h.Name, strPtrToNull(h.Ticker), strPtrToNull(h.Category), strPtrToNull(h.Sector),
		h.InvestedUSD, fp(h.AvgOpenPrice), fp(h.CurrentPrice),
		fp(h.RSI14), fp(h.MA50), fp(h.MA200), bp(h.GoldenCross),
		fp(h.Support), fp(h.Resistance), fp(h.AnalystTarget),
		fp(h.ProposedEntry),
		stringFromPtrOrEmpty(h.TechnicalSetup),
		stringFromPtrOrEmpty(h.AnalystRRView),
		fp(h.StopLoss), fp(h.TakeProfit), h.StrategyNote,
		strPtrToNull(h.Note), fp(h.Beta),
		strPtrToNull(h.EarningsDate), strPtrToNull(h.ExDividendDate),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateStockHolding writes the user-editable fields back to the row. Only
// the fields editable from the Spec 3 D5 Edit modal are touched; market-data
// fields (RSI/MAs etc.) are managed by the refresh service. updated_at bumps
// on every call.
//
// Spec 9c additions: support_1/2, resistance_1/2, setup_type, stage. Stage
// is normally auto-managed (via tp1_hit endpoint) but allowed here for
// admin/correction flows. Tip-hit timestamps and ATR/vol_tier_auto are
// NOT user-editable — refresh service / daily cron owns them.
func (s *Store) UpdateStockHolding(ctx context.Context, h *domain.StockHolding) error {
	_, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings SET
		   name = ?, ticker = ?, category = ?, sector = ?,
		   invested_usd = ?, avg_open_price = ?, current_price = ?,
		   stop_loss = ?, take_profit = ?,
		   strategy_note = ?, note = ?,
		   support_1 = ?, support_2 = ?, resistance_1 = ?, resistance_2 = ?,
		   setup_type = ?, stage = ?,
		   thesis_link = ?,
		   exchange_override = ?,
		   updated_at = strftime('%s','now')
		 WHERE user_id = ? AND id = ?`,
		h.Name, strPtrToNull(h.Ticker), strPtrToNull(h.Category), strPtrToNull(h.Sector),
		h.InvestedUSD, fp(h.AvgOpenPrice), fp(h.CurrentPrice),
		fp(h.StopLoss), fp(h.TakeProfit),
		h.StrategyNote, strPtrToNull(h.Note),
		fp(h.Support1), fp(h.Support2), fp(h.Resistance1), fp(h.Resistance2),
		strPtrToNull(h.SetupType), h.Stage,
		strPtrToNull(h.ThesisLink),
		strPtrToNull(h.ExchangeOverride),
		h.UserID, h.ID,
	)
	return err
}

// SoftDeleteStockHolding sets deleted_at = now. Doesn't physically remove.
func (s *Store) SoftDeleteStockHolding(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings SET deleted_at = strftime('%s','now')
		 WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RestoreStockHolding clears deleted_at on a previously soft-deleted row.
func (s *Store) RestoreStockHolding(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE stock_holdings SET deleted_at = NULL
		 WHERE user_id = ? AND id = ? AND deleted_at IS NOT NULL`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAllStockHoldings(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM stock_holdings WHERE user_id = ?`, userID)
	return err
}

// --- Crypto holdings -------------------------------------------------------

const cryptoSelectCols = `id, user_id, name, symbol, classification, is_core, category, wallet,
        quantity_held, quantity_staked,
        avg_buy_eur, cost_basis_eur, current_price_eur, current_value_eur,
        avg_buy_usd, cost_basis_usd, current_price_usd, current_value_usd,
        rsi14, change_7d_pct, change_30d_pct, daily_change_pct, strategy_note,
        note, vol_tier, deleted_at,
        support_1, support_2, resistance_1, resistance_2,
        atr_weekly, vol_tier_auto, setup_type, stage,
        tp1_hit_at, tp2_hit_at, time_stop_review_at,
        thesis_link, realized_pnl_usd,
        updated_at`

func (s *Store) ListCryptoHoldings(ctx context.Context, userID int64) ([]*domain.CryptoHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+cryptoSelectCols+`
		 FROM crypto_holdings WHERE user_id = ? AND deleted_at IS NULL
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

func (s *Store) ListDeletedCryptoHoldings(ctx context.Context, userID int64) ([]*domain.CryptoHolding, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+cryptoSelectCols+`
		 FROM crypto_holdings WHERE user_id = ? AND deleted_at IS NOT NULL
		 ORDER BY deleted_at DESC, id`, userID,
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

func (s *Store) GetCryptoHolding(ctx context.Context, userID, id int64) (*domain.CryptoHolding, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+cryptoSelectCols+`
		 FROM crypto_holdings WHERE user_id = ? AND id = ?`, userID, id,
	)
	h, err := scanCrypto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

func (s *Store) InsertCryptoHolding(ctx context.Context, h *domain.CryptoHolding) (int64, error) {
	return execInsertCrypto(ctx, s.DB, h)
}

// InsertCryptoHoldingTx is the tx-aware variant used by the promote-to-holdings
// flow in Spec 4 D6.
func (s *Store) InsertCryptoHoldingTx(ctx context.Context, tx *sql.Tx, h *domain.CryptoHolding) (int64, error) {
	return execInsertCrypto(ctx, tx, h)
}

func execInsertCrypto(ctx context.Context, e execer, h *domain.CryptoHolding) (int64, error) {
	isCore := 0
	if h.IsCore || h.Classification == "core" {
		isCore = 1
	}
	tier := h.VolTier
	if tier == "" {
		tier = "medium"
	}
	res, err := e.ExecContext(ctx,
		`INSERT INTO crypto_holdings (
		    user_id, name, symbol, classification, is_core, category, wallet,
		    quantity_held, quantity_staked,
		    avg_buy_eur, cost_basis_eur, current_price_eur, current_value_eur,
		    avg_buy_usd, cost_basis_usd, current_price_usd, current_value_usd,
		    rsi14, change_7d_pct, change_30d_pct, strategy_note,
		    note, vol_tier,
		    updated_at
		 ) VALUES (?,?,?,?,?,?,?, ?,?, ?,?,?,?, ?,?,?,?, ?,?,?,?, ?,?, strftime('%s','now'))`,
		h.UserID, h.Name, h.Symbol, h.Classification, isCore, strPtrToNull(h.Category), strPtrToNull(h.Wallet),
		h.QuantityHeld, h.QuantityStaked,
		fp(h.AvgBuyEUR), fp(h.CostBasisEUR), fp(h.CurrentPriceEUR), fp(h.CurrentValueEUR),
		fp(h.AvgBuyUSD), fp(h.CostBasisUSD), fp(h.CurrentPriceUSD), fp(h.CurrentValueUSD),
		fp(h.RSI14), fp(h.Change7dPct), fp(h.Change30dPct), h.StrategyNote,
		strPtrToNull(h.Note), tier,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateCryptoHolding(ctx context.Context, h *domain.CryptoHolding) error {
	isCore := 0
	if h.IsCore || h.Classification == "core" {
		isCore = 1
	}
	tier := h.VolTier
	if tier == "" {
		tier = "medium"
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings SET
		   name = ?, symbol = ?, classification = ?, is_core = ?, vol_tier = ?,
		   category = ?, wallet = ?,
		   quantity_held = ?, quantity_staked = ?,
		   avg_buy_eur = ?, cost_basis_eur = ?,
		   strategy_note = ?, note = ?,
		   support_1 = ?, support_2 = ?, resistance_1 = ?, resistance_2 = ?,
		   setup_type = ?, stage = ?,
		   thesis_link = ?,
		   updated_at = strftime('%s','now')
		 WHERE user_id = ? AND id = ?`,
		h.Name, h.Symbol, h.Classification, isCore, tier,
		strPtrToNull(h.Category), strPtrToNull(h.Wallet),
		h.QuantityHeld, h.QuantityStaked,
		fp(h.AvgBuyEUR), fp(h.CostBasisEUR),
		h.StrategyNote, strPtrToNull(h.Note),
		fp(h.Support1), fp(h.Support2), fp(h.Resistance1), fp(h.Resistance2),
		strPtrToNull(h.SetupType), h.Stage,
		strPtrToNull(h.ThesisLink),
		h.UserID, h.ID,
	)
	return err
}

func (s *Store) SoftDeleteCryptoHolding(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings SET deleted_at = strftime('%s','now')
		 WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RestoreCryptoHolding(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE crypto_holdings SET deleted_at = NULL
		 WHERE user_id = ? AND id = ? AND deleted_at IS NOT NULL`, userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAllCryptoHoldings(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM crypto_holdings WHERE user_id = ?`, userID)
	return err
}

// --- Scanners --------------------------------------------------------------

// Scannable is anything with a Scan method.
type Scannable interface {
	Scan(dest ...any) error
}

func scanStock(r Scannable) (*domain.StockHolding, error) {
	var h domain.StockHolding
	var ticker, category, sector, techSetup, rrView sql.NullString
	var note, earningsDate, exDivDate, exchangeOverride sql.NullString
	var avgOpen, currentPrice sql.NullFloat64
	var rsi, ma50, ma200, beta sql.NullFloat64
	var goldenCross sql.NullInt64
	var support, resistance, analystTarget sql.NullFloat64
	var proposedEntry, stopLoss, takeProfit sql.NullFloat64
	var dailyChange sql.NullFloat64
	var deletedAt sql.NullInt64
	var updatedAt int64
	// Spec 9c columns:
	var support1, support2, resistance1, resistance2 sql.NullFloat64
	var atrWeekly sql.NullFloat64
	var volTierAuto, setupType, stage, timeStopReviewAt sql.NullString
	var tp1HitAt, tp2HitAt sql.NullInt64
	// Spec 10 columns:
	var thesisLink sql.NullString
	var realizedPnL float64
	if err := r.Scan(
		&h.ID, &h.UserID, &h.Name, &ticker, &category, &sector,
		&h.InvestedUSD, &avgOpen, &currentPrice,
		&rsi, &ma50, &ma200, &goldenCross,
		&support, &resistance, &analystTarget,
		&proposedEntry, &techSetup, &rrView,
		&stopLoss, &takeProfit, &h.StrategyNote,
		&dailyChange,
		&note, &beta, &earningsDate, &exDivDate, &deletedAt,
		&exchangeOverride,
		&support1, &support2, &resistance1, &resistance2,
		&atrWeekly, &volTierAuto, &setupType, &stage,
		&tp1HitAt, &tp2HitAt, &timeStopReviewAt,
		&thesisLink, &realizedPnL,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	h.ThesisLink = nsToPtrNonEmpty(thesisLink)
	h.RealizedPnLUSD = realizedPnL
	h.Support1 = nfToPtr(support1)
	h.Support2 = nfToPtr(support2)
	h.Resistance1 = nfToPtr(resistance1)
	h.Resistance2 = nfToPtr(resistance2)
	h.ATRWeekly = nfToPtr(atrWeekly)
	h.VolTierAuto = nsToPtrNonEmpty(volTierAuto)
	h.SetupType = nsToPtrNonEmpty(setupType)
	if stage.Valid && stage.String != "" {
		h.Stage = stage.String
	} else {
		h.Stage = "pre_tp1"
	}
	if tp1HitAt.Valid {
		t := time.Unix(tp1HitAt.Int64, 0).UTC()
		h.TP1HitAt = &t
	}
	if tp2HitAt.Valid {
		t := time.Unix(tp2HitAt.Int64, 0).UTC()
		h.TP2HitAt = &t
	}
	h.TimeStopReviewAt = nsToPtrNonEmpty(timeStopReviewAt)
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
	h.Note = nsToPtrNonEmpty(note)
	h.Beta = nfToPtr(beta)
	h.EarningsDate = nsToPtrNonEmpty(earningsDate)
	h.ExDividendDate = nsToPtrNonEmpty(exDivDate)
	h.ExchangeOverride = nsToPtrNonEmpty(exchangeOverride)
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0)
		h.DeletedAt = &t
	}
	h.UpdatedAt = time.Unix(updatedAt, 0)
	return &h, nil
}

func scanCrypto(r Scannable) (*domain.CryptoHolding, error) {
	var h domain.CryptoHolding
	var category, wallet sql.NullString
	var note sql.NullString
	var isCore int64
	var avgEur, costEur, priceEur, valueEur sql.NullFloat64
	var avgUsd, costUsd, priceUsd, valueUsd sql.NullFloat64
	var rsi, c7, c30, daily sql.NullFloat64
	var deletedAt sql.NullInt64
	var updatedAt int64
	// Spec 9c columns:
	var support1, support2, resistance1, resistance2 sql.NullFloat64
	var atrWeekly sql.NullFloat64
	var volTierAuto, setupType, stage, timeStopReviewAt sql.NullString
	var tp1HitAt, tp2HitAt sql.NullInt64
	// Spec 10 columns:
	var thesisLink sql.NullString
	var realizedPnL float64
	if err := r.Scan(
		&h.ID, &h.UserID, &h.Name, &h.Symbol, &h.Classification, &isCore, &category, &wallet,
		&h.QuantityHeld, &h.QuantityStaked,
		&avgEur, &costEur, &priceEur, &valueEur,
		&avgUsd, &costUsd, &priceUsd, &valueUsd,
		&rsi, &c7, &c30, &daily, &h.StrategyNote,
		&note, &h.VolTier, &deletedAt,
		&support1, &support2, &resistance1, &resistance2,
		&atrWeekly, &volTierAuto, &setupType, &stage,
		&tp1HitAt, &tp2HitAt, &timeStopReviewAt,
		&thesisLink, &realizedPnL,
		&updatedAt,
	); err != nil {
		return nil, err
	}
	h.ThesisLink = nsToPtrNonEmpty(thesisLink)
	h.RealizedPnLUSD = realizedPnL
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
	h.Note = nsToPtrNonEmpty(note)
	if deletedAt.Valid {
		t := time.Unix(deletedAt.Int64, 0)
		h.DeletedAt = &t
	}
	// Spec 9c fields:
	h.Support1 = nfToPtr(support1)
	h.Support2 = nfToPtr(support2)
	h.Resistance1 = nfToPtr(resistance1)
	h.Resistance2 = nfToPtr(resistance2)
	h.ATRWeekly = nfToPtr(atrWeekly)
	h.VolTierAuto = nsToPtrNonEmpty(volTierAuto)
	h.SetupType = nsToPtrNonEmpty(setupType)
	if stage.Valid && stage.String != "" {
		h.Stage = stage.String
	} else {
		h.Stage = "pre_tp1"
	}
	if tp1HitAt.Valid {
		t := time.Unix(tp1HitAt.Int64, 0).UTC()
		h.TP1HitAt = &t
	}
	if tp2HitAt.Valid {
		t := time.Unix(tp2HitAt.Int64, 0).UTC()
		h.TP2HitAt = &t
	}
	h.TimeStopReviewAt = nsToPtrNonEmpty(timeStopReviewAt)
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
