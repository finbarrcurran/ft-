package store

import (
	"context"
	"database/sql"
	"ft/internal/domain"
)

// SC-36 AI Nexus store layer — universe membership, the symbol bridge, and the
// three snapshot tables (technical / exhaustion / fundamentals). Snapshots are
// keyed (ticker, as_of, source); a same-(as_of,source) re-ingest replaces that
// slice wholesale (delete-then-insert in one tx), a new date appends.

// --- nullable scan/bind helpers -------------------------------------------

func nfp(n sql.NullFloat64) *float64 {
	if n.Valid {
		v := n.Float64
		return &v
	}
	return nil
}
func nip(n sql.NullInt64) *int {
	if n.Valid {
		v := int(n.Int64)
		return &v
	}
	return nil
}
func nsp(n sql.NullString) *string {
	if n.Valid {
		v := n.String
		return &v
	}
	return nil
}
func bind(p any) any { // *float64 / *int / *string → value or nil
	switch v := p.(type) {
	case *float64:
		if v == nil {
			return nil
		}
		return *v
	case *int:
		if v == nil {
			return nil
		}
		return *v
	case *string:
		if v == nil {
			return nil
		}
		return *v
	}
	return p
}

// CountDailyBars returns how many daily_bars rows exist for a (ticker, kind).
func (s *Store) CountDailyBars(ctx context.Context, ticker, kind string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM daily_bars WHERE ticker = ? AND kind = ?`, ticker, kind).Scan(&n)
	return n, err
}

// LastDailyBarDate returns the most recent bar date for a (ticker, kind), or ""
// when none exist.
func (s *Store) LastDailyBarDate(ctx context.Context, ticker, kind string) (string, error) {
	var d sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT MAX(date) FROM daily_bars WHERE ticker = ? AND kind = ?`, ticker, kind).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil
}

// --- universe + ticker map -------------------------------------------------

// CountNexusUniverse returns how many seeded (is_nexus=1) members exist.
func (s *Store) CountNexusUniverse(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM nexus_universe WHERE is_nexus = 1 AND deleted_at IS NULL`).Scan(&n)
	return n, err
}

// BulkUpsertNexusUniverse inserts/updates universe rows (used by the seed and
// future universe edits). company/theme/is_nexus refresh on conflict.
func (s *Store) BulkUpsertNexusUniverse(ctx context.Context, rows []domain.NexusUniverseRow) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nexus_universe (ticker, company, theme, is_nexus, active, added_at)
		VALUES (?, ?, ?, ?, 1, strftime('%s','now'))
		ON CONFLICT(ticker) DO UPDATE SET
		  company  = excluded.company,
		  theme    = excluded.theme,
		  is_nexus = excluded.is_nexus,
		  deleted_at = NULL`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		isNexus := 0
		if r.IsNexus {
			isNexus = 1
		}
		if _, err := stmt.ExecContext(ctx, r.Ticker, r.Company, bind(r.Theme), isNexus); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListNexusUniverse returns active seeded members.
func (s *Store) ListNexusUniverse(ctx context.Context) ([]domain.NexusUniverseRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, company, theme, is_nexus, active
		  FROM nexus_universe WHERE deleted_at IS NULL
		  ORDER BY theme, ticker`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NexusUniverseRow
	for rows.Next() {
		var r domain.NexusUniverseRow
		var theme sql.NullString
		var isNexus, active int
		if err := rows.Scan(&r.Ticker, &r.Company, &theme, &isNexus, &active); err != nil {
			return nil, err
		}
		r.Theme = nsp(theme)
		r.IsNexus = isNexus == 1
		r.Active = active == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetNexusTickerMap returns the source→ft symbol bridge.
func (s *Store) GetNexusTickerMap(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT source_symbol, ft_symbol FROM nexus_ticker_map`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, err
		}
		m[a] = b
	}
	return m, rows.Err()
}

