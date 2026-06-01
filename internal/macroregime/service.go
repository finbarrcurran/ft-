package macroregime

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Service is the DB access layer for the macro-regime tables (migration 0037).
type Service struct {
	DB *sql.DB
}

// New constructs a Service from a shared *sql.DB handle.
func New(db *sql.DB) *Service { return &Service{DB: db} }

// UpsertIndicator writes/replaces the latest reading for one series.
func (s *Service) UpsertIndicator(ctx context.Context, ind Indicator) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO macro_indicators
		  (series_id, fred_id, name, source, axis, grp, value, prior, roc, direction, as_of, fetch_error, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(series_id) DO UPDATE SET
		  fred_id=excluded.fred_id, name=excluded.name, source=excluded.source,
		  axis=excluded.axis, grp=excluded.grp, value=excluded.value, prior=excluded.prior,
		  roc=excluded.roc, direction=excluded.direction, as_of=excluded.as_of,
		  fetch_error=excluded.fetch_error, updated_at=excluded.updated_at`,
		ind.SeriesID, ind.FREDID, ind.Name, ind.Source, ind.Axis, ind.Group,
		ind.Value, ind.Prior, ind.RoC, ind.Direction, ind.AsOf, ind.FetchError, time.Now().Unix())
	return err
}

// ListIndicators returns every macro_indicators row keyed by series_id.
func (s *Service) ListIndicators(ctx context.Context) (map[string]Indicator, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT series_id, fred_id, name, source, axis, grp, value, prior, roc,
		       COALESCE(direction,''), COALESCE(as_of,''), COALESCE(fetch_error,''), updated_at
		FROM macro_indicators`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Indicator{}
	for rows.Next() {
		var i Indicator
		if err := rows.Scan(&i.SeriesID, &i.FREDID, &i.Name, &i.Source, &i.Axis, &i.Group,
			&i.Value, &i.Prior, &i.RoC, &i.Direction, &i.AsOf, &i.FetchError, &i.UpdatedAt); err != nil {
			return nil, err
		}
		out[i.SeriesID] = i
	}
	return out, rows.Err()
}

// ListIndicatorsWithHistory returns indicators in canonical Series order, each
// carrying up to `days` of recent snapshot values for a sparkline.
func (s *Service) ListIndicatorsWithHistory(ctx context.Context, days int) ([]Indicator, error) {
	byID, err := s.ListIndicators(ctx)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 90
	}
	out := make([]Indicator, 0, len(Series))
	for _, d := range Series {
		ind, ok := byID[d.ID]
		if !ok {
			ind = Indicator{SeriesID: d.ID, FREDID: d.FREDID, Name: d.Name, Source: "FRED", Axis: d.Axis, Group: d.Group}
		}
		hist, _ := s.indicatorHistory(ctx, d.ID, days)
		ind.History = hist
		out = append(out, ind)
	}
	return out, nil
}

func (s *Service) indicatorHistory(ctx context.Context, seriesID string, days int) ([]float64, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT value FROM macro_indicator_snapshots
		WHERE series_id = ? AND value IS NOT NULL
		ORDER BY snapshot_date DESC LIMIT ?`, seriesID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var desc []float64
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		desc = append(desc, v)
	}
	// reverse → oldest first for the sparkline
	out := make([]float64, len(desc))
	for i := range desc {
		out[len(desc)-1-i] = desc[i]
	}
	return out, rows.Err()
}

// WriteSnapshot appends today's macro_indicator_snapshots from current readings.
func (s *Service) WriteSnapshot(ctx context.Context, snapshotDate string) error {
	byID, err := s.ListIndicators(ctx)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ind := range byID {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO macro_indicator_snapshots (snapshot_date, series_id, value, roc)
			VALUES (?,?,?,?)
			ON CONFLICT(snapshot_date, series_id) DO UPDATE SET value=excluded.value, roc=excluded.roc`,
			snapshotDate, ind.SeriesID, ind.Value, ind.RoC); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// WriteRegime appends a macro_regime_history row only when the regime state
// (quadrant or any augmenting field) differs from the latest, to avoid bloat.
// Returns true if a new row was written.
func (s *Service) WriteRegime(ctx context.Context, st RegimeState) (bool, error) {
	if prev, ok, _ := s.LatestRegime(ctx); ok && !regimeChanged(prev, st) {
		return false, nil
	}
	flags, _ := json.Marshal(st.ThematicFlags)
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO macro_regime_history
		  (quadrant, shorthand, growth_dir, inflation_dir, rates_regime, liquidity_regime,
		   curve_regime, credit_regime, dollar_regime, confidence, thematic_flags_json,
		   growth_momentum, inflation_momentum, suggested_jordi, computed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		st.Quadrant, st.Shorthand, st.GrowthDir, st.InflationDir, st.RatesRegime, st.LiquidityRegime,
		st.CurveRegime, st.CreditRegime, st.DollarRegime, st.Confidence, string(flags),
		st.GrowthMomentum, st.InflationMomentum, st.SuggestedJordi, st.ComputedAt)
	if err != nil {
		return false, err
	}
	return true, nil
}

