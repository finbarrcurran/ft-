package cryptoindicators

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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
	ID           string         `json:"id"`
	Bucket       string         `json:"bucket"`
	DisplayName  string         `json:"displayName"`
	Unit         string         `json:"unit,omitempty"`
	Source       string         `json:"source"`
	CurrentValue *float64       `json:"currentValue,omitempty"`
	CurrentScore *float64       `json:"currentScore,omitempty"`
	Trend4w      *float64       `json:"trend4w,omitempty"`
	UpdatedAt    *int64         `json:"updatedAt,omitempty"`
	FetchError   string         `json:"fetchError,omitempty"`
	Tooltip      string         `json:"tooltip"`
	History      []HistoryPoint `json:"history,omitempty"` // last 30 daily snapshots, oldest first
}

// HistoryPoint is one day of snapshot history per indicator. Used by the
// frontend to render per-card sparklines.
type HistoryPoint struct {
	Date  string   `json:"date"`            // ISO YYYY-MM-DD
	Value *float64 `json:"value,omitempty"` // raw_value from crypto_indicator_snapshots
	Score *float64 `json:"score,omitempty"` // score from crypto_indicator_snapshots
}

// BTCHistoryPoint is one day of BTC closes used by the BTC log-band chart
// at the top of the cowen bucket.
type BTCHistoryPoint struct {
	Date  string  `json:"date"`     // ISO YYYY-MM-DD
	Close float64 `json:"closeUsd"` // USD close price
}

// CompositeHistoryPoint is one day of composite + sub-scores used by the
// hero gauge trend on the crypto-indicators tab.
type CompositeHistoryPoint struct {
	Date           string   `json:"date"`
	CompositeScore float64  `json:"compositeScore"`
	BTCPriceUSD    *float64 `json:"btcPriceUsd,omitempty"`
	ActionBand     string   `json:"actionBand"`
}

// ETFFlowPoint is one day of BTC spot-ETF aggregate net flow in USD
// millions. Sourced from the Playwright-scraped Farside JSON cache at
// /var/lib/ft/data/farside/etf-flow.json. Used by the ETF flow bar chart
// at the top of the universal bucket.
type ETFFlowPoint struct {
	Date   string  `json:"date"`   // ISO YYYY-MM-DD
	TotalM float64 `json:"totalM"` // USD millions; negative = net outflow
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Attach 30 days of snapshot history per indicator for sparklines.
	if err := s.attachHistory(ctx, out, 30); err != nil {
		// Non-fatal — sparklines just won't render. Log via the caller
		// since Service has no logger; the API client can still use
		// current values.
		_ = err
	}
	return out, nil
}

