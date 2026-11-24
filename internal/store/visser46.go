package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"
)

// Jordi-46 Part B storage: the per-endpoint CoinGecko call counters, the monthly keyed total the soft stop
// reads, the preference switches, and the /coins/markets snapshot of the index tokens.

// Preference keys of Part B.
const (
	PrefPartBEnabled  = "coingecko_partb_enabled"   // 'on' | 'off'; the kill switch, default off
	PrefPartBSoftStop = "coingecko_partb_soft_stop" // keyed calls per month; default 7000
	DefaultSoftStop   = 7000
)

// PartBEnabled reads the kill switch. Fail closed: anything but exactly 'on' (missing row, read error,
// stray value) is off.
func (s *Store) PartBEnabled(ctx context.Context) bool {
	v, err := s.GetPreference(ctx, PrefPartBEnabled)
	return err == nil && v == "on"
}

// PartBSoftStop reads the monthly keyed-call soft stop; a missing or invalid value gives the default.
func (s *Store) PartBSoftStop(ctx context.Context) int {
	v, err := s.GetPreference(ctx, PrefPartBSoftStop)
	if err != nil {
		return DefaultSoftStop
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return DefaultSoftStop
	}
	return n
}

// RecordCoinGeckoCall counts one CoinGecko attempt under its endpoint for today (UTC). Errors are returned
// for the caller to log; bookkeeping must never break a call path.
func (s *Store) RecordCoinGeckoCall(ctx context.Context, endpoint, feature string, keyed, ok, is429 bool) error {
	return s.recordCoinGeckoCallOn(ctx, time.Now().UTC(), endpoint, feature, keyed, ok, is429)
}

func (s *Store) recordCoinGeckoCallOn(ctx context.Context, at time.Time, endpoint, feature string, keyed, ok, is429 bool) error {
	k, okN, failN, f429 := 0, 0, 0, 0
	if keyed {
		k = 1
	}
	if ok {
		okN = 1
	} else {
		failN = 1
		if is429 {
			f429 = 1
		}
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO coingecko_calls (day, endpoint, feature, keyed, calls, ok, fail, fail_429)
		VALUES (?, ?, ?, ?, 1, ?, ?, ?)
		ON CONFLICT(day, endpoint, feature, keyed) DO UPDATE SET
		  calls = calls + 1, ok = ok + excluded.ok, fail = fail + excluded.fail, fail_429 = fail_429 + excluded.fail_429`,
		at.Format("2006-01-02"), endpoint, feature, k, okN, failN, f429)
	return err
}

// CoinGeckoMonthKeyed is the number of keyed CoinGecko calls so far in the UTC month of `at`. It reads
// provider_daily (provider 'coingecko_key', counted since v1.98.0), which is the same counter the Demo
// key's 10,000 a month allowance is judged by, so a deploy in mid-month does not reset it.
func (s *Store) CoinGeckoMonthKeyed(ctx context.Context, at time.Time) (int, error) {
	first := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(calls),0) FROM provider_daily WHERE provider = 'coingecko_key' AND day >= ? AND day < ?`,
		first.Format("2006-01-02"), next.Format("2006-01-02")).Scan(&n)
	return n, err
}

// EndpointUsage is one endpoint's calls over a period.
type EndpointUsage struct {
	Endpoint string `json:"endpoint"`
	Feature  string `json:"feature"`
	Keyed    bool   `json:"keyed"`
	Calls    int    `json:"calls"`
	OK       int    `json:"ok"`
	Fail     int    `json:"fail"`
	Fail429  int    `json:"fail429"`
}

