package store

import (
	"context"
	"database/sql"

	"ft/internal/domain"
)

// MarketStatsWrite is what one fetch produced (SC-57). ChartOK / CapOK say which half answered: the half that
// failed keeps its previous values and records the error instead.
type MarketStatsWrite struct {
	Ticker, ProviderSymbol, Currency string
	ChartOK, CapOK                   bool
	High52w, Low52w, ATH, Price      *float64
	ATHDate, HistFrom, HistTo        string
	ChartAsOf                        int64
	MarketCap                        *float64
	CapAsOf                          int64
	Err                              string
	Now                              int64
}

const statsSource = "yahoo"

// UpsertMarketStats stores one fetch. A half that answered replaces its columns (including with NULL: a value
// Yahoo stops returning is not kept); a half that failed leaves them alone.
func (s *Store) UpsertMarketStats(ctx context.Context, w MarketStatsWrite) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO ticker_market_stats (ticker, updated_at) VALUES (?, ?)`, w.Ticker, w.Now); err != nil {
		return err
	}
	if w.ChartOK {
		if _, err := s.DB.ExecContext(ctx, `UPDATE ticker_market_stats SET currency = COALESCE(NULLIF(?, ''), currency),
			high_52w = ?, low_52w = ?, ath = ?, ath_date = NULLIF(?, ''), hist_from = NULLIF(?, ''), hist_to = NULLIF(?, ''),
			chart_source = ?, chart_as_of = ?, price = ?, price_as_of = ? WHERE ticker = ?`,
			w.Currency, w.High52w, w.Low52w, w.ATH, w.ATHDate, w.HistFrom, w.HistTo, statsSource, w.ChartAsOf, w.Price, w.ChartAsOf, w.Ticker); err != nil {
			return err
		}
	}
	if w.CapOK {
		if _, err := s.DB.ExecContext(ctx, `UPDATE ticker_market_stats SET market_cap = ?, cap_source = ?, cap_as_of = ? WHERE ticker = ?`,
			w.MarketCap, statsSource, w.CapAsOf, w.Ticker); err != nil {
			return err
		}
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE ticker_market_stats SET provider_symbol = ?, last_error = NULLIF(?, ''),
		error_count = CASE WHEN ? = '' THEN 0 ELSE error_count + 1 END, updated_at = ? WHERE ticker = ?`,
		w.ProviderSymbol, w.Err, w.Err, w.Now, w.Ticker)
	return err
}

// MarketStatsMap returns the API view of every stored row, keyed by ticker.
func (s *Store) MarketStatsMap(ctx context.Context) map[string]*domain.MarketStatsInfo {
	rows, err := s.DB.QueryContext(ctx, `SELECT ticker, currency, high_52w, low_52w, ath, ath_date, hist_from, hist_to,
		chart_source, chart_as_of, market_cap, cap_source, cap_as_of, last_error, price FROM ticker_market_stats`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]*domain.MarketStatsInfo{}
	for rows.Next() {
		var t string
		var cur, athDate, hf, ht, cs, ks, le sql.NullString
		var h, l, ath, mc, px sql.NullFloat64
		var cAsOf, kAsOf sql.NullInt64
		if err := rows.Scan(&t, &cur, &h, &l, &ath, &athDate, &hf, &ht, &cs, &cAsOf, &mc, &ks, &kAsOf, &le, &px); err != nil {
			return out
		}
		m := &domain.MarketStatsInfo{Currency: cur.String}
		fill := func(v *domain.StatValue, f sql.NullFloat64, src sql.NullString, at sql.NullInt64) {
			if !f.Valid {
				return
			}
			x := f.Float64
			v.Value, v.Source = &x, src.String
			if at.Valid {
				a := at.Int64
				v.AsOf = &a
			}
		}
		fill(&m.High52w, h, cs, cAsOf)
		fill(&m.Low52w, l, cs, cAsOf)
		fill(&m.ATH.StatValue, ath, cs, cAsOf)
		fill(&m.MarketCap, mc, ks, kAsOf)
		fill(&m.Price, px, cs, cAsOf)
		if m.ATH.Value != nil {
			m.ATH.Date, m.ATH.HistoryFrom, m.ATH.HistoryTo = nullStrPtr(athDate), nullStrPtr(hf), nullStrPtr(ht)
		}
		if m.ATH.Value != nil && m.High52w.Value != nil && *m.ATH.Value < *m.High52w.Value {
			m.Note = "all-time high comes from a history shorter than 52 weeks (below the 52-week high)"
		}
		if le.Valid {
			if m.Note != "" {
				m.Note += "; "
			}
			m.Note += "last fetch: " + le.String
		}
		out[t] = m
	}
	return out
}

func nullStrPtr(n sql.NullString) *string {
	if !n.Valid || n.String == "" {
		return nil
	}
	v := n.String
	return &v
}
