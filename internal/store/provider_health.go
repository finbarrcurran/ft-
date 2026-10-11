// Spec 7 D1/D2 — provider_health store.

package store

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
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
	s.recordProviderDaily(ctx, provider, ok, errMsg) // SC-48 per-day counters; never blocks the main write
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

// httpStatusInErr finds "http 429" style statuses the providers embed in their errors.
var httpStatusInErr = regexp.MustCompile(`(?i)\bhttp (\d{3})\b`)

// recordProviderDaily upserts today's counters for the provider (SC-48). Errors are
// swallowed: bookkeeping must never break the calling code path.
func (s *Store) recordProviderDaily(ctx context.Context, provider string, ok bool, errMsg string) {
	day := time.Now().UTC().Format("2006-01-02")
	var okN, failN, f429, f4xx, f5xx, fother int
	if ok {
		okN = 1
	} else {
		failN = 1
		if m := httpStatusInErr.FindStringSubmatch(errMsg); m != nil {
			code, _ := strconv.Atoi(m[1])
			switch {
			case code == 429:
				f429 = 1
			case code >= 400 && code < 500:
				f4xx = 1
			case code >= 500:
				f5xx = 1
			default:
				fother = 1
			}
		} else {
			fother = 1
		}
	}
	_, _ = s.DB.ExecContext(ctx, `
		INSERT INTO provider_daily (provider, day, calls, ok, fail, fail_429, fail_4xx, fail_5xx, fail_other)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, day) DO UPDATE SET
		  calls = calls + 1, ok = ok + excluded.ok, fail = fail + excluded.fail,
		  fail_429 = fail_429 + excluded.fail_429, fail_4xx = fail_4xx + excluded.fail_4xx,
		  fail_5xx = fail_5xx + excluded.fail_5xx, fail_other = fail_other + excluded.fail_other`,
		provider, day, okN, failN, f429, f4xx, f5xx, fother)
}

// ProviderDay is one provider-day of call counters.
type ProviderDay struct {
	Provider  string `json:"provider"`
	Day       string `json:"day"`
	Calls     int    `json:"calls"`
	OK        int    `json:"ok"`
	Fail      int    `json:"fail"`
	Fail429   int    `json:"fail429"`
	Fail4xx   int    `json:"fail4xx"`
	Fail5xx   int    `json:"fail5xx"`
	FailOther int    `json:"failOther"`
}

// ProviderDaily returns the last `days` UTC days of counters (today included), newest first.
func (s *Store) ProviderDaily(ctx context.Context, days int) ([]ProviderDay, error) {
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.DB.QueryContext(ctx, `
		SELECT provider, day, calls, ok, fail, fail_429, fail_4xx, fail_5xx, fail_other
		  FROM provider_daily WHERE day >= ? ORDER BY day DESC, provider`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderDay{}
	for rows.Next() {
		var p ProviderDay
		if err := rows.Scan(&p.Provider, &p.Day, &p.Calls, &p.OK, &p.Fail, &p.Fail429, &p.Fail4xx, &p.Fail5xx, &p.FailOther); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
