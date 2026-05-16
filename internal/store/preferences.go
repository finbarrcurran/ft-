package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Spec 6 D1 — key/value preference store. Single-user; no user_id.
//
// Keys are short kebab-or-snake-case strings. Values are opaque strings (the
// caller is responsible for serialization — JSON, plain literals, etc).

// GetPreference returns the value for a key, or ("", ErrNotFound) if absent.
func (s *Store) GetPreference(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrNotFound
	}
	var v string
	err := s.DB.QueryRowContext(ctx,
		`SELECT value FROM user_preferences WHERE key = ?`, key,
	).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetPreference upserts the key/value pair.
func (s *Store) SetPreference(ctx context.Context, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty preference key")
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_preferences (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
		  value = excluded.value,
		  updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Unix(),
	)
	return err
}

// ListPreferences returns every (key, value) pair. Used by the future
// Settings → Preferences panel.
func (s *Store) ListPreferences(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM user_preferences`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