// CoinGeckoEndpointUsage sums the per-endpoint counters for days in [from, to) (UTC 'YYYY-MM-DD').
func (s *Store) CoinGeckoEndpointUsage(ctx context.Context, from, to string) ([]EndpointUsage, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT endpoint, feature, keyed, SUM(calls), SUM(ok), SUM(fail), SUM(fail_429) FROM coingecko_calls
		WHERE day >= ? AND day < ? GROUP BY endpoint, feature, keyed ORDER BY keyed DESC, feature, endpoint`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EndpointUsage{}
	for rows.Next() {
		var u EndpointUsage
		var k int
		if err := rows.Scan(&u.Endpoint, &u.Feature, &k, &u.Calls, &u.OK, &u.Fail, &u.Fail429); err != nil {
			return nil, err
		}
		u.Keyed = k == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

// IndexSnapshot is one token's latest /coins/markets row.
type IndexSnapshot struct {
	CoinGeckoID string   `json:"coingeckoId"`
	Symbol      string   `json:"symbol"`
	FetchedAt   int64    `json:"fetchedAt"`
	PriceUSD    *float64 `json:"priceUsd"`
	MarketCap   *float64 `json:"marketCap"`
	MCapRank    *int     `json:"marketCapRank"`
	Chg24h      *float64 `json:"change24h"`
	Chg7d       *float64 `json:"change7d"`
	Chg30d      *float64 `json:"change30d"`
}

// UpsertIndexSnapshots replaces the stored snapshot of every row given, in one transaction.
func (s *Store) UpsertIndexSnapshots(ctx context.Context, rows []IndexSnapshot) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `
		INSERT INTO crypto_index_snapshot (coingecko_id, symbol, fetched_at, price_usd, market_cap, mcap_rank, chg_24h, chg_7d, chg_30d)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(coingecko_id) DO UPDATE SET symbol=excluded.symbol, fetched_at=excluded.fetched_at,
		  price_usd=excluded.price_usd, market_cap=excluded.market_cap, mcap_rank=excluded.mcap_rank,
		  chg_24h=excluded.chg_24h, chg_7d=excluded.chg_7d, chg_30d=excluded.chg_30d`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rows {
		if _, err := st.ExecContext(ctx, r.CoinGeckoID, r.Symbol, r.FetchedAt, r.PriceUSD, r.MarketCap, r.MCapRank, r.Chg24h, r.Chg7d, r.Chg30d); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListIndexSnapshots returns every stored snapshot row keyed by CoinGecko id.
func (s *Store) ListIndexSnapshots(ctx context.Context) (map[string]IndexSnapshot, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT coingecko_id, symbol, fetched_at, price_usd, market_cap, mcap_rank, chg_24h, chg_7d, chg_30d FROM crypto_index_snapshot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]IndexSnapshot{}
	for rows.Next() {
		var r IndexSnapshot
		if err := rows.Scan(&r.CoinGeckoID, &r.Symbol, &r.FetchedAt, &r.PriceUSD, &r.MarketCap, &r.MCapRank, &r.Chg24h, &r.Chg7d, &r.Chg30d); err != nil {
			return nil, err
		}
		out[r.CoinGeckoID] = r
	}
	return out, rows.Err()
}

// NewestIndexSnapshotAt returns the newest fetched_at (0 when the table is empty).
func (s *Store) NewestIndexSnapshotAt(ctx context.Context) (int64, error) {
	var v sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(fetched_at) FROM crypto_index_snapshot`).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// CountPriceHistory returns how many closes are stored for (ticker, kind) and the newest date.
func (s *Store) CountPriceHistory(ctx context.Context, ticker, kind string) (n int, newest string, err error) {
	var d sql.NullString
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*), MAX(date) FROM price_history WHERE ticker = ? AND kind = ?`, ticker, kind).Scan(&n, &d)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return n, d.String, err
}

// ListPriceHistory returns every stored close for (ticker, kind), oldest first.
func (s *Store) ListPriceHistory(ctx context.Context, ticker, kind string) ([]PricePoint, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT date, close FROM price_history WHERE ticker = ? AND kind = ? ORDER BY date`, ticker, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.Date, &p.Close); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CoinGeckoFeatureCallsOn returns the CoinGecko calls recorded for a feature on a UTC day ('YYYY-MM-DD'), any key state.
func (s *Store) CoinGeckoFeatureCallsOn(ctx context.Context, day, feature string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(calls),0) FROM coingecko_calls WHERE day = ? AND feature = ?`, day, feature).Scan(&n)
	return n, err
}
