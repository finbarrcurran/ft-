package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// GetMeta returns the value at key, or "" + ErrNotFound if not present.
func (s *Store) GetMeta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetMeta upserts a string value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// GetMetaFloat is a typed convenience wrapper.
func (s *Store) GetMetaFloat(ctx context.Context, key string, def float64) float64 {
	v, err := s.GetMeta(ctx, key)
	if err != nil {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
