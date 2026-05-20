package cryptoindicators

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Service is the entry point for the crypto-indicators package. Owns
// DB access; pure scoring/composite math is in scoring.go / composite.go.
type Service struct {
	DB *sql.DB
}

// New constructs a Service.
func New(db *sql.DB) *Service { return &Service{DB: db} }

// IndicatorRow is the API shape — one row per indicator, latest reading.
type IndicatorRow struct {
	ID            string   `json:"id"`
	Bucket        string   `json:"bucket"`
	DisplayName   string   `json:"displayName"`
	Unit          string   `json:"unit,omitempty"`
	Source        string   `json:"source"`
	CurrentValue  *float64 `json:"currentValue,omitempty"`
	CurrentScore  *float64 `json:"currentScore,omitempty"`
	Trend4w       *float64 `json:"trend4w,omitempty"`
	UpdatedAt     *int64   `json:"updatedAt,omitempty"`
	FetchError    string   `json:"fetchError,omitempty"`
	Tooltip       string   `json:"tooltip"`
}

// CompositeSnapshot mirrors the crypto_composite_snapshots row shape +
// effective weights map. Returned by GET .../composite/latest.
type CompositeSnapshot struct {
	SnapshotDate      string             `json:"snapshotDate"`
	CompositeScore    float64            `json:"compositeScore"`
	CowenSubscore     *float64           `json:"cowenSubscore,omitempty"`
	PalSubscore       *float64           `json:"palSubscore,omitempty"`
	UniversalSubscore *float64           `json:"universalSubscore,omitempty"`
	SentimentSubscore *float64           `json:"sentimentSubscore,omitempty"`
	ActionBand        string             `json:"actionBand"`
	BTCPriceUSD       *float64           `json:"btcPriceUsd,omitempty"`
	Notes             string             `json:"notes,omitempty"`
	EffectiveWeights  map[string]float64 `json:"effectiveWeights,omitempty"`
}