// attachHistory loads the last `days` of snapshot history for every row
// in `rows` and attaches it to each .History field. Single query batched
// across all indicators to keep this O(1) HTTP per ListIndicators call.
func (s *Service) attachHistory(ctx context.Context, rows []IndicatorRow, days int) error {
	if len(rows) == 0 || days < 2 {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	q, err := s.DB.QueryContext(ctx, `
		SELECT indicator_id, snapshot_date, raw_value, score
		  FROM crypto_indicator_snapshots
		 WHERE snapshot_date >= ?
		 ORDER BY indicator_id, snapshot_date ASC`, cutoff)
	if err != nil {
		return err
	}
	defer q.Close()
	byID := map[string][]HistoryPoint{}
	for q.Next() {
		var id, date string
		var raw, score sql.NullFloat64
		if err := q.Scan(&id, &date, &raw, &score); err != nil {
			return err
		}
		hp := HistoryPoint{Date: date}
		if raw.Valid {
			v := raw.Float64
			hp.Value = &v
		}
		if score.Valid {
			v := score.Float64
			hp.Score = &v
		}
		byID[id] = append(byID[id], hp)
	}
	for i := range rows {
		rows[i].History = byID[rows[i].ID]
	}
	return q.Err()
}

// BTCPriceHistory returns the most recent `days` of BTC daily closes
// from btc_price_history. Used by the BTC log-band chart at the top of
// the cowen bucket. Empty slice if seed hasn't run yet.
func (s *Service) BTCPriceHistory(ctx context.Context, days int) ([]BTCHistoryPoint, error) {
	if days < 1 {
		days = 730
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := s.DB.QueryContext(ctx,
		`SELECT snapshot_date, close_usd
		   FROM btc_price_history
		  WHERE snapshot_date >= ?
		  ORDER BY snapshot_date ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BTCHistoryPoint{}
	for rows.Next() {
		var p BTCHistoryPoint
		if err := rows.Scan(&p.Date, &p.Close); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CompositeHistory returns the most recent `days` of composite snapshots
// for the hero gauge trend display.
func (s *Service) CompositeHistory(ctx context.Context, days int) ([]CompositeHistoryPoint, error) {
	if days < 1 {
		days = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	rows, err := s.DB.QueryContext(ctx,
		`SELECT snapshot_date, composite_score, btc_price_usd, action_band
		   FROM crypto_composite_snapshots
		  WHERE snapshot_date >= ?
		  ORDER BY snapshot_date ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CompositeHistoryPoint{}
	for rows.Next() {
		var p CompositeHistoryPoint
		var btc sql.NullFloat64
		if err := rows.Scan(&p.Date, &p.CompositeScore, &btc, &p.ActionBand); err != nil {
			return nil, err
		}
		if btc.Valid {
			v := btc.Float64
			p.BTCPriceUSD = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ETFFlowHistory returns the most recent `days` of BTC spot-ETF aggregate
// net flow from the Playwright-scraped Farside JSON cache. Returned in
// chronological order (oldest first) so the frontend bar chart renders
// left-to-right naturally.
//
// Returns an empty slice (NOT an error) if the cache file is missing —
// caller treats this as "awaiting first fetch" rather than an error
// state. The file is written daily by /home/curran/scripts/farside-fetch.js
// via the /etc/cron.d/ft-farside-fetch cron at 00:25 UTC.
func (s *Service) ETFFlowHistory(ctx context.Context, days int) ([]ETFFlowPoint, error) {
	if days < 1 {
		days = 30
	}
	const cachePath = "/var/lib/ft/data/farside/etf-flow.json"
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []ETFFlowPoint{}, nil
		}
		return nil, err
	}
	var cached struct {
		FetchedAt time.Time `json:"fetchedAt"`
		Rows      []struct {
			Date   string  `json:"date"`
			TotalM float64 `json:"totalM"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		return nil, fmt.Errorf("parse etf-flow.json: %w", err)
	}
	// Source file is sorted newest-first; reverse to chronological for
	// the chart, then take the last `days` (most recent N).
	n := len(cached.Rows)
	if n == 0 {
		return []ETFFlowPoint{}, nil
	}
	take := days
	if take > n {
		take = n
	}
	out := make([]ETFFlowPoint, 0, take)
	// Walk from index take-1 down to 0 (which gives oldest-first of the
	// most recent `take` rows since source is newest-first).
	for i := take - 1; i >= 0; i-- {
		out = append(out, ETFFlowPoint{Date: cached.Rows[i].Date, TotalM: cached.Rows[i].TotalM})
	}
	return out, nil
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

// PriorSnapshotValue returns the indicator's raw_value from the
// snapshot nearest to N days ago. Used by the refresher to compute
// trend_4w for providers that don't supply trend natively (CoinGecko,
// F&G). Returns nil if no snapshot exists within ±2 days of the target.
func (s *Service) PriorSnapshotValue(ctx context.Context, indicatorID string, daysAgo int) (*float64, error) {
	target := time.Now().UTC().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	// Allow ±2 day window so we don't miss when cron is irregular.
	earliest := time.Now().UTC().AddDate(0, 0, -(daysAgo + 2)).Format("2006-01-02")
	latest := time.Now().UTC().AddDate(0, 0, -(daysAgo - 2)).Format("2006-01-02")

	var raw sql.NullFloat64
	err := s.DB.QueryRowContext(ctx, `
		SELECT raw_value FROM crypto_indicator_snapshots
		 WHERE indicator_id = ?
		   AND snapshot_date BETWEEN ? AND ?
		   AND raw_value IS NOT NULL
		 ORDER BY ABS(julianday(snapshot_date) - julianday(?))
		 LIMIT 1`,
		indicatorID, earliest, latest, target).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !raw.Valid {
		return nil, nil
	}
	v := raw.Float64
	return &v, nil
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
