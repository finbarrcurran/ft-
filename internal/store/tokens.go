package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"strings"
	"time"
)

// --- Service tokens --------------------------------------------------------

func (s *Store) CreateServiceToken(ctx context.Context, userID int64, name string, scopes []string, tokenHash string) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO service_tokens (user_id, name, token_hash, scopes, created_at)
		 VALUES (?, ?, ?, ?, strftime('%s','now'))`,
		userID, name, tokenHash, strings.Join(scopes, " "),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindServiceTokenByHash returns the (active, unrevoked) token whose hash matches,
// along with the owning user_id. Used by middleware on every bearer-auth request.
func (s *Store) FindServiceTokenByHash(ctx context.Context, tokenHash string) (*domain.ServiceToken, int64, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, user_id, name, scopes, created_at, last_used_at, revoked_at
		 FROM service_tokens
		 WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash,
	)
	var t domain.ServiceToken
	var userID int64
	var scopes string
	var created int64
	var lastUsed, revoked sql.NullInt64
	if err := row.Scan(&t.ID, &userID, &t.Name, &scopes, &created, &lastUsed, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	t.Scopes = strings.Fields(scopes)
	t.CreatedAt = time.Unix(created, 0)
	if lastUsed.Valid {
		v := time.Unix(lastUsed.Int64, 0)
		t.LastUsedAt = &v
	}
	if revoked.Valid {
		v := time.Unix(revoked.Int64, 0)
		t.RevokedAt = &v
	}
	return &t, userID, nil
}

func (s *Store) TouchServiceTokenLastUsed(ctx context.Context, id int64) {
	_, _ = s.DB.ExecContext(ctx,
		`UPDATE service_tokens SET last_used_at = strftime('%s','now') WHERE id = ?`, id,
	)
}

func (s *Store) ListServiceTokens(ctx context.Context, userID int64) ([]*domain.ServiceToken, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, name, scopes, created_at, last_used_at, revoked_at
		 FROM service_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ServiceToken
	for rows.Next() {
		var t domain.ServiceToken
		var scopes string
		var created int64
		var lastUsed, revoked sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &scopes, &created, &lastUsed, &revoked); err != nil {
			return nil, err
		}
		t.Scopes = strings.Fields(scopes)
		t.CreatedAt = time.Unix(created, 0)
		if lastUsed.Valid {
			v := time.Unix(lastUsed.Int64, 0)
			t.LastUsedAt = &v
		}
		if revoked.Valid {
			v := time.Unix(revoked.Int64, 0)
			t.RevokedAt = &v
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeServiceToken(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE service_tokens SET revoked_at = strftime('%s','now')
		 WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
