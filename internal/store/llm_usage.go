package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Spec 9c.1 — LLM usage log + daily aggregate store helpers.
//
// Every llm.Call() invocation writes one row to llm_usage_log (success,
// budget_blocked, paused, error, or truncated). The daily aggregate
// (llm_usage_daily) is updated transactionally so the budget gate's read
// path is fast (PK lookup, one row).

// LLMUsageRow is the input to InsertLLMUsage.
type LLMUsageRow struct {
	CalledAt         time.Time
	FeatureID        string
	FeatureContext   string
	Provider         string
	Model            string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
	Outcome          string
	ErrorMessage     string
	LatencyMs        int
	RequestSummary   string
}

// LLMUsageOut is the row shape returned by ListLLMUsage.
type LLMUsageOut struct {
	ID               int64      `json:"id"`
	CalledAt         time.Time  `json:"calledAt"`
	FeatureID        string     `json:"featureId"`
	FeatureContext   string     `json:"featureContext,omitempty"`
	Provider         string     `json:"provider"`
	Model            string     `json:"model"`
	InputTokens      int        `json:"inputTokens"`
	OutputTokens     int        `json:"outputTokens"`
	CacheReadTokens  int        `json:"cacheReadTokens"`
	CacheWriteTokens int        `json:"cacheWriteTokens"`
	CostUSD          float64    `json:"costUsd"`
	Outcome          string     `json:"outcome"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	LatencyMs        int        `json:"latencyMs"`
	RequestSummary   string     `json:"requestSummary,omitempty"`
}

// InsertLLMUsage appends one row. Errors are non-fatal at the caller; we
// log a warning rather than failing the call (the API call already
// happened; losing one log row is worse than losing the user's response).
func (s *Store) InsertLLMUsage(ctx context.Context, r LLMUsageRow) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO llm_usage_log (
			called_at, feature_id, feature_context, provider, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
			cost_usd, outcome, error_message, latency_ms, request_summary
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.CalledAt.Unix(), r.FeatureID, nullIfEmpty(r.FeatureContext), r.Provider, r.Model,
		r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens,
		r.CostUSD, r.Outcome, nullIfEmpty(r.ErrorMessage), r.LatencyMs, nullIfEmpty(r.RequestSummary))
	return err
}

// UpdateLLMUsageDaily increments today's row in llm_usage_daily. Wrapped
// in BEGIN IMMEDIATE so two concurrent calls in the same second can't
// race-read-then-overwrite. The per-feature breakdown JSON is merged
// in-process; we accept the read-modify-write cost in exchange for fast
// budget reads.
func (s *Store) UpdateLLMUsageDaily(ctx context.Context, date, featureID string, costUSD float64, outcome string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingCallCount, existingBlocked int
	var existingTotal float64
	var existingByFeatureJSON sql.NullString
	row := tx.QueryRowContext(ctx, `
		SELECT call_count, blocked_count, total_cost_usd, cost_by_feature_json
		  FROM llm_usage_daily WHERE date = ?`, date)
	switch err := row.Scan(&existingCallCount, &existingBlocked, &existingTotal, &existingByFeatureJSON); err {
	case sql.ErrNoRows:
		// First call of the day; existing zeros are fine.
	case nil:
	default:
		return err
	}

	// Merge per-feature breakdown.
	byFeature := map[string]float64{}
	if existingByFeatureJSON.Valid && existingByFeatureJSON.String != "" {
		_ = json.Unmarshal([]byte(existingByFeatureJSON.String), &byFeature)
	}
	byFeature[featureID] += costUSD
	encoded, _ := json.Marshal(byFeature)

	newCallCount := existingCallCount + 1
	newBlocked := existingBlocked
	if outcome == "budget_blocked" || outcome == "paused" {
		newBlocked++
	}
	newTotal := existingTotal + costUSD

	_, err = tx.ExecContext(ctx, `
		INSERT INTO llm_usage_daily (date, call_count, blocked_count, total_cost_usd, cost_by_feature_json, computed_at)
		VALUES (?, ?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(date) DO UPDATE SET
		  call_count = excluded.call_count,
		  blocked_count = excluded.blocked_count,
		  total_cost_usd = excluded.total_cost_usd,
		  cost_by_feature_json = excluded.cost_by_feature_json,
		  computed_at = excluded.computed_at`,
		date, newCallCount, newBlocked, newTotal, string(encoded))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// GetLLMUsageDailyCost returns today's running total (or 0 if no row).
func (s *Store) GetLLMUsageDailyCost(ctx context.Context, date string) (float64, error) {
	var v float64
	err := s.DB.QueryRowContext(ctx,
		`SELECT total_cost_usd FROM llm_usage_daily WHERE date = ?`, date,
	).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return v, err
}

// GetLLMUsageMonthCost sums total_cost_usd for the given YYYY-MM prefix.
func (s *Store) GetLLMUsageMonthCost(ctx context.Context, monthPrefix string) (float64, error) {
	var v sql.NullFloat64
	err := s.DB.QueryRowContext(ctx,
		`SELECT SUM(total_cost_usd) FROM llm_usage_daily WHERE date LIKE ?`,
		monthPrefix+"-%",
	).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Float64, err
}

// LLMUsageDailyRow is the aggregated row used by the Settings dashboard.
type LLMUsageDailyRow struct {
	Date            string             `json:"date"`
	CallCount       int                `json:"callCount"`
	BlockedCount    int                `json:"blockedCount"`
	TotalCostUSD    float64            `json:"totalCostUsd"`
	CostByFeature   map[string]float64 `json:"costByFeature"`
}

// GetLLMUsageDailyRows returns the last `limit` daily aggregate rows.
func (s *Store) GetLLMUsageDailyRows(ctx context.Context, limit int) ([]LLMUsageDailyRow, error) {
	if limit <= 0 {
		limit = 90
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT date, call_count, blocked_count, total_cost_usd, cost_by_feature_json
		  FROM llm_usage_daily ORDER BY date DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMUsageDailyRow
	for rows.Next() {
		var r LLMUsageDailyRow
		var byFeatureJSON sql.NullString
		if err := rows.Scan(&r.Date, &r.CallCount, &r.BlockedCount, &r.TotalCostUSD, &byFeatureJSON); err != nil {
			return nil, err
		}
		r.CostByFeature = map[string]float64{}
		if byFeatureJSON.Valid && byFeatureJSON.String != "" {
			_ = json.Unmarshal([]byte(byFeatureJSON.String), &r.CostByFeature)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LLMUsageFilter is the query shape for the call log table.
type LLMUsageFilter struct {
	FeatureID string // "" = any
	Outcome   string // "" = any
	FromTS    int64  // 0 = any
	ToTS      int64
	Limit     int
}

// ListLLMUsage returns matching rows, newest first.
func (s *Store) ListLLMUsage(ctx context.Context, f LLMUsageFilter) ([]LLMUsageOut, error) {
	q := `SELECT id, called_at, feature_id, COALESCE(feature_context,''), provider, model,
	             input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
	             cost_usd, outcome, COALESCE(error_message,''), latency_ms, COALESCE(request_summary,'')
	        FROM llm_usage_log WHERE 1=1`
	args := []any{}
	if f.FeatureID != "" {
		q += ` AND feature_id = ?`
		args = append(args, f.FeatureID)
	}
	if f.Outcome != "" {
		q += ` AND outcome = ?`
		args = append(args, f.Outcome)
	}
	if f.FromTS > 0 {
		q += ` AND called_at >= ?`
		args = append(args, f.FromTS)
	}
	if f.ToTS > 0 {
		q += ` AND called_at <= ?`
		args = append(args, f.ToTS)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	q += ` ORDER BY called_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMUsageOut
	for rows.Next() {
		var r LLMUsageOut
		var calledAt int64
		if err := rows.Scan(&r.ID, &calledAt, &r.FeatureID, &r.FeatureContext, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens,
			&r.CostUSD, &r.Outcome, &r.ErrorMessage, &r.LatencyMs, &r.RequestSummary); err != nil {
			return nil, err
		}
		r.CalledAt = time.Unix(calledAt, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// nullIfEmpty returns NULL for empty strings (so column defaults / indexes
// behave). Locally-defined so we don't reach into other files' helpers.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
