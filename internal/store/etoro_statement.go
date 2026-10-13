package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// SC-50 — position-level eToro statement storage.
//
// A statement upload is merged, never replaced:
//   - closed positions and dividend rows are stored once each, keyed by eToro's Position ID
//     (re-importing the same statement, or an overlapping one, adds nothing twice);
//   - the Financial Summary's whole-period lines are stored per statement period; a period
//     contained in an already-imported one is recorded as superseded, a period that contains
//     older ones supersedes them, and a partial overlap is flagged (the overlap's fees and
//     interest may then be counted twice);
//   - the per-year rows the Performance tab reads are re-derived from these tables, but a
//     year is only REPLACED when the imported periods cover it completely, so a narrower
//     statement can no longer erase a wider earlier one.

// EtoroPosition is one closed position.
type EtoroPosition struct {
	PositionID string
	CloseDate  string
	Year       int
	AssetType  string
	IsCopy     bool
	ProfitUSD  float64
	ProfitEUR  float64
}

// EtoroDividend is one dividend row.
type EtoroDividend struct {
	Key        string
	PositionID string
	PayDate    string
	Year       int
	AssetType  string
	USD, EUR   float64
}

// EtoroPeriod is one statement's whole-period Financial Summary figures.
type EtoroPeriod struct {
	RangeStart, RangeEnd                       string
	NetUSD, NetEUR                             float64
	RealisedUSD, RealisedEUR                   float64
	DividendsUSD, DividendsEUR                 float64
	DividendResidualUSD, DividendResidualEUR   float64
	FeesUSD, FeesEUR                           float64
	InterestUSD, InterestEUR                   float64
	RefundsUSD, RefundsEUR                     float64
	OtherIncomeUSD, OtherIncomeEUR             float64
	UnclassifiedUSD, UnclassifiedEUR           float64
	ReconPnLDeltaUSD, ReconPnLDeltaEUR         float64
	ReconNetDeltaUSD, ReconNetDeltaEUR         float64
	FeesByType                                 map[string][2]float64
	LinesJSON                                  string
}

// EtoroStatementData is everything one statement contributes.
type EtoroStatementData struct {
	FileName  string
	Positions []EtoroPosition
	Dividends []EtoroDividend
	Period    EtoroPeriod
}

// EtoroYearOutcome says what happened to one year.
type EtoroYearOutcome struct {
	Year   int    `json:"year"`
	Action string `json:"action"` // replaced | kept_existing | created_partial
	Note   string `json:"note"`
}

// EtoroApplyResult is returned by ApplyEtoroStatement.
type EtoroApplyResult struct {
	PositionsNew   int                `json:"positionsNew"`
	PositionsKnown int                `json:"positionsKnown"`
	DividendsNew   int                `json:"dividendsNew"`
	DividendsKnown int                `json:"dividendsKnown"`
	PeriodAction   string             `json:"periodAction"` // inserted | replaced_identical | contained_in_existing
	Years          []EtoroYearOutcome `json:"years"`
	Warnings       []string           `json:"warnings"`
}

