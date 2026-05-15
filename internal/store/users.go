package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"time"
)

// --- Users -----------------------------------------------------------------

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, displayName string) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, display_name, created_at)
		 VALUES (?, ?, NULLIF(?, ''), strftime('%s','now'))`,
		email, passwordHash, displayName,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FindUserByEmail(ctx context.Context, email string) (*domain.User, string, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, email, COALESCE(display_name,''), password_hash, created_at, last_login_at
		 FROM users WHERE email = ? COLLATE NOCASE`, email,
	)
	var u domain.User
	var hash string
	var createdAt int64
	var lastLogin sql.NullInt64
	if err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &hash, &createdAt, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0)
		u.LastLoginAt = &t
	}
	return &u, hash, nil
}

func (s *Store) FindUserByID(ctx context.Context, id int64) (*domain.User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, email, COALESCE(display_name,''), created_at, last_login_at
		 FROM users WHERE id = ?`, id,
	)
	var u domain.User
	var createdAt int64
	var lastLogin sql.NullInt64
	if err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &createdAt, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(createdAt, 0)
	if lastLogin.Valid {
		t := time.Unix(lastLogin.Int64, 0)
		u.LastLoginAt = &t
	}
	return &u, nil
}

func (s *Store) TouchUserLastLogin(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE users SET last_login_at = strftime('%s','now') WHERE id = ?`, id)
}

// CountUsers helps the server decide whether to render the first-run setup
// page versus the normal login screen.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// --- Sessions --------------------------------------------------------------

func (s *Store) CreateSession(ctx context.Context, token string, userID int64, ttl time.Duration, ua, ip string) error {
	expires := time.Now().Add(ttl).Unix()
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token, user_id, created_at, expires_at, user_agent, ip)
		 VALUES (?, ?, strftime('%s','now'), ?, NULLIF(?, ''), NULLIF(?, ''))`,
		token, userID, expires, ua, ip,
	)
	return err
}

func (s *Store) FindSession(ctx context.Context, token string) (*domain.Session, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT token, user_id, created_at, expires_at FROM sessions
		 WHERE token = ? AND expires_at > strftime('%s','now')`, token,
	)
	var sess domain.Session
	var created, expires int64
	if err := row.Scan(&sess.Token, &sess.UserID, &created, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sess.CreatedAt = time.Unix(created, 0)
	sess.ExpiresAt = time.Unix(expires, 0)
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= strftime('%s','now')`)
	return err
}
