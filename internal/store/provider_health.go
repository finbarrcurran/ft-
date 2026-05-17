// Spec 7 D1/D2 — provider_health store.

package store

import (
	"context"
	"database/sql"
	"time"
)

// ProviderHealth mirrors one row of provider_health.
type ProviderHealth struct {
	Provider             string     `json:"provider"`
	LastSuccessAt        *time.Time `json:"lastSuccessAt,omitempty"`
	LastFailureAt        *time.Time `json:"lastFailureAt,omitempty"`
	LastError            string     `json:"lastError,omitempty"`
	ConsecutiveFailures  int        `json:"consecutiveFailures"`
	SuccessCount         int64      `json:"successCount"`
	FailureCount         int64      `json:"failureCount"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

// RecordProviderHealth UPSERTs one row, updating counts + timestamps.
// `errMsg` is truncated at 200 chars by callers.
func (s *Store) RecordProviderHealth(ctx context.Context, provider string, ok bool, errMsg string) error {
	now := time.Now().UTC().Unix()
	if ok {
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO provider_health
			  (provider, last_success_at, success_count, consecutive_failures, updated_at)
			VALUES (?, ?, 1, 0, ?)
			ON CONFLICT(provider) DO UPDATE SET
			  last_success_at = excluded.last_success_at,
			  success_count   = success_count + 1,
			  consecutive_failures = 0,
			  updated_at      = excluded.updated_at`,
			provider, now, now,
		)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO provider_health
		  (provider, last_failure_at, last_error, failure_count, consecutive_failures, updated_at)
		VALUES (?, ?, ?, 1, 1, ?)
		ON CONFLICT(provider) DO UPDATE SET
		  last_failure_at      = excluded.last_failure_at,
		  last_error           = excluded.last_error,
		  failure_count        = failure_count + 1,
		  consecutive_failures = consecutive_failures + 1,
		  updated_at           = excluded.updated_at`,
		provider, now, errMsg, now,
	)
	return err
}

// ListProviderHealth returns every known provider row, sorted by name.
func (s *Store) ListProviderHealth(ctx context.Context) ([]ProviderHealth, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT provider, last_success_at, last_failure_at, last_error,
		       consecutive_failures, success_count, failure_count, updated_at
		  FROM provider_health
		 ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderHealth{}
	for rows.Next() {
		var p ProviderHealth
		var lsa, lfa sql.NullInt64
		var lerr sql.NullString
		var updated int64
		if err := rows.Scan(&p.Provider, &lsa, &lfa, &lerr,
			&p.ConsecutiveFailures, &p.SuccessCount, &p.FailureCount, &updated); err != nil {
			return nil, err
		}
		if lsa.Valid {
			t := time.Unix(lsa.Int64, 0).UTC()
			p.LastSuccessAt = &t
		}
		if lfa.Valid {
			t := time.Unix(lfa.Int64, 0).UTC()
			p.LastFailureAt = &t
		}
		if lerr.Valid {
			p.LastError = lerr.String
		}
		p.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}
