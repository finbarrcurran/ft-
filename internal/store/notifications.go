package store

import (
	"context"
)

// HasAlertBeenAckedToday returns true if a row exists in notification_log for
// (holdingKind, holdingID, alertKind, today). The DB has a UNIQUE constraint on
// that tuple so this is the dedup primitive the bot relies on.
func (s *Store) HasAlertBeenAckedToday(ctx context.Context, holdingKind string, holdingID int64, alertKind, alertDay string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notification_log
		 WHERE holding_kind = ? AND holding_id = ? AND alert_kind = ? AND alert_day = ?`,
		holdingKind, holdingID, alertKind, alertDay,
	).Scan(&n)
	return n > 0, err
}

// AckAlert records that an alert was sent. Idempotent — if a row already
// exists for that (holding, kind, day), we just update acked_at.
func (s *Store) AckAlert(ctx context.Context, holdingKind string, holdingID int64, alertKind, alertDay string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO notification_log (holding_kind, holding_id, alert_kind, alert_day, acked_at, created_at)
		 VALUES (?, ?, ?, ?, strftime('%s','now'), strftime('%s','now'))
		 ON CONFLICT(holding_kind, holding_id, alert_kind, alert_day)
		 DO UPDATE SET acked_at = strftime('%s','now')`,
		holdingKind, holdingID, alertKind, alertDay,
	)
	return err
}