// ListIndicators returns the latest reading for every indicator, joined
// with the embedded definition (for display_name, unit, tooltip).
// Ordered by bucket → display_name for stable rendering.
func (s *Service) ListIndicators(ctx context.Context) ([]IndicatorRow, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, bucket, display_name, unit, source,
		       current_value, current_score, trend_4w, updated_at,
		       COALESCE(fetch_error, '')
		  FROM crypto_indicators
		 ORDER BY CASE bucket
		            WHEN 'cowen' THEN 1
		            WHEN 'pal' THEN 2
		            WHEN 'universal' THEN 3
		            WHEN 'sentiment' THEN 4
		            ELSE 5 END,
		          display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IndicatorRow{}
	for rows.Next() {
		var r IndicatorRow
		var unit sql.NullString
		var cv, cs, tr sql.NullFloat64
		var ua sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Bucket, &r.DisplayName, &unit, &r.Source,
			&cv, &cs, &tr, &ua, &r.FetchError); err != nil {
			return nil, err
		}
		if unit.Valid {
			r.Unit = unit.String
		}
		if cv.Valid {
			v := cv.Float64
			r.CurrentValue = &v
		}
		if cs.Valid {
			v := cs.Float64
			r.CurrentScore = &v
		}
		if tr.Valid {
			v := tr.Float64
			r.Trend4w = &v
		}
		if ua.Valid {
			v := ua.Int64
			r.UpdatedAt = &v
		}
		r.Tooltip = TooltipFor(r.ID)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestComposite returns the most recent composite snapshot, computed
// live from current indicator readings (NOT from
// crypto_composite_snapshots — that table is for the daily backtest
// trail). If no indicators have data yet, returns a zero composite with
// notes explaining the empty state.
func (s *Service) LatestComposite(ctx context.Context) (*CompositeSnapshot, error) {
	indicators, err := s.ListIndicators(ctx)
	if err != nil {
		return nil, err
	}

	active := []ActiveScore{}
	for _, r := range indicators {
		if r.CurrentScore == nil {
			continue
		}
		if r.FetchError != "" {
			continue // exclude errored
		}
		active = append(active, ActiveScore{
			IndicatorID: r.ID,
			Bucket:      r.Bucket,
			Score:       *r.CurrentScore,
		})
	}

	weights, err := s.loadWeights(ctx)
	if err != nil {
		return nil, err
	}

	res := ComputeComposite(active, weights)

	snap := &CompositeSnapshot{
		SnapshotDate:     time.Now().UTC().Format("2006-01-02"),
		CompositeScore:   res.Composite,
		ActionBand:       res.ActionBand,
		EffectiveWeights: res.EffectiveWeights,
	}
	if v := res.SubScores["cowen"]; v != nil {
		x := roundTo(*v, 2)
		snap.CowenSubscore = &x
	}
	if v := res.SubScores["pal"]; v != nil {
		x := roundTo(*v, 2)
		snap.PalSubscore = &x
	}
	if v := res.SubScores["universal"]; v != nil {
		x := roundTo(*v, 2)
		snap.UniversalSubscore = &x
	}
	if v := res.SubScores["sentiment"]; v != nil {
		x := roundTo(*v, 2)
		snap.SentimentSubscore = &x
	}
	if len(active) == 0 {
		snap.Notes = "No indicator data yet — Phase 2 data providers (FRED, DefiLlama, Farside, ISM, Cowen log-band) ship in v1.8.1. Composite stays at 0/neutral until then."
	}
	return snap, nil
}

func (s *Service) loadWeights(ctx context.Context) (Weights, error) {
	w := DefaultWeights()
	row := s.DB.QueryRowContext(ctx,
		`SELECT cowen_weight, pal_weight, universal_weight, sentiment_weight
		   FROM crypto_indicator_weights WHERE id = 1`)
	if err := row.Scan(&w.Cowen, &w.Pal, &w.Universal, &w.Sentiment); err != nil {
		if err == sql.ErrNoRows {
			return DefaultWeights(), nil
		}
		return DefaultWeights(), err
	}
	return w, nil
}

// UpsertIndicatorReading writes a single indicator's latest value +
// score back into crypto_indicators. Used by Phase 2 cron once
// providers are wired. Phase 1 callers: tests only.
func (s *Service) UpsertIndicatorReading(ctx context.Context, id string, value, score, trend *float64, fetchErr string) error {
	now := time.Now().UTC().Unix()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE crypto_indicators
		   SET current_value = ?, current_score = ?, trend_4w = ?,
		       updated_at = ?, fetch_error = ?
		 WHERE id = ?`,
		nullFloat(value), nullFloat(score), nullFloat(trend),
		now, nullStr(fetchErr), id)
	return err
}

// WriteDailySnapshot appends today's per-indicator snapshots and the
// composite snapshot in one go. Used by Phase 2 daily cron at 00:30 UTC.
// Idempotent via INSERT OR REPLACE keyed on (date, id) and date.
func (s *Service) WriteDailySnapshot(ctx context.Context, snapshotDate string, btcPrice *float64) error {
	indicators, err := s.ListIndicators(ctx)
	if err != nil {
		return fmt.Errorf("list indicators: %w", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, r := range indicators {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO crypto_indicator_snapshots
			  (snapshot_date, indicator_id, raw_value, score)
			VALUES (?, ?, ?, ?)`,
			snapshotDate, r.ID, nullFloatPtr(r.CurrentValue), nullFloatPtr(r.CurrentScore)); err != nil {
			return fmt.Errorf("snapshot %s: %w", r.ID, err)
		}
	}

	composite, err := s.LatestComposite(ctx)
	if err != nil {
		return fmt.Errorf("compute composite for snapshot: %w", err)
	}

	weightsJSON, _ := json.Marshal(composite.EffectiveWeights)
	notes := string(weightsJSON)
	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO crypto_composite_snapshots
		  (snapshot_date, composite_score, cowen_subscore, pal_subscore,
		   universal_subscore, sentiment_subscore, action_band,
		   btc_price_usd, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshotDate, composite.CompositeScore,
		nullFloatPtr(composite.CowenSubscore), nullFloatPtr(composite.PalSubscore),
		nullFloatPtr(composite.UniversalSubscore), nullFloatPtr(composite.SentimentSubscore),
		composite.ActionBand, nullFloatPtr(btcPrice), notes)
	if err != nil {
		return fmt.Errorf("write composite snapshot: %w", err)
	}
	return tx.Commit()
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullFloatPtr(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
