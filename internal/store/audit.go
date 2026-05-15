package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"ft/internal/domain"
	"time"
)

// AuditAction values per the migration's enum.
const (
	AuditCreate         = "create"
	AuditUpdate         = "update"
	AuditSoftDelete     = "soft_delete"
	AuditRestore        = "restore"
	AuditImportReplace  = "import_replace"
)

// RecordAudit appends one row to holdings_audit. `changes` is marshalled to JSON.
func (s *Store) RecordAudit(
	ctx context.Context,
	userID int64,
	kind string,
	holdingID int64,
	ticker *string,
	symbol *string,
	action string,
	changes any,
	reason *string,
) error {
	body, err := json.Marshal(changes)
	if err != nil {
		body = []byte(`{}`)
	}
	_, err = s.DB.ExecContext(ctx,
		`INSERT INTO holdings_audit
		   (ts, user_id, holding_kind, holding_id, ticker, symbol, action, changes_json, reason, actor)
		 VALUES (strftime('%s','now'), ?, ?, ?, ?, ?, ?, ?, ?, 'fin')`,
		userID, kind, holdingID,
		strPtrToNull(ticker), strPtrToNull(symbol),
		action, string(body), strPtrToNull(reason),
	)
	return err
}

// ListAudit returns the most recent audit rows for a user (paginated).
// limit ≤ 0 falls back to 200. offset >= 0.
func (s *Store) ListAudit(ctx context.Context, userID int64, limit, offset int) ([]*domain.HoldingsAudit, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, ts, user_id, holding_kind, holding_id,
		        ticker, symbol, action, changes_json, reason, actor
		 FROM holdings_audit WHERE user_id = ?
		 ORDER BY ts DESC, id DESC
		 LIMIT ? OFFSET ?`, userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.HoldingsAudit
	for rows.Next() {
		var a domain.HoldingsAudit
		var ts int64
		var ticker, symbol, reason sql.NullString
		if err := rows.Scan(
			&a.ID, &ts, &a.UserID, &a.HoldingKind, &a.HoldingID,
			&ticker, &symbol, &a.Action, &a.Changes, &reason, &a.Actor,
		); err != nil {
			return nil, err
		}
		a.Timestamp = time.Unix(ts, 0).UTC()
		a.Ticker = nsToPtr(ticker)
		a.Symbol = nsToPtr(symbol)
		a.Reason = nsToPtr(reason)
		out = append(out, &a)
	}
	return out, rows.Err()
}