func dayAfter(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// ApplyEtoroStatement merges one statement. See the package comment above.
func (s *Store) ApplyEtoroStatement(ctx context.Context, userID int64, d EtoroStatementData) (*EtoroApplyResult, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().Unix()
	res := &EtoroApplyResult{Years: []EtoroYearOutcome{}, Warnings: []string{}}
	touched := map[int]bool{}

	// 1. Positions, keyed by Position ID.
	for _, p := range d.Positions {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM etoro_closed_positions WHERE user_id = ? AND position_id = ?`, userID, p.PositionID).Scan(&one)
		known := err == nil
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO etoro_closed_positions (user_id, position_id, close_date, year, asset_type, is_copy, profit_usd, profit_eur, source_file, imported_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(user_id, position_id) DO UPDATE SET
			  close_date = excluded.close_date, year = excluded.year, asset_type = excluded.asset_type, is_copy = excluded.is_copy,
			  profit_usd = excluded.profit_usd, profit_eur = excluded.profit_eur, source_file = excluded.source_file, imported_at = excluded.imported_at`,
			userID, p.PositionID, p.CloseDate, p.Year, p.AssetType, boolToInt(p.IsCopy), p.ProfitUSD, p.ProfitEUR, d.FileName, now); err != nil {
			return nil, err
		}
		if known {
			res.PositionsKnown++
		} else {
			res.PositionsNew++
		}
		touched[p.Year] = true
	}
	// 2. Dividends.
	for _, v := range d.Dividends {
		var one int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM etoro_dividend_rows WHERE user_id = ? AND row_key = ?`, userID, v.Key).Scan(&one)
		known := err == nil
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO etoro_dividend_rows (user_id, row_key, position_id, pay_date, year, asset_type, usd, eur, source_file, imported_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(user_id, row_key) DO UPDATE SET
			  pay_date = excluded.pay_date, year = excluded.year, asset_type = excluded.asset_type,
			  usd = excluded.usd, eur = excluded.eur, source_file = excluded.source_file, imported_at = excluded.imported_at`,
			userID, v.Key, v.PositionID, v.PayDate, v.Year, v.AssetType, v.USD, v.EUR, d.FileName, now); err != nil {
			return nil, err
		}
		if known {
			res.DividendsKnown++
		} else {
			res.DividendsNew++
		}
		touched[v.Year] = true
	}

	// 3. The statement period.
	p := d.Period
	if len(p.RangeEnd) >= 4 {
		var ey int
		fmt.Sscanf(p.RangeEnd[:4], "%d", &ey)
		touched[ey] = true
	}
	type per struct {
		id         int64
		start, end string
	}
	var existing []per
	rows, err := tx.QueryContext(ctx, `SELECT id, range_start, range_end FROM etoro_statement_periods WHERE user_id = ? AND superseded = 0`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e per
		if err := rows.Scan(&e.id, &e.start, &e.end); err != nil {
			rows.Close()
			return nil, err
		}
		existing = append(existing, e)
	}
	rows.Close()
	newSuperseded := false
	res.PeriodAction = "inserted"
	for _, e := range existing {
		switch {
		case e.start == p.RangeStart && e.end == p.RangeEnd:
			_, _ = tx.ExecContext(ctx, `UPDATE etoro_statement_periods SET superseded = 1 WHERE id = ?`, e.id)
			res.PeriodAction = "replaced_identical"
		case e.start >= p.RangeStart && e.end <= p.RangeEnd: // new contains old
			_, _ = tx.ExecContext(ctx, `UPDATE etoro_statement_periods SET superseded = 1 WHERE id = ?`, e.id)
		case p.RangeStart >= e.start && p.RangeEnd <= e.end: // old contains new
			newSuperseded = true
			res.PeriodAction = "contained_in_existing"
		case p.RangeStart <= e.end && p.RangeEnd >= e.start: // partial overlap
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"this statement (%s to %s) partly overlaps an earlier one (%s to %s): the closed positions and dividends are de-duplicated by Position ID, but fees, interest, refunds and other income in the overlapping days may be counted twice",
				p.RangeStart, p.RangeEnd, e.start, e.end))
		}
	}
	feesJSON, _ := json.Marshal(p.FeesByType)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO etoro_statement_periods (user_id, range_start, range_end, source_file,
		  net_usd, net_eur, realised_usd, realised_eur, dividends_usd, dividends_eur,
		  dividend_residual_usd, dividend_residual_eur, fees_usd, fees_eur, interest_usd, interest_eur,
		  refunds_usd, refunds_eur, other_income_usd, other_income_eur, unclassified_usd, unclassified_eur,
		  recon_pnl_delta_usd, recon_pnl_delta_eur, recon_net_delta_usd, recon_net_delta_eur,
		  fees_by_type_json, lines_json, superseded, imported_at)
		VALUES (?,?,?,?, ?,?,?,?,?,?, ?,?,?,?,?,?, ?,?,?,?,?,?, ?,?,?,?, ?,?,?,?)`,
		userID, p.RangeStart, p.RangeEnd, d.FileName,
		p.NetUSD, p.NetEUR, p.RealisedUSD, p.RealisedEUR, p.DividendsUSD, p.DividendsEUR,
		p.DividendResidualUSD, p.DividendResidualEUR, p.FeesUSD, p.FeesEUR, p.InterestUSD, p.InterestEUR,
		p.RefundsUSD, p.RefundsEUR, p.OtherIncomeUSD, p.OtherIncomeEUR, p.UnclassifiedUSD, p.UnclassifiedEUR,
		p.ReconPnLDeltaUSD, p.ReconPnLDeltaEUR, p.ReconNetDeltaUSD, p.ReconNetDeltaEUR,
		string(feesJSON), nz(p.LinesJSON, "[]"), boolToInt(newSuperseded), now); err != nil {
		return nil, err
	}

	// 4. Re-derive the touched years.
	years := make([]int, 0, len(touched))
	for y := range touched {
		years = append(years, y)
	}
	sort.Ints(years)
	for _, y := range years {
		out, err := s.rederiveYear(ctx, tx, userID, y, d.FileName, now)
		if err != nil {
			return nil, err
		}
		res.Years = append(res.Years, out)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func nz(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

type interval struct{ start, end string }

// coverage merges the periods' ranges (adjacent days join) and returns the merged intervals.
func mergeIntervals(in []interval) []interval {
	sort.Slice(in, func(i, j int) bool { return in[i].start < in[j].start })
	var out []interval
	for _, v := range in {
		if n := len(out); n > 0 && v.start <= dayAfter(out[n-1].end) {
			if v.end > out[n-1].end {
				out[n-1].end = v.end
			}
			continue
		}
		out = append(out, v)
	}
	return out
}

func (s *Store) rederiveYear(ctx context.Context, tx *sql.Tx, userID int64, y int, fileName string, now int64) (EtoroYearOutcome, error) {
	out := EtoroYearOutcome{Year: y}
	yStart, yEnd := fmt.Sprintf("%d-01-01", y), fmt.Sprintf("%d-12-31", y)
	currentYear := y == time.Now().UTC().Year()

	// Periods (non-superseded), for coverage and the whole-period lines.
	type pr struct {
		start, end string
		fees, feesE, int_, intE, ref, refE, oth, othE, unc, uncE, resU, resE, rU, rE, nU, nE float64
		byType                                                                              string
	}
	prs := []pr{}
	rows, err := tx.QueryContext(ctx, `
		SELECT range_start, range_end, fees_usd, fees_eur, interest_usd, interest_eur, refunds_usd, refunds_eur,
		       other_income_usd, other_income_eur, unclassified_usd, unclassified_eur,
		       dividend_residual_usd, dividend_residual_eur, recon_pnl_delta_usd, recon_pnl_delta_eur,
		       recon_net_delta_usd, recon_net_delta_eur, fees_by_type_json
		  FROM etoro_statement_periods WHERE user_id = ? AND superseded = 0 ORDER BY range_start`, userID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p pr
		var rnU, rnE float64
		if err := rows.Scan(&p.start, &p.end, &p.fees, &p.feesE, &p.int_, &p.intE, &p.ref, &p.refE, &p.oth, &p.othE, &p.unc, &p.uncE,
			&p.resU, &p.resE, &p.rU, &p.rE, &rnU, &rnE, &p.byType); err != nil {
			rows.Close()
			return out, err
		}
		p.nU, p.nE = rnU, rnE
		prs = append(prs, p)
	}
	rows.Close()
	ivs := []interval{}
	for _, p := range prs {
		ivs = append(ivs, interval{p.start, p.end})
	}
	merged := mergeIntervals(ivs)
	covered := false
	var covStart, covEnd string
	for _, m := range merged {
		// A past year is covered when one merged interval spans all of it; the CURRENT year is
		// covered when the imported statements reach back to 1 January (they can only run up to
		// their own end date, which is the YTD figure).
		if m.start <= yStart && (currentYear || m.end >= yEnd) {
			covered = true
		}
		if m.end >= yStart && m.start <= yEnd { // intersects the year
			cs, ce := m.start, m.end
			if cs < yStart {
				cs = yStart
			}
			if ce > yEnd {
				ce = yEnd
			}
			if covStart == "" || cs < covStart {
				covStart = cs
			}
			if ce > covEnd {
				covEnd = ce
			}
		}
	}
	if covStart == "" { // no period touches this year (positions only): use the position dates
		covStart, covEnd = yStart, yEnd
	}

	// Is there a live row, and does it already cover at least what we can?
	var liveStart, liveEnd string
	liveErr := tx.QueryRowContext(ctx, `SELECT range_start, range_end FROM etoro_performance_year WHERE user_id = ? AND year = ? AND superseded = 0`, userID, y).Scan(&liveStart, &liveEnd)
	hasLive := liveErr == nil
	if liveErr != nil && liveErr != sql.ErrNoRows {
		return out, liveErr
	}
	if !covered && hasLive && liveStart <= covStart && liveEnd >= covEnd {
		out.Action = "kept_existing"
		out.Note = fmt.Sprintf("%d is only partly covered by the imported statements (%s to %s) and an existing row (%s to %s) is at least as wide, so it was kept; the positions from this statement are stored, upload the full-year statement to refresh the year", y, covStart, covEnd, liveStart, liveEnd)
		return out, nil
	}

	// Derive the year from the tables.
	type acc struct {
		discU, discE, copyU, copyE, divU, divE, feeU, feeE float64
		n                                                  int
	}
	assets := map[string]*acc{}
	get := func(t string) *acc {
		if assets[t] == nil {
			assets[t] = &acc{}
		}
		return assets[t]
	}
	var cpU, cpE, divU, divE float64
	crow, err := tx.QueryContext(ctx, `SELECT asset_type, is_copy, profit_usd, profit_eur FROM etoro_closed_positions WHERE user_id = ? AND year = ?`, userID, y)
	if err != nil {
		return out, err
	}
	for crow.Next() {
		var t string
		var isCopy int
		var u, e float64
		if err := crow.Scan(&t, &isCopy, &u, &e); err != nil {
			crow.Close()
			return out, err
		}
		a := get(t)
		a.n++
		if isCopy == 1 {
			a.copyU += u
			a.copyE += e
		} else {
			a.discU += u
			a.discE += e
		}
		cpU += u
		cpE += e
	}
	crow.Close()
	drow, err := tx.QueryContext(ctx, `SELECT asset_type, usd, eur FROM etoro_dividend_rows WHERE user_id = ? AND year = ?`, userID, y)
	if err != nil {
		return out, err
	}
	for drow.Next() {
		var t string
		var u, e float64
		if err := drow.Scan(&t, &u, &e); err != nil {
			drow.Close()
			return out, err
		}
		a := get(t)
		a.divU += u
		a.divE += e
		divU += u
		divE += e
	}
	drow.Close()

	var feeU, feeE, intU, intE, refU, refE, othU, othE, uncU, uncE, rpU, rpE, rnU, rnE float64
	for _, p := range prs {
		if len(p.end) < 4 || p.end[:4] != fmt.Sprintf("%d", y) {
			continue // whole-period lines belong to the End Date year
		}
		feeU += p.fees
		feeE += p.feesE
		intU += p.int_
		intE += p.intE
		refU += p.ref
		refE += p.refE
		othU += p.oth
		othE += p.othE
		uncU += p.unc
		uncE += p.uncE
		divU += p.resU
		divE += p.resE
		if p.resU != 0 || p.resE != 0 {
			a := get("CFDs")
			a.divU += p.resU
			a.divE += p.resE
		}
		rpU, rpE = p.rU, p.rE
		rnU, rnE = p.nU, p.nE
		var byType map[string][2]float64
		_ = json.Unmarshal([]byte(p.byType), &byType)
		for t, v := range byType {
			a := get(t)
			a.feeU += v[0]
			a.feeE += v[1]
		}
	}
	netU := cpU + divU + feeU + intU + refU + othU + uncU
	netE := cpE + divE + feeE + intE + refE + othE + uncE

	rangeStart, rangeEnd := covStart, covEnd
	isYTD := rangeEnd < yEnd || y == time.Now().UTC().Year()
	note := ""
	if !covered {
		note = fmt.Sprintf("partial: the imported statements cover %s to %s of %d", covStart, covEnd, y)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE etoro_performance SET superseded = 1 WHERE user_id = ? AND year = ? AND superseded = 0`, userID, y); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE etoro_performance_year SET superseded = 1 WHERE user_id = ? AND year = ? AND superseded = 0`, userID, y); err != nil {
		return out, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO etoro_performance_year (
			user_id, year, range_start, range_end, is_ytd,
			realised_pnl_usd, realised_pnl_eur, dividends_usd, dividends_eur,
			fees_usd, fees_eur, interest_usd, interest_eur, net_usd, net_eur,
			computed_pnl_usd, computed_pnl_eur, recon_delta_usd, recon_delta_eur,
			refunds_usd, refunds_eur, other_income_usd, other_income_eur, unclassified_usd, unclassified_eur,
			recon_net_delta_usd, recon_net_delta_eur, coverage_note,
			superseded, source_file, imported_at
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?,?,?,?, ?,?,?,?, ?,?,?,?,?,?, ?,?,?, 0,?,?)`,
		userID, y, rangeStart, rangeEnd, boolToInt(isYTD),
		cpU, cpE, divU, divE, feeU, feeE, intU, intE, netU, netE,
		cpU, cpE, rpU, rpE,
		refU, refE, othU, othE, uncU, uncE, rnU, rnE, note,
		fileName, now); err != nil {
		return out, err
	}
	types := make([]string, 0, len(assets))
	for t := range assets {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		a := assets[t]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO etoro_performance (user_id, year, asset_type,
				realised_pnl_usd, realised_pnl_eur, realised_disc_usd, realised_disc_eur, realised_copy_usd, realised_copy_eur,
				dividends_usd, dividends_eur, fees_usd, fees_eur, trade_count, superseded, source_file, imported_at)
			VALUES (?,?,?, ?,?, ?,?, ?,?, ?,?,?,?, ?,0,?,?)`,
			userID, y, t, a.discU+a.copyU, a.discE+a.copyE, a.discU, a.discE, a.copyU, a.copyE,
			a.divU, a.divE, a.feeU, a.feeE, a.n, fileName, now); err != nil {
			return out, err
		}
	}
	if covered {
		out.Action = "replaced"
		out.Note = fmt.Sprintf("%d re-derived from the merged positions, dividends and statement periods", y)
	} else if hasLive {
		out.Action = "replaced"
		out.Note = fmt.Sprintf("%d re-derived; coverage is partial (%s to %s) but wider than the row it replaced", y, covStart, covEnd)
	} else {
		out.Action = "created_partial"
		out.Note = note
	}
	return out, nil
}
