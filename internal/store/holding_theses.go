// Spec 14 — Per-holding long-form thesis storage.
//
// One row per (holding_kind, holding_id). Append-only version history.
// Status workflow: draft / locked / needs-review (mirrors Spec 9g).

package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// HoldingThesis mirrors one row of holding_theses.
type HoldingThesis struct {
	ID              int64     `json:"id"`
	HoldingKind     string    `json:"holdingKind"`
	HoldingID       int64     `json:"holdingId"`
	Ticker          string    `json:"ticker"`
	CurrentVersion  string    `json:"currentVersion"`
	Status          string    `json:"status"`
	MarkdownCurrent string    `json:"markdownCurrent,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// HoldingThesisVersion mirrors one row of holding_thesis_versions.
type HoldingThesisVersion struct {
	ID            int64     `json:"id"`
	Version       string    `json:"version"`
	Markdown      string    `json:"markdown,omitempty"`
	ChangelogNote *string   `json:"changelogNote,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// GetHoldingThesis returns the current row for (kind, id) or ErrNotFound.
func (s *Store) GetHoldingThesis(ctx context.Context, kind string, holdingID int64) (*HoldingThesis, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, holding_kind, holding_id, ticker, current_version,
		       status, markdown_current, created_at, updated_at
		  FROM holding_theses
		 WHERE holding_kind = ? AND holding_id = ?`, kind, holdingID)
	var t HoldingThesis
	var createdAt, updatedAt int64
	if err := row.Scan(&t.ID, &t.HoldingKind, &t.HoldingID, &t.Ticker,
		&t.CurrentVersion, &t.Status, &t.MarkdownCurrent, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.CreatedAt = time.Unix(createdAt, 0).UTC()
	t.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &t, nil
}

// UpsertHoldingThesisBody upserts the body (no version bump). Creates
// the row on first call. `ticker` is denormalized for search queries.
func (s *Store) UpsertHoldingThesisBody(ctx context.Context, kind string, holdingID int64, ticker, markdown string) error {
	if strings.TrimSpace(markdown) == "" {
		return errors.New("markdown required")
	}
	// Try update first (cheaper than checking existence).
	res, err := s.DB.ExecContext(ctx, `
		UPDATE holding_theses
		   SET markdown_current = ?, ticker = ?,
		       updated_at = strftime('%s','now')
		 WHERE holding_kind = ? AND holding_id = ?`,
		markdown, ticker, kind, holdingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	// First save → INSERT with version "1".
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO holding_theses
		  (holding_kind, holding_id, ticker, current_version, status, markdown_current)
		VALUES (?, ?, ?, '1', 'draft', ?)`,
		kind, holdingID, ticker, markdown)
	return err
}