func regimeChanged(a, b RegimeState) bool {
	return a.Quadrant != b.Quadrant ||
		a.RatesRegime != b.RatesRegime ||
		a.LiquidityRegime != b.LiquidityRegime ||
		a.CurveRegime != b.CurveRegime ||
		a.CreditRegime != b.CreditRegime ||
		a.DollarRegime != b.DollarRegime ||
		a.Confidence != b.Confidence ||
		a.SuggestedJordi != b.SuggestedJordi
}

// LatestRegime returns the most recent regime row (current state).
func (s *Service) LatestRegime(ctx context.Context) (RegimeState, bool, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT quadrant, shorthand, growth_dir, inflation_dir,
		       COALESCE(rates_regime,''), COALESCE(liquidity_regime,''), COALESCE(curve_regime,''),
		       COALESCE(credit_regime,''), COALESCE(dollar_regime,''), COALESCE(confidence,''),
		       COALESCE(thematic_flags_json,'[]'), growth_momentum, inflation_momentum,
		       COALESCE(suggested_jordi,''), computed_at
		FROM macro_regime_history ORDER BY computed_at DESC, id DESC LIMIT 1`)
	var st RegimeState
	var flags string
	err := row.Scan(&st.Quadrant, &st.Shorthand, &st.GrowthDir, &st.InflationDir,
		&st.RatesRegime, &st.LiquidityRegime, &st.CurveRegime, &st.CreditRegime, &st.DollarRegime,
		&st.Confidence, &flags, &st.GrowthMomentum, &st.InflationMomentum, &st.SuggestedJordi, &st.ComputedAt)
	if err == sql.ErrNoRows {
		return RegimeState{}, false, nil
	}
	if err != nil {
		return RegimeState{}, false, err
	}
	_ = json.Unmarshal([]byte(flags), &st.ThematicFlags)
	if st.ThematicFlags == nil {
		st.ThematicFlags = []string{}
	}
	return st, true, nil
}

// ListPlaybook returns playbook rows for a regime (all if regimeKey == "").
func (s *Service) ListPlaybook(ctx context.Context, regimeKey string) ([]PlaybookRow, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if regimeKey == "" {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, regime_key, asset_or_sector, stance, COALESCE(rationale,''), COALESCE(source,''), sort_order
			FROM regime_playbook ORDER BY regime_key, sort_order`)
	} else {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, regime_key, asset_or_sector, stance, COALESCE(rationale,''), COALESCE(source,''), sort_order
			FROM regime_playbook WHERE regime_key = ? ORDER BY sort_order`, regimeKey)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlaybookRow{}
	for rows.Next() {
		var p PlaybookRow
		if err := rows.Scan(&p.ID, &p.RegimeKey, &p.AssetOrSector, &p.Stance, &p.Rationale, &p.Source, &p.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPlaybookRow inserts (id==0) or updates a doctrine row.
func (s *Service) UpsertPlaybookRow(ctx context.Context, p PlaybookRow) (int64, error) {
	if p.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `
			INSERT INTO regime_playbook (regime_key, asset_or_sector, stance, rationale, source, sort_order)
			VALUES (?,?,?,?,?,?)`, p.RegimeKey, p.AssetOrSector, p.Stance, p.Rationale, p.Source, p.SortOrder)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE regime_playbook SET regime_key=?, asset_or_sector=?, stance=?, rationale=?, source=?, sort_order=?
		WHERE id=?`, p.RegimeKey, p.AssetOrSector, p.Stance, p.Rationale, p.Source, p.SortOrder, p.ID)
	return p.ID, err
}

// DeletePlaybookRow removes a doctrine row by id.
func (s *Service) DeletePlaybookRow(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM regime_playbook WHERE id = ?`, id)
	return err
}

// SetISMManual records a manual ISM headline override.
func (s *Service) SetISMManual(ctx context.Context, value float64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO ism_manual (value, entered_at) VALUES (?,?)`, value, time.Now().Unix())
	return err
}

// LatestISM returns the most recent manual ISM override + freshness.
func (s *Service) LatestISM(ctx context.Context) (ISMStatus, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT value, entered_at FROM ism_manual ORDER BY entered_at DESC, id DESC LIMIT 1`)
	var st ISMStatus
	var v float64
	var at int64
	err := row.Scan(&v, &at)
	if err == sql.ErrNoRows {
		return ISMStatus{}, nil
	}
	if err != nil {
		return ISMStatus{}, err
	}
	st.Value = &v
	st.EnteredAt = at
	st.Fresh = time.Since(time.Unix(at, 0)) <= ISMStaleDays*24*time.Hour
	return st, nil
}
