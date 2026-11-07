package store

import (
	"context"
	"database/sql"
	"time"

	"ft/internal/domain"
)

// Earnings statuses (ticker_calendar.earnings_status).
const (
	CalUnknown       = "UNKNOWN"
	CalDateKnown     = "DATE_KNOWN"
	CalDateEstimated = "DATE_ESTIMATED"
	CalReported      = "REPORTED"
	CalNotAnnounced  = "NOT_ANNOUNCED"
	CalNotApplicable = "NOT_APPLICABLE"
	CalFetchError    = "FETCH_ERROR"
)

// TickerCalendar is one row of ticker_calendar (SC-56).
type TickerCalendar struct {
	Ticker           string  `json:"ticker"`
	ProviderSymbol   string  `json:"providerSymbol,omitempty"`
	EarningsDate     *string `json:"earningsDate"`
	IsEstimate       bool    `json:"earningsIsEstimate"`
	Status           string  `json:"earningsStatus"`
	Source           string  `json:"earningsSource,omitempty"`
	FetchedAt        *int64  `json:"earningsFetchedAt,omitempty"`
	LastError        string  `json:"lastError,omitempty"`
	ErrorCount       int     `json:"-"`
	ErrorPermanent   bool    `json:"-"`
	LastReportedDate *string `json:"lastReportedDate,omitempty"`
	ExDividendDate   *string `json:"exDividendDate,omitempty"`
	NotApplicableWhy string  `json:"notApplicableReason,omitempty"`
	SecondCheckedAt  *int64  `json:"-"`
	PrevEarningsDate *string `json:"-"`
	DateChangedAt    *int64  `json:"-"`
	UpdatedAt        int64   `json:"-"`
}

// CalendarTicker is a ticker in the refresh set with where it comes from.
type CalendarTicker struct {
	Ticker         string
	ProviderSymbol string
	Holding        bool
	Watchlist      bool
	Thesis         bool
	NotApplicable  string // reason, "" when applicable
}

const calCols = `ticker, COALESCE(provider_symbol,''), earnings_date, earnings_is_estimate, earnings_status,
	COALESCE(earnings_source,''), earnings_fetched_at, COALESCE(last_error,''), error_count, error_permanent,
	last_reported_date, ex_dividend_date, COALESCE(not_applicable_reason,''), second_checked_at,
	prev_earnings_date, date_changed_at, updated_at`

func scanCal(sc interface{ Scan(...any) error }) (*TickerCalendar, error) {
	var c TickerCalendar
	var ed, lr, xd, prev sql.NullString
	var fetched, second, changed sql.NullInt64
	var est, perm int
	if err := sc.Scan(&c.Ticker, &c.ProviderSymbol, &ed, &est, &c.Status, &c.Source, &fetched, &c.LastError,
		&c.ErrorCount, &perm, &lr, &xd, &c.NotApplicableWhy, &second, &prev, &changed, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.IsEstimate, c.ErrorPermanent = est != 0, perm != 0
	if ed.Valid {
		c.EarningsDate = &ed.String
	}
	if lr.Valid {
		c.LastReportedDate = &lr.String
	}
	if xd.Valid {
		c.ExDividendDate = &xd.String
	}
	if prev.Valid {
		c.PrevEarningsDate = &prev.String
	}
	if fetched.Valid {
		c.FetchedAt = &fetched.Int64
	}
	if second.Valid {
		c.SecondCheckedAt = &second.Int64
	}
	if changed.Valid {
		c.DateChangedAt = &changed.Int64
	}
	return &c, nil
}

// CalendarTickerSet returns the union of active stock holdings, non-deleted stock-watchlist rows and live
// theses (not superseded, not archived), with the provider symbol and any not-applicable reason.
func (s *Store) CalendarTickerSet(ctx context.Context) ([]CalendarTicker, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT u.ticker, COALESCE(m.provider_symbol, u.ticker), MAX(u.h), MAX(u.w), MAX(u.t), COALESCE(n.reason, '')
		  FROM (
		        SELECT ticker, 1 AS h, 0 AS w, 0 AS t FROM stock_holdings WHERE deleted_at IS NULL AND ticker IS NOT NULL AND ticker != ''
		        UNION ALL
		        SELECT ticker, 0, 1, 0 FROM watchlist WHERE deleted_at IS NULL AND kind = 'stock' AND ticker != ''
		        UNION ALL
		        SELECT ticker, 0, 0, 1 FROM theses_index WHERE status NOT IN ('superseded', 'archived')
		       ) u
		  LEFT JOIN ticker_symbol_map m ON m.ticker = u.ticker
		  LEFT JOIN ticker_calendar_na n ON n.ticker = u.ticker
		 GROUP BY u.ticker
		 ORDER BY u.ticker`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarTicker
	for rows.Next() {
		var c CalendarTicker
		var h, w, t int
		if err := rows.Scan(&c.Ticker, &c.ProviderSymbol, &h, &w, &t, &c.NotApplicable); err != nil {
			return nil, err
		}
		c.Holding, c.Watchlist, c.Thesis = h > 0, w > 0, t > 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetTickerCalendar returns one row, or nil when the ticker has none yet.
func (s *Store) GetTickerCalendar(ctx context.Context, ticker string) (*TickerCalendar, error) {
	c, err := scanCal(s.DB.QueryRowContext(ctx, `SELECT `+calCols+` FROM ticker_calendar WHERE ticker = ?`, ticker))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// ListTickerCalendar returns every row keyed by ticker.
func (s *Store) ListTickerCalendar(ctx context.Context) (map[string]*TickerCalendar, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+calCols+` FROM ticker_calendar`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*TickerCalendar{}
	for rows.Next() {
		c, err := scanCal(rows)
		if err != nil {
			return nil, err
		}
		out[c.Ticker] = c
	}
	return out, rows.Err()
}

// SaveTickerCalendar writes the whole row.
func (s *Store) SaveTickerCalendar(ctx context.Context, c *TickerCalendar) error {
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	if c.UpdatedAt == 0 {
		c.UpdatedAt = time.Now().Unix()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO ticker_calendar (ticker, provider_symbol, earnings_date, earnings_is_estimate, earnings_status,
		    earnings_source, earnings_fetched_at, last_error, error_count, error_permanent, last_reported_date,
		    ex_dividend_date, not_applicable_reason, second_checked_at, prev_earnings_date, date_changed_at, updated_at)
		VALUES (?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?)
		ON CONFLICT (ticker) DO UPDATE SET
		    provider_symbol = excluded.provider_symbol, earnings_date = excluded.earnings_date,
		    earnings_is_estimate = excluded.earnings_is_estimate, earnings_status = excluded.earnings_status,
		    earnings_source = excluded.earnings_source, earnings_fetched_at = excluded.earnings_fetched_at,
		    last_error = excluded.last_error, error_count = excluded.error_count, error_permanent = excluded.error_permanent,
		    last_reported_date = excluded.last_reported_date, ex_dividend_date = excluded.ex_dividend_date,
		    not_applicable_reason = excluded.not_applicable_reason, second_checked_at = excluded.second_checked_at,
		    prev_earnings_date = excluded.prev_earnings_date, date_changed_at = excluded.date_changed_at,
		    updated_at = excluded.updated_at`,
		c.Ticker, c.ProviderSymbol, c.EarningsDate, b2i(c.IsEstimate), c.Status, c.Source, c.FetchedAt, c.LastError,
		c.ErrorCount, b2i(c.ErrorPermanent), c.LastReportedDate, c.ExDividendDate, c.NotApplicableWhy,
		c.SecondCheckedAt, c.PrevEarningsDate, c.DateChangedAt, c.UpdatedAt)
	return err
}