// SaveHoldingThesisAsNewVersion archives the prior body under its
// current_version label, then bumps current_version + body.
func (s *Store) SaveHoldingThesisAsNewVersion(ctx context.Context, kind string, holdingID int64, ticker, newVersion, newMarkdown, changelogNote string) error {
	if strings.TrimSpace(newVersion) == "" {
		return errors.New("new version required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Fetch current state (if any).
	var thesisID int64
	var curVersion, curMarkdown string
	row := tx.QueryRowContext(ctx, `
		SELECT id, current_version, markdown_current FROM holding_theses
		 WHERE holding_kind = ? AND holding_id = ?`, kind, holdingID)
	switch err := row.Scan(&thesisID, &curVersion, &curMarkdown); err {
	case nil:
		// Archive the current body under its existing label.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO holding_thesis_versions
			  (thesis_id, version, markdown, changelog_note)
			VALUES (?, ?, ?, NULL)
			ON CONFLICT(thesis_id, version) DO NOTHING`,
			thesisID, curVersion, curMarkdown); err != nil {
			return err
		}
		if newVersion == curVersion {
			return errors.New("new version must differ from current")
		}
		// Update the row.
		if _, err := tx.ExecContext(ctx, `
			UPDATE holding_theses
			   SET markdown_current = ?, current_version = ?, ticker = ?,
			       updated_at = strftime('%s','now')
			 WHERE id = ?`,
			newMarkdown, newVersion, ticker, thesisID); err != nil {
			return err
		}
	case sql.ErrNoRows:
		// First save — INSERT directly with the user-chosen version.
		res, err := tx.ExecContext(ctx, `
			INSERT INTO holding_theses
			  (holding_kind, holding_id, ticker, current_version, status, markdown_current)
			VALUES (?, ?, ?, ?, 'draft', ?)`,
			kind, holdingID, ticker, newVersion, newMarkdown)
		if err != nil {
			return err
		}
		thesisID, _ = res.LastInsertId()
	default:
		return err
	}

	// Archive the new body under the new version label too.
	var note sql.NullString
	if strings.TrimSpace(changelogNote) != "" {
		note = sql.NullString{String: changelogNote, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO holding_thesis_versions
		  (thesis_id, version, markdown, changelog_note)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(thesis_id, version) DO NOTHING`,
		thesisID, newVersion, newMarkdown, note); err != nil {
		return err
	}
	return tx.Commit()
}

// SetHoldingThesisStatus updates the workflow state.
func (s *Store) SetHoldingThesisStatus(ctx context.Context, kind string, holdingID int64, status string) error {
	switch status {
	case "draft", "locked", "needs-review":
	default:
		return errors.New("status must be draft|locked|needs-review")
	}
	res, err := s.DB.ExecContext(ctx, `
		UPDATE holding_theses SET status = ?, updated_at = strftime('%s','now')
		 WHERE holding_kind = ? AND holding_id = ?`, status, kind, holdingID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListHoldingThesisVersions returns the full archive newest first.
func (s *Store) ListHoldingThesisVersions(ctx context.Context, kind string, holdingID int64) ([]HoldingThesisVersion, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT v.id, v.version, v.markdown, v.changelog_note, v.created_at
		  FROM holding_thesis_versions v
		  JOIN holding_theses t ON t.id = v.thesis_id
		 WHERE t.holding_kind = ? AND t.holding_id = ?
		 ORDER BY v.created_at DESC`, kind, holdingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HoldingThesisVersion{}
	for rows.Next() {
		var v HoldingThesisVersion
		var note sql.NullString
		var createdAt int64
		if err := rows.Scan(&v.ID, &v.Version, &v.Markdown, &note, &createdAt); err != nil {
			return nil, err
		}
		if note.Valid {
			v.ChangelogNote = &note.String
		}
		v.CreatedAt = time.Unix(createdAt, 0).UTC()
		out = append(out, v)
	}
	return out, rows.Err()
}

// HoldingThesisSummary is a per-holding "does it have a thesis?" indicator,
// joined into the holdings list response so the UI can show a 📄 chip.
type HoldingThesisSummary struct {
	HoldingKind    string `json:"holdingKind"`
	HoldingID      int64  `json:"holdingId"`
	CurrentVersion string `json:"currentVersion"`
	Status         string `json:"status"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// HoldingThesisSummaries returns thesis metadata for every holding that has one.
// Cheap one-query bulk read used by the Stocks/Crypto table renders.
func (s *Store) HoldingThesisSummaries(ctx context.Context) (map[string]HoldingThesisSummary, error) {
	out := map[string]HoldingThesisSummary{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT holding_kind, holding_id, current_version, status, updated_at
		  FROM holding_theses`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var summ HoldingThesisSummary
		if err := rows.Scan(&summ.HoldingKind, &summ.HoldingID, &summ.CurrentVersion, &summ.Status, &summ.UpdatedAt); err != nil {
			return nil, err
		}
		out[summKey(summ.HoldingKind, summ.HoldingID)] = summ
	}
	return out, rows.Err()
}

func summKey(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}
