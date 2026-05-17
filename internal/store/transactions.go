package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Spec 10 — append-only transactions log + derived position state (FIFO).
//
// The store layer exposes:
//   - InsertTransaction / SupersedeTransaction (no UPDATE on data)
//   - InsertDividend
//   - ListTransactions / ListDividends per holding
//   - ComputeAndCacheDerivedPosition — pure-ish helper that walks the
//     active txn log, computes FIFO tax lots + realized P&L, writes the
//     cached fields back to the holding row.

// TxnType constants matching the migration's CHECK.
const (
	TxnTypeBuy     = "buy"
	TxnTypeSell    = "sell"
	TxnTypeFee     = "fee"
	TxnTypeOpening = "opening_position"
)

// TransactionRow mirrors the transactions table.
type TransactionRow struct {
	ID            int64     `json:"id"`
	HoldingKind   string    `json:"holdingKind"`
	HoldingID     int64     `json:"holdingId"`
	Ticker        string    `json:"ticker"`
	TxnType       string    `json:"txnType"`
	ExecutedAt    time.Time `json:"executedAt"`
	Quantity      float64   `json:"quantity"`
	PriceUSD      float64   `json:"priceUsd"`
	FeesUSD       float64   `json:"feesUsd"`
	TotalUSD      float64   `json:"totalUsd"`
	Venue         string    `json:"venue,omitempty"`
	ExternalID    string    `json:"externalId,omitempty"`
	Note          string    `json:"note,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	SupersededAt  *time.Time `json:"supersededAt,omitempty"`
}

// InsertTransaction appends one row. NEVER updates an existing row's data;
// corrections go through SupersedeTransaction.
func (s *Store) InsertTransaction(ctx context.Context, t TransactionRow) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO transactions (
		  holding_kind, holding_id, ticker, txn_type, executed_at,
		  quantity, price_usd, fees_usd, total_usd,
		  venue, external_id, note, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,strftime('%s','now'))`,
		t.HoldingKind, t.HoldingID, strings.ToUpper(t.Ticker), t.TxnType, t.ExecutedAt.Unix(),
		t.Quantity, t.PriceUSD, t.FeesUSD, t.TotalUSD,
		nullIfBlank(t.Venue), nullIfBlank(t.ExternalID), nullIfBlank(t.Note),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SupersedeTransaction soft-deletes a txn (sets superseded_at). Excludes
// the row from future derivation while preserving the history record.
func (s *Store) SupersedeTransaction(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE transactions SET superseded_at = strftime('%s','now')
		 WHERE id = ? AND superseded_at IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTransactions returns active (non-superseded) transactions for a holding,
// ordered chronologically (oldest-first) for FIFO walk.
func (s *Store) ListTransactions(ctx context.Context, kind string, holdingID int64) ([]TransactionRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, holding_kind, holding_id, ticker, txn_type, executed_at,
		       quantity, price_usd, fees_usd, total_usd,
		       COALESCE(venue,''), COALESCE(external_id,''), COALESCE(note,''),
		       created_at, superseded_at
		  FROM transactions
		 WHERE holding_kind = ? AND holding_id = ? AND superseded_at IS NULL
		 ORDER BY executed_at ASC, id ASC`, kind, holdingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TransactionRow{}
	for rows.Next() {
		var (
			t                              TransactionRow
			executedAt, createdAt          int64
			supersededAt                   sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.HoldingKind, &t.HoldingID, &t.Ticker, &t.TxnType,
			&executedAt, &t.Quantity, &t.PriceUSD, &t.FeesUSD, &t.TotalUSD,
			&t.Venue, &t.ExternalID, &t.Note, &createdAt, &supersededAt); err != nil {
			return nil, err
		}
		t.ExecutedAt = time.Unix(executedAt, 0).UTC()
		t.CreatedAt = time.Unix(createdAt, 0).UTC()
		if supersededAt.Valid {
			ts := time.Unix(supersededAt.Int64, 0).UTC()
			t.SupersededAt = &ts
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListAllTransactions returns active txns for all holdings of a user,
// joined to derive ticker/kind for display in Settings.
func (s *Store) ListAllTransactions(ctx context.Context, userID int64, limit int) ([]TransactionRow, error) {
	if limit <= 0 {
		limit = 500
	}
	// Note: transactions table doesn't have user_id directly; we filter via
	// the holding's user_id. UNION across both kinds.
	rows, err := s.DB.QueryContext(ctx, `
		SELECT t.id, t.holding_kind, t.holding_id, t.ticker, t.txn_type, t.executed_at,
		       t.quantity, t.price_usd, t.fees_usd, t.total_usd,
		       COALESCE(t.venue,''), COALESCE(t.external_id,''), COALESCE(t.note,''),
		       t.created_at, t.superseded_at
		  FROM transactions t
		  LEFT JOIN stock_holdings sh ON t.holding_kind='stock' AND t.holding_id = sh.id AND sh.user_id = ?
		  LEFT JOIN crypto_holdings ch ON t.holding_kind='crypto' AND t.holding_id = ch.id AND ch.user_id = ?
		 WHERE t.superseded_at IS NULL
		   AND (sh.id IS NOT NULL OR ch.id IS NOT NULL)
		 ORDER BY t.executed_at DESC, t.id DESC
		 LIMIT ?`, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TransactionRow{}
	for rows.Next() {
		var (
			t                              TransactionRow
			executedAt, createdAt          int64
			supersededAt                   sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.HoldingKind, &t.HoldingID, &t.Ticker, &t.TxnType,
			&executedAt, &t.Quantity, &t.PriceUSD, &t.FeesUSD, &t.TotalUSD,
			&t.Venue, &t.ExternalID, &t.Note, &createdAt, &supersededAt); err != nil {
			return nil, err
		}
		t.ExecutedAt = time.Unix(executedAt, 0).UTC()
		t.CreatedAt = time.Unix(createdAt, 0).UTC()
		if supersededAt.Valid {
			ts := time.Unix(supersededAt.Int64, 0).UTC()
			t.SupersededAt = &ts
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ----- Dividends -------------------------------------------------------

type DividendRow struct {
	ID                int64     `json:"id"`
	HoldingID         int64     `json:"holdingId"`
	Ticker            string    `json:"ticker"`
	ExDate            string    `json:"exDate"`
	PayDate           string    `json:"payDate,omitempty"`
	AmountPerShareUSD float64   `json:"amountPerShareUsd"`
	SharesHeld        float64   `json:"sharesHeld"`
	TotalReceivedUSD  float64   `json:"totalReceivedUsd"`
	Note              string    `json:"note,omitempty"`
	CreatedAt         time.Time `json:"createdAt"`
}

func (s *Store) InsertDividend(ctx context.Context, d DividendRow) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO dividends (
		  holding_id, ticker, ex_date, pay_date,
		  amount_per_share_usd, shares_held, total_received_usd, note, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%s','now'))`,
		d.HoldingID, strings.ToUpper(d.Ticker), d.ExDate, nullIfBlank(d.PayDate),
		d.AmountPerShareUSD, d.SharesHeld, d.TotalReceivedUSD, nullIfBlank(d.Note))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListDividendsForHolding(ctx context.Context, holdingID int64) ([]DividendRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, holding_id, ticker, ex_date, COALESCE(pay_date,''),
		       amount_per_share_usd, shares_held, total_received_usd,
		       COALESCE(note,''), created_at
		  FROM dividends WHERE holding_id = ?
		  ORDER BY ex_date DESC, id DESC`, holdingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DividendRow{}
	for rows.Next() {
		var d DividendRow
		var createdAt int64
		if err := rows.Scan(&d.ID, &d.HoldingID, &d.Ticker, &d.ExDate, &d.PayDate,
			&d.AmountPerShareUSD, &d.SharesHeld, &d.TotalReceivedUSD,
			&d.Note, &createdAt); err != nil {
			return nil, err
		}
		d.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}

// ----- Derived position state (FIFO) -----------------------------------

// TaxLot is one remaining open lot — slice of an earlier buy that hasn't
// been sold yet.
type TaxLot struct {
	OpenedAt         time.Time `json:"openedAt"`
	QuantityOpen     float64   `json:"quantityOpen"`     // remaining
	QuantityOrig     float64   `json:"quantityOrig"`     // at acquisition
	PricePerUnit     float64   `json:"pricePerUnit"`
	HoldingDays      int       `json:"holdingDays"`
	UnrealizedPnLUSD float64   `json:"unrealizedPnlUsd"`
}

// DerivedPosition is the pure-function output of ComputeFIFOPosition.
type DerivedPosition struct {
	Quantity         float64  `json:"quantity"`
	CostBasisAvgUSD  float64  `json:"costBasisAvgUsd"`
	TotalInvestedUSD float64  `json:"totalInvestedUsd"` // sum of all buy + opening totals
	RealizedPnLUSD   float64  `json:"realizedPnlUsd"`
	TaxLots          []TaxLot `json:"taxLots"`
}

// ComputeFIFOPosition walks the txn log and returns the live position
// state. Pure function — no IO. ASC-by-executed_at is assumed.
//
// FIFO rules:
//   - buy / opening_position: pushes a new lot to the queue
//   - sell: consumes lots from the front of the queue. Realized P&L
//     for the consumed portion = (sell_price − lot_price) × consumed_qty.
//     Fees deducted from realized P&L.
//   - fee: pure cost (reduces realized P&L without changing quantity)
//
// Crypto handles fractional quantities (8+ decimals); REAL has enough
// precision. We round display values at the UI, not here.
func ComputeFIFOPosition(txns []TransactionRow, currentPrice float64) DerivedPosition {
	lots := []TaxLot{}
	var realized float64
	var totalInvested float64
	now := time.Now().UTC()

	for _, t := range txns {
		switch t.TxnType {
		case TxnTypeBuy, TxnTypeOpening:
			lots = append(lots, TaxLot{
				OpenedAt:     t.ExecutedAt,
				QuantityOpen: t.Quantity,
				QuantityOrig: t.Quantity,
				PricePerUnit: t.PriceUSD,
			})
			totalInvested += t.Quantity*t.PriceUSD + t.FeesUSD
			realized -= t.FeesUSD // buy fees come out of P&L immediately
		case TxnTypeSell:
			remaining := t.Quantity
			for remaining > 0 && len(lots) > 0 {
				lot := &lots[0]
				take := lot.QuantityOpen
				if take > remaining {
					take = remaining
				}
				realized += (t.PriceUSD - lot.PricePerUnit) * take
				lot.QuantityOpen -= take
				remaining -= take
				if lot.QuantityOpen <= 1e-9 {
					lots = lots[1:]
				}
			}
			realized -= t.FeesUSD
		case TxnTypeFee:
			realized -= t.FeesUSD
		}
	}

	// Aggregate live numbers.
	var qty, costSum float64
	out := DerivedPosition{
		RealizedPnLUSD:   realized,
		TotalInvestedUSD: totalInvested,
	}
	for i := range lots {
		l := &lots[i]
		l.HoldingDays = int(now.Sub(l.OpenedAt).Hours() / 24)
		if currentPrice > 0 {
			l.UnrealizedPnLUSD = (currentPrice - l.PricePerUnit) * l.QuantityOpen
		}
		qty += l.QuantityOpen
		costSum += l.PricePerUnit * l.QuantityOpen
		out.TaxLots = append(out.TaxLots, *l)
	}
	out.Quantity = qty
	if qty > 0 {
		out.CostBasisAvgUSD = costSum / qty
	}
	return out
}

// CacheDerivedPosition writes the derived quantity / cost / realized_pnl
// fields back to the holding row. Called after every transaction
// mutation so list views stay fast (no per-row recompute).
func (s *Store) CacheDerivedStockPosition(ctx context.Context, holdingID int64, pos DerivedPosition) error {
	// stock_holdings stores invested_usd (cost basis) + avg_open_price + realized_pnl_usd.
	// quantity is implicit (invested / avg_open). We update invested_usd =
	// total_invested_usd and avg_open_price = cost_basis_avg.
	_, err := s.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET avg_open_price = ?, realized_pnl_usd = ?,
		       updated_at = strftime('%s','now')
		 WHERE id = ?`,
		nullIfZero(pos.CostBasisAvgUSD), pos.RealizedPnLUSD, holdingID)
	return err
}

func (s *Store) CacheDerivedCryptoPosition(ctx context.Context, holdingID int64, pos DerivedPosition) error {
	// crypto_holdings stores quantity_held + avg_buy_usd + cost_basis_usd
	// + realized_pnl_usd. We don't touch quantity_staked (that's a separate
	// concept — staked qty is a subset of total held, manually tracked).
	_, err := s.DB.ExecContext(ctx, `
		UPDATE crypto_holdings
		   SET quantity_held = ?, avg_buy_usd = ?, cost_basis_usd = ?,
		       realized_pnl_usd = ?, updated_at = strftime('%s','now')
		 WHERE id = ?`,
		pos.Quantity, nullIfZero(pos.CostBasisAvgUSD), pos.TotalInvestedUSD,
		pos.RealizedPnLUSD, holdingID)
	return err
}

// GetTransaction returns one row by id, regardless of superseded state.
func (s *Store) GetTransaction(ctx context.Context, id int64) (*TransactionRow, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, holding_kind, holding_id, ticker, txn_type, executed_at,
		       quantity, price_usd, fees_usd, total_usd,
		       COALESCE(venue,''), COALESCE(external_id,''), COALESCE(note,''),
		       created_at, superseded_at
		  FROM transactions WHERE id = ?`, id)
	var (
		t                     TransactionRow
		executedAt, createdAt int64
		supersededAt          sql.NullInt64
	)
	if err := row.Scan(&t.ID, &t.HoldingKind, &t.HoldingID, &t.Ticker, &t.TxnType,
		&executedAt, &t.Quantity, &t.PriceUSD, &t.FeesUSD, &t.TotalUSD,
		&t.Venue, &t.ExternalID, &t.Note, &createdAt, &supersededAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.ExecutedAt = time.Unix(executedAt, 0).UTC()
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	if supersededAt.Valid {
		ts := time.Unix(supersededAt.Int64, 0).UTC()
		t.SupersededAt = &ts
	}
	return &t, nil
}

// nullIfZero — for SQL writes where 0 should be NULL (avg_open_price
// shouldn't be 0 just because we don't know it yet).
func nullIfZero(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
