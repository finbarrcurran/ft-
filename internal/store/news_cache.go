package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetNewsCache returns the cached payload + fetched_at for a scope, or
// ErrNotFound if missing.
func (s *Store) GetNewsCache(ctx context.Context, scope string) (payload string, fetchedAt time.Time, err error) {
	var ts int64
	err = s.DB.QueryRowContext(ctx,
		`SELECT payload, fetched_at FROM news_cache WHERE scope = ?`, scope,
	).Scan(&payload, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, ErrNotFound
	}
	if err != nil {
		return "", time.Time{}, err
	}
	return payload, time.Unix(ts, 0), nil
}

// SetNewsCache upserts a payload for a scope.
func (s *Store) SetNewsCache(ctx context.Context, scope, payload string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO news_cache (scope, fetched_at, payload)
		 VALUES (?, strftime('%s','now'), ?)
		 ON CONFLICT(scope) DO UPDATE SET fetched_at = excluded.fetched_at, payload = excluded.payload`,
		scope, payload)
	return err
}