// SyncHoldingCalendarDates keeps stock_holdings.earnings_date / ex_dividend_date populated for back-compat
// (CSV export, older readers): the next date, or NULL when there is none to show. It only writes rows
// whose value actually changes, so updated_at is not bumped hourly.
func (s *Store) SyncHoldingCalendarDates(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE stock_holdings
		   SET earnings_date = (SELECT c.earnings_date FROM ticker_calendar c WHERE c.ticker = stock_holdings.ticker),
		       ex_dividend_date = COALESCE((SELECT c.ex_dividend_date FROM ticker_calendar c WHERE c.ticker = stock_holdings.ticker), ex_dividend_date),
		       updated_at = ?
		 WHERE deleted_at IS NULL
		   AND EXISTS (SELECT 1 FROM ticker_calendar c WHERE c.ticker = stock_holdings.ticker)
		   AND (COALESCE(earnings_date, '') != COALESCE((SELECT c.earnings_date FROM ticker_calendar c WHERE c.ticker = stock_holdings.ticker), '')
		        OR COALESCE(ex_dividend_date, '') != COALESCE((SELECT c.ex_dividend_date FROM ticker_calendar c WHERE c.ticker = stock_holdings.ticker), COALESCE(ex_dividend_date, '')))`,
		time.Now().Unix())
	return err
}

// CalendarFetchedSince counts tickers whose provider answered within the window (daily-job log line).
func (s *Store) CalendarFetchedSince(ctx context.Context, d time.Duration) int {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticker_calendar WHERE earnings_fetched_at >= ?`, time.Now().Add(-d).Unix()).Scan(&n)
	return n
}

// CalendarInfoMap returns the API view of every ticker_calendar row, keyed by ticker.
func (s *Store) CalendarInfoMap(ctx context.Context) map[string]*domain.CalendarInfo {
	rows, err := s.ListTickerCalendar(ctx)
	if err != nil {
		return nil
	}
	out := make(map[string]*domain.CalendarInfo, len(rows))
	for t, c := range rows {
		if c.Status == CalUnknown {
			continue
		}
		note := ""
		switch c.Status {
		case CalNotApplicable:
			note = c.NotApplicableWhy
		case CalFetchError:
			note = c.LastError
		}
		out[t] = &domain.CalendarInfo{Status: c.Status, Date: c.EarningsDate, IsEstimate: c.IsEstimate, Source: c.Source,
			FetchedAt: c.FetchedAt, LastReportedDate: c.LastReportedDate, Note: note}
	}
	return out
}