// NexusSnapshotDates returns the distinct as_of dates present for a source in
// one of the nexus snapshot tables (whitelisted).
func (s *Store) NexusSnapshotDates(ctx context.Context, table, source string) ([]string, error) {
	switch table {
	case "nexus_technical", "nexus_exhaustion", "nexus_fundamentals":
	default:
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT DISTINCT as_of FROM `+table+` WHERE source = ? ORDER BY as_of`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// --- technical snapshots ---------------------------------------------------

// ReplaceNexusTechnical wipes then re-inserts the (as_of, source) slice in one tx.
func (s *Store) ReplaceNexusTechnical(ctx context.Context, asOf, source string, rows []domain.NexusTechnical) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM nexus_technical WHERE as_of = ? AND source = ?`, asOf, source); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nexus_technical
		  (ticker, as_of, source, price, trend_score, setup_label, components_json,
		   rsi14, ret_1w, ret_1m, ret_3m, vs_20d, vs_50d, vs_200d, slope_50d, slope_200d,
		   dist_52w_hi, atr_pct, vol_ratio, rs_spy, rs_qqq, rs_rank, monday_note)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.Ticker, asOf, source, bind(r.Price), bind(r.TrendScore), bind(r.SetupLabel), r.Components,
			bind(r.RSI14), bind(r.Ret1W), bind(r.Ret1M), bind(r.Ret3M), bind(r.Vs20D), bind(r.Vs50D), bind(r.Vs200D),
			bind(r.Slope50D), bind(r.Slope200D), bind(r.Dist52WHi), bind(r.ATRPct), bind(r.VolRatio),
			bind(r.RSSpy), bind(r.RSQqq), bind(r.RSRank), bind(r.MondayNote)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestNexusTechnicalAsOf returns the most recent as_of for a source (empty if none).
func (s *Store) LatestNexusTechnicalAsOf(ctx context.Context, source string) (string, error) {
	var d sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT MAX(as_of) FROM nexus_technical WHERE source = ?`, source).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil
}

// ListNexusTechnical returns all rows for an (as_of, source).
func (s *Store) ListNexusTechnical(ctx context.Context, asOf, source string) ([]domain.NexusTechnical, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, price, trend_score, setup_label, rsi14, ret_1w, ret_1m, ret_3m,
		       vs_20d, vs_50d, vs_200d, slope_50d, slope_200d, dist_52w_hi, atr_pct,
		       vol_ratio, rs_spy, rs_qqq, rs_rank, monday_note
		  FROM nexus_technical WHERE as_of = ? AND source = ? ORDER BY trend_score DESC, ticker`, asOf, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NexusTechnical
	for rows.Next() {
		r := domain.NexusTechnical{AsOf: asOf, Source: source}
		var price, rsi, r1w, r1m, r3m, v20, v50, v200, s50, s200, d52, atr, vr, spy, qqq sql.NullFloat64
		var ts, rank sql.NullInt64
		var setup, monday sql.NullString
		if err := rows.Scan(&r.Ticker, &price, &ts, &setup, &rsi, &r1w, &r1m, &r3m,
			&v20, &v50, &v200, &s50, &s200, &d52, &atr, &vr, &spy, &qqq, &rank, &monday); err != nil {
			return nil, err
		}
		r.Price, r.TrendScore, r.SetupLabel = nfp(price), nip(ts), nsp(setup)
		r.RSI14, r.Ret1W, r.Ret1M, r.Ret3M = nfp(rsi), nfp(r1w), nfp(r1m), nfp(r3m)
		r.Vs20D, r.Vs50D, r.Vs200D = nfp(v20), nfp(v50), nfp(v200)
		r.Slope50D, r.Slope200D, r.Dist52WHi = nfp(s50), nfp(s200), nfp(d52)
		r.ATRPct, r.VolRatio, r.RSSpy, r.RSQqq, r.RSRank = nfp(atr), nfp(vr), nfp(spy), nfp(qqq), nip(rank)
		r.MondayNote = nsp(monday)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- exhaustion snapshots --------------------------------------------------

func (s *Store) ReplaceNexusExhaustion(ctx context.Context, asOf, source string, rows []domain.NexusExhaustion) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM nexus_exhaustion WHERE as_of = ? AND source = ?`, asOf, source); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nexus_exhaustion
		  (ticker, as_of, source, price, exh_score, band, components_json,
		   rsi14, rsi5, williams_r, pos_20d, ext_20d_atr, ext_50d_atr, ret_vol_1m, imp_5d_atr,
		   vol_ratio, atr_expansion, td_setup, td_countdown, td_score, atr_pct, ret_1m, ret_5d, data_wt_pct)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.Ticker, asOf, source, bind(r.Price), bind(r.ExhScore), bind(r.Band), r.Components,
			bind(r.RSI14), bind(r.RSI5), bind(r.WilliamsR), bind(r.Pos20D), bind(r.Ext20DATR), bind(r.Ext50DATR),
			bind(r.RetVol1M), bind(r.Imp5DATR), bind(r.VolRatio), bind(r.ATRExpansion),
			bind(r.TDSetup), bind(r.TDCountdown), bind(r.TDScore), bind(r.ATRPct), bind(r.Ret1M), bind(r.Ret5D),
			bind(r.DataWtPct)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LatestNexusExhaustionAsOf(ctx context.Context, source string) (string, error) {
	var d sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT MAX(as_of) FROM nexus_exhaustion WHERE source = ?`, source).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil
}

func (s *Store) ListNexusExhaustion(ctx context.Context, asOf, source string) ([]domain.NexusExhaustion, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, price, exh_score, band, rsi14, rsi5, williams_r, pos_20d, ext_20d_atr,
		       ext_50d_atr, ret_vol_1m, imp_5d_atr, vol_ratio, atr_expansion, td_setup, td_countdown,
		       td_score, atr_pct, ret_1m, ret_5d, data_wt_pct
		  FROM nexus_exhaustion WHERE as_of = ? AND source = ? ORDER BY exh_score DESC, ticker`, asOf, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NexusExhaustion
	for rows.Next() {
		r := domain.NexusExhaustion{AsOf: asOf, Source: source}
		var price, exh, rsi, rsi5, wr, p20, e20, e50, rv, i5, vr, ae, tds, atr, r1m, r5d, dw sql.NullFloat64
		var tset, tcnt sql.NullInt64
		var band sql.NullString
		if err := rows.Scan(&r.Ticker, &price, &exh, &band, &rsi, &rsi5, &wr, &p20, &e20,
			&e50, &rv, &i5, &vr, &ae, &tset, &tcnt, &tds, &atr, &r1m, &r5d, &dw); err != nil {
			return nil, err
		}
		r.Price, r.ExhScore, r.Band = nfp(price), nfp(exh), nsp(band)
		r.RSI14, r.RSI5, r.WilliamsR = nfp(rsi), nfp(rsi5), nfp(wr)
		r.Pos20D, r.Ext20DATR, r.Ext50DATR = nfp(p20), nfp(e20), nfp(e50)
		r.RetVol1M, r.Imp5DATR, r.VolRatio, r.ATRExpansion = nfp(rv), nfp(i5), nfp(vr), nfp(ae)
		r.TDSetup, r.TDCountdown, r.TDScore = nip(tset), nip(tcnt), nfp(tds)
		r.ATRPct, r.Ret1M, r.Ret5D, r.DataWtPct = nfp(atr), nfp(r1m), nfp(r5d), nfp(dw)
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- fundamentals snapshots ------------------------------------------------

func (s *Store) ReplaceNexusFundamentals(ctx context.Context, asOf, source string, rows []domain.NexusFundamentals) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM nexus_fundamentals WHERE as_of = ? AND source = ?`, asOf, source); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nexus_fundamentals
		  (ticker, as_of, source, market_cap, fwd_pe, next_fy_eps_growth, fwd_peg, price,
		   current_fy_eps, next_fy_eps, current_fy_end, next_fy_end, data_status)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.Ticker, asOf, source, bind(r.MarketCap), bind(r.FwdPE), bind(r.NextFYEPSGrowth), bind(r.FwdPEG),
			bind(r.Price), bind(r.CurrentFYEPS), bind(r.NextFYEPS), bind(r.CurrentFYEnd), bind(r.NextFYEnd),
			bind(r.DataStatus)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LatestNexusFundamentalsAsOf(ctx context.Context, source string) (string, error) {
	var d sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT MAX(as_of) FROM nexus_fundamentals WHERE source = ?`, source).Scan(&d)
	if err != nil {
		return "", err
	}
	return d.String, nil
}

func (s *Store) ListNexusFundamentals(ctx context.Context, asOf, source string) ([]domain.NexusFundamentals, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT ticker, market_cap, fwd_pe, next_fy_eps_growth, fwd_peg, price,
		       current_fy_eps, next_fy_eps, current_fy_end, next_fy_end, data_status
		  FROM nexus_fundamentals WHERE as_of = ? AND source = ? ORDER BY fwd_peg, ticker`, asOf, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.NexusFundamentals
	for rows.Next() {
		r := domain.NexusFundamentals{AsOf: asOf, Source: source}
		var mc, pe, g, peg, price, cfe, nfe sql.NullFloat64
		var cfy, nfy, ds sql.NullString
		if err := rows.Scan(&r.Ticker, &mc, &pe, &g, &peg, &price, &cfe, &nfe, &cfy, &nfy, &ds); err != nil {
			return nil, err
		}
		r.MarketCap, r.FwdPE, r.NextFYEPSGrowth, r.FwdPEG = nfp(mc), nfp(pe), nfp(g), nfp(peg)
		r.Price, r.CurrentFYEPS, r.NextFYEPS = nfp(price), nfp(cfe), nfp(nfe)
		r.CurrentFYEnd, r.NextFYEnd, r.DataStatus = nsp(cfy), nsp(nfy), nsp(ds)
		out = append(out, r)
	}
	return out, rows.Err()
}
