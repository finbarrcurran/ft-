package server

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"ft/internal/snapshot"
)

// snapshotHistory returns the three weeks before weekStart, newest first: the stored snapshot
// where there is one, otherwise a SEED holding only the metrics that existing history tables
// really hold (portfolio value, alerts raised, regimes, thesis scores and lock dates). Nothing
// else is reconstructed or estimated.
func (s *Server) snapshotHistory(ctx context.Context, weekStart string) ([]*snapshot.Payload, []string, error) {
	ws, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return nil, nil, err
	}
	var out []*snapshot.Payload
	seededKeys := map[string]bool{}
	stored, err := snapshot.Before(ctx, s.store.DB, weekStart, 3)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]*snapshot.Payload{}
	for _, p := range stored {
		have[p.WeekStart] = p
	}
	for k := 1; k <= 3; k++ {
		w := ws.AddDate(0, 0, -7*k)
		wk := w.Format("2006-01-02")
		if p, ok := have[wk]; ok {
			out = append(out, p)
			continue
		}
		seed, err := s.seedPayload(ctx, wk)
		if err != nil {
			return nil, nil, err
		}
		if len(seed.Metrics)+len(seed.Texts)+len(seed.Sets) > 0 {
			out = append(out, seed)
			for key := range seed.Metrics {
				seededKeys[key] = true
			}
			for key := range seed.Texts {
				seededKeys[key] = true
			}
			for key := range seed.Sets {
				seededKeys[key] = true
			}
		}
	}
	// A stored snapshot older than the three-week window still serves as "previous" when
	// nothing newer exists at all.
	if len(out) == 0 && len(stored) > 0 {
		out = append(out, stored[0])
	}
	// Order newest first by week.
	sort.SliceStable(out, func(i, j int) bool { return out[i].WeekStart > out[j].WeekStart })
	keys := make([]string, 0, len(seededKeys))
	for k := range seededKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return out, keys, nil
}

// seedPayload builds the history-table part of a past week, as of that week's Sunday.
func (s *Server) seedPayload(ctx context.Context, weekStart string) (*snapshot.Payload, error) {
	ws, _ := time.Parse("2006-01-02", weekStart)
	sunday := ws.AddDate(0, 0, 6)
	asOf := sunday.Format("2006-01-02")
	endTS := sunday.Add(24*time.Hour - time.Second).Unix()
	p := snapshot.New(weekStart, time.Now())
	db := s.store.DB

	var tot, st, cr sql.NullFloat64
	err := db.QueryRowContext(ctx, `SELECT total_value_usd, stocks_value_usd, crypto_value_usd FROM portfolio_value_history WHERE date <= ? ORDER BY date DESC LIMIT 1`, asOf).Scan(&tot, &st, &cr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("seed portfolio: %w", err)
	}
	if err == nil && tot.Valid {
		p.Metrics["portfolio.total_value_usd"] = tot.Float64
		p.Metrics["portfolio.stocks_value_usd"] = st.Float64
		p.Metrics["portfolio.crypto_value_usd"] = cr.Float64
	}

	counts, err := alertCounts7d(ctx, db, sunday)
	if err != nil {
		return nil, fmt.Errorf("seed alerts: %w", err)
	}
	total := 0
	for k, n := range counts {
		p.Metrics["alerts.7d."+k] = float64(n)
		total += n
	}
	if len(counts) > 0 {
		p.Metrics["alerts.7d_total"] = float64(total)
	}

	for _, fw := range []string{"jordi", "cowen"} {
		var regime string
		err := db.QueryRowContext(ctx, `SELECT regime FROM regime_history WHERE framework_id = ? AND ts <= ? ORDER BY ts DESC LIMIT 1`, fw, endTS).Scan(&regime)
		if err == nil {
			p.Texts["regime."+fw] = regime
		} else if err != sql.ErrNoRows {
			return nil, fmt.Errorf("seed regime: %w", err)
		}
	}
	var q, sh string
	err = db.QueryRowContext(ctx, `SELECT quadrant, shorthand FROM macro_regime_history WHERE computed_at <= ? ORDER BY computed_at DESC LIMIT 1`, endTS).Scan(&q, &sh)
	if err == nil {
		p.Texts["regime.macro_quadrant"], p.Texts["regime.macro_shorthand"] = q, sh
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("seed macro regime: %w", err)
	}

	// Theses: per ticker the highest version already locked by that Sunday.
	rows, err := db.QueryContext(ctx, `
		SELECT t.ticker, t.score FROM theses_index t
		 WHERE t.locked_date IS NOT NULL AND t.locked_date <= ? AND t.status IN ('locked','superseded')
		   AND t.version = (SELECT MAX(v.version) FROM theses_index v WHERE v.ticker = t.ticker AND v.locked_date IS NOT NULL AND v.locked_date <= ? AND v.status IN ('locked','superseded'))`, asOf, asOf)
	if err != nil {
		return nil, fmt.Errorf("seed theses: %w", err)
	}
	defer rows.Close()
	locked := []string{}
	for rows.Next() {
		var tk string
		var sc sql.NullInt64
		if err := rows.Scan(&tk, &sc); err != nil {
			return nil, err
		}
		locked = append(locked, tk)
		if sc.Valid {
			p.Metrics["thesis.score."+tk] = float64(sc.Int64)
		}
	}
	if len(locked) > 0 {
		sort.Strings(locked)
		p.Sets["theses.locked"] = locked
		p.Metrics["theses.locked_count"] = float64(len(locked))
	}
	return p, rows.Err()
}
