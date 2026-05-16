package store

import (
	"context"
	"database/sql"
	"errors"
	"ft/internal/domain"
	"time"
)

// RecordRegimeChange appends one row to regime_history. Returns the row id.
// `inputsJSON` is a raw JSON string; pass "" for none.
func (s *Store) RecordRegimeChange(ctx context.Context, frameworkID, regime, source, inputsJSON, note string) (int64, error) {
	var inputs, noteVal any
	if inputsJSON != "" {
		inputs = inputsJSON
	}
	if note != "" {
		noteVal = note
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO regime_history (ts, framework_id, regime, source, inputs_json, note)
		VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Unix(), frameworkID, regime, source, inputs, noteVal)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListRegimeHistory returns recent rows, newest first. `framework` is
// optional ("" = both). `limit` defaults to 100.
func (s *Store) ListRegimeHistory(ctx context.Context, framework string, limit int) ([]*domain.RegimeRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if framework != "" {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, ts, framework_id, regime, source, inputs_json, note
			  FROM regime_history
			 WHERE framework_id = ?
			 ORDER BY ts DESC, id DESC
			 LIMIT ?`, framework, limit)
	} else {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, ts, framework_id, regime, source, inputs_json, note
			  FROM regime_history
			 ORDER BY ts DESC, id DESC
			 LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.RegimeRecord
	for rows.Next() {
		r := &domain.RegimeRecord{}
		var ts int64
		var inputs, note sql.NullString
		if err := rows.Scan(&r.ID, &ts, &r.FrameworkID, &r.Regime, &r.Source, &inputs, &note); err != nil {
			return nil, err
		}
		r.Timestamp = time.Unix(ts, 0).UTC()
		if inputs.Valid {
			v := inputs.String
			r.InputsJSON = &v
		}
		if note.Valid {
			v := note.String
			r.Note = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MostRecentCyclePhase returns the cycle_phase from the most recent
// auto_cowen_form row, or 0 if none. Used by the classifier's "phase 2 after
// prior phase 3" rule.
func (s *Store) MostRecentCyclePhase(ctx context.Context) (int, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT inputs_json FROM regime_history
		 WHERE framework_id = 'cowen' AND source = 'auto_cowen_form'
		 ORDER BY ts DESC, id DESC
		 LIMIT 1`)
	var raw sql.NullString
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	if !raw.Valid {
		return 0, nil
	}
	// Decode just enough JSON to grab cycle_phase.
	return extractCyclePhase(raw.String), nil
}

// extractCyclePhase is a tiny string-only JSON probe so we don't pull in
// encoding/json just for one int.
func extractCyclePhase(s string) int {
	const key = `"cycle_phase":`
	for i := 0; i < len(s)-len(key); i++ {
		if s[i] != '"' {
			continue
		}
		if i+len(key) <= len(s) && s[i:i+len(key)] == key {
			j := i + len(key)
			// skip whitespace
			for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
				j++
			}
			n := 0
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				n = n*10 + int(s[j]-'0')
				j++
			}
			return n
		}
	}
	return 0
}
