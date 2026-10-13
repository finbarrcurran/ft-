package snapshot

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Save writes the payload for its week, replacing that week's row (earlier weeks are kept).
func Save(ctx context.Context, db *sql.DB, p *Payload, generated time.Time) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO ft_snapshots (week_start, generated_at, schema_version, payload_json) VALUES (?,?,?,?)
		ON CONFLICT(week_start) DO UPDATE SET generated_at = excluded.generated_at,
		  schema_version = excluded.schema_version, payload_json = excluded.payload_json`,
		p.WeekStart, generated.Unix(), p.Version, string(b))
	return err
}

// Get returns the stored payload of a week, or nil.
func Get(ctx context.Context, db *sql.DB, weekStart string) (*Payload, error) {
	var raw string
	err := db.QueryRowContext(ctx, `SELECT payload_json FROM ft_snapshots WHERE week_start = ?`, weekStart).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("snapshot %s: %w", weekStart, err)
	}
	return &p, nil
}

// Before returns up to n stored payloads older than weekStart, newest first.
func Before(ctx context.Context, db *sql.DB, weekStart string, n int) ([]*Payload, error) {
	rows, err := db.QueryContext(ctx, `SELECT payload_json FROM ft_snapshots WHERE week_start < ? ORDER BY week_start DESC LIMIT ?`, weekStart, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Payload
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p Payload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// Count is the number of stored weeks.
func Count(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ft_snapshots`).Scan(&n)
	return n, err
}
