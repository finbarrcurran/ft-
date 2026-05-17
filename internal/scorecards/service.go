// Spec 9g — Sector Scorecard Repository.
//
// CRUD + version-history + markdown rendering for adapter scorecards.
// Three docs ship seeded at deploy time (Philosophy v1.1 is the doctrine
// — is_doctrine=1, no UI edit; Energy-Power + Hydrocarbons are adapter
// markdown). Subsequent adapters seed via the admin POST endpoint when
// drafted.
//
// Markdown is rendered server-side via goldmark + bluemonday so the
// frontend stays library-free (matches existing FT convention).

package scorecards

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed seed/*.md
var seedFS embed.FS

// ----- Errors ------------------------------------------------------------

var (
	ErrNotFound       = errors.New("scorecard not found")
	ErrIsDoctrine     = errors.New("scorecard is doctrine; UI edit blocked")
	ErrInvalidStatus  = errors.New("status must be draft|locked|needs-review")
	ErrVersionExists  = errors.New("version already exists in history")
)

// ----- Domain ------------------------------------------------------------

// Scorecard is the row shape returned by list/get.
type Scorecard struct {
	ID                int64     `json:"id"`
	Code              string    `json:"code"`
	DisplayName       string    `json:"displayName"`
	ShortDescription  string    `json:"shortDescription"`
	CurrentVersion    string    `json:"currentVersion"`
	Status            string    `json:"status"`
	MarkdownCurrent   string    `json:"markdownCurrent,omitempty"`
	AppliesToSectors  []string  `json:"appliesToSectors"`
	IsDoctrine        bool      `json:"isDoctrine"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	HoldingsCount     int       `json:"holdingsCount"`
}

// Version is one row of sector_scorecard_versions.
type Version struct {
	ID            int64     `json:"id"`
	Version       string    `json:"version"`
	Markdown      string    `json:"markdown,omitempty"`
	ChangelogNote *string   `json:"changelogNote,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ----- Markdown renderer ------------------------------------------------

var (
	mdOnce     sync.Once
	mdRenderer goldmark.Markdown
	mdPolicy   *bluemonday.Policy
)

func renderer() goldmark.Markdown {
	mdOnce.Do(func() {
		mdRenderer = goldmark.New(
			goldmark.WithExtensions(extension.GFM, extension.Footnote),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
		)
		mdPolicy = bluemonday.UGCPolicy()
		// Permit a few extras for our use:
		//   - class on code/pre/td (so syntax highlighting carries through)
		//   - id on headings (anchor links from autoheading IDs)
		mdPolicy.AllowAttrs("class").OnElements("code", "pre", "td", "th", "table")
		mdPolicy.AllowAttrs("id").OnElements("h1", "h2", "h3", "h4", "h5", "h6")
	})
	return mdRenderer
}

// Render returns sanitized HTML for the given markdown.
func Render(md string) string {
	var buf bytes.Buffer
	if err := renderer().Convert([]byte(md), &buf); err != nil {
		return "<p>render error</p>"
	}
	return mdPolicy.Sanitize(buf.String())
}

// ----- Service ----------------------------------------------------------

// Service wraps a *sql.DB for CRUD ops.
type Service struct {
	DB *sql.DB
}

// New returns a service tied to the given DB handle.
func New(db *sql.DB) *Service { return &Service{DB: db} }

// List returns all scorecards (without full markdown body, for the left
// pane). HoldingsCount joined via stock_holdings.sector_universe_id.
func (s *Service) List(ctx context.Context) ([]Scorecard, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, code, display_name, short_description, current_version,
		       status, applies_to_sectors, is_doctrine,
		       created_at, updated_at
		  FROM sector_scorecards
		 ORDER BY is_doctrine ASC,
		          CASE status WHEN 'locked' THEN 0 WHEN 'needs-review' THEN 1 ELSE 2 END,
		          display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Scorecard{}
	for rows.Next() {
		var sc Scorecard
		var appliesJSON string
		var isDoctrine int
		var createdAt, updatedAt int64
		if err := rows.Scan(&sc.ID, &sc.Code, &sc.DisplayName, &sc.ShortDescription,
			&sc.CurrentVersion, &sc.Status, &appliesJSON, &isDoctrine,
			&createdAt, &updatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(appliesJSON), &sc.AppliesToSectors)
		sc.IsDoctrine = isDoctrine != 0
		sc.CreatedAt = time.Unix(createdAt, 0).UTC()
		sc.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Attach holdings count via one extra query (cheap; ≤34 sectors).
	counts, _ := s.holdingsBySectorCode(ctx)
	for i := range out {
		for _, code := range out[i].AppliesToSectors {
			out[i].HoldingsCount += counts[code]
		}
	}
	return out, nil
}

// Get returns one scorecard by code, including the full markdown body.
func (s *Service) Get(ctx context.Context, code string) (*Scorecard, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, code, display_name, short_description, current_version,
		       status, markdown_current, applies_to_sectors, is_doctrine,
		       created_at, updated_at
		  FROM sector_scorecards WHERE code = ?`, code)
	var sc Scorecard
	var appliesJSON string
	var isDoctrine int
	var createdAt, updatedAt int64
	if err := row.Scan(&sc.ID, &sc.Code, &sc.DisplayName, &sc.ShortDescription,
		&sc.CurrentVersion, &sc.Status, &sc.MarkdownCurrent, &appliesJSON,
		&isDoctrine, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(appliesJSON), &sc.AppliesToSectors)
	sc.IsDoctrine = isDoctrine != 0
	sc.CreatedAt = time.Unix(createdAt, 0).UTC()
	sc.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &sc, nil
}

// UpdateBody updates markdown_current without a version bump. Rejects
// doctrine rows. Caller validated UI auth already.
func (s *Service) UpdateBody(ctx context.Context, code, markdown string) error {
	sc, err := s.Get(ctx, code)
	if err != nil {
		return err
	}
	if sc.IsDoctrine {
		return ErrIsDoctrine
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE sector_scorecards
		   SET markdown_current = ?,
		       updated_at = strftime('%s','now')
		 WHERE code = ?`, markdown, code)
	return err
}

// SaveAsNewVersion archives the current body to history under sc.CurrentVersion,
// then bumps current_version + markdown_current. Rejects doctrine + duplicate
// version strings.
func (s *Service) SaveAsNewVersion(ctx context.Context, code, newVersion, newMarkdown, changelogNote string) error {
	sc, err := s.Get(ctx, code)
	if err != nil {
		return err
	}
	if sc.IsDoctrine {
		return ErrIsDoctrine
	}
	newVersion = strings.TrimSpace(newVersion)
	if newVersion == "" || newVersion == sc.CurrentVersion {
		return errors.New("new version must be non-empty and different from current")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Archive current body under the existing version label.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sector_scorecard_versions
		  (scorecard_id, version, markdown, changelog_note)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scorecard_id, version) DO NOTHING`,
		sc.ID, sc.CurrentVersion, sc.MarkdownCurrent, sql.NullString{}); err != nil {
		return err
	}

	var note sql.NullString
	if strings.TrimSpace(changelogNote) != "" {
		note = sql.NullString{String: changelogNote, Valid: true}
	}
	// Archive the new body under the new version label too — gives a
	// complete history line.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sector_scorecard_versions
		  (scorecard_id, version, markdown, changelog_note)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(scorecard_id, version) DO NOTHING`,
		sc.ID, newVersion, newMarkdown, note); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE sector_scorecards
		   SET markdown_current = ?, current_version = ?,
		       updated_at = strftime('%s','now')
		 WHERE id = ?`, newMarkdown, newVersion, sc.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStatus updates the status workflow. Rejects doctrine + unknown
// status values.
func (s *Service) SetStatus(ctx context.Context, code, status string) error {
	switch status {
	case "draft", "locked", "needs-review":
	default:
		return ErrInvalidStatus
	}
	sc, err := s.Get(ctx, code)
	if err != nil {
		return err
	}
	if sc.IsDoctrine {
		return ErrIsDoctrine
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE sector_scorecards
		   SET status = ?, updated_at = strftime('%s','now')
		 WHERE code = ?`, status, code)
	return err
}

// VersionHistory returns the full archive for a scorecard, newest first.
func (s *Service) VersionHistory(ctx context.Context, code string) ([]Version, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT v.id, v.version, v.markdown, v.changelog_note, v.created_at
		  FROM sector_scorecard_versions v
		  JOIN sector_scorecards sc ON sc.id = v.scorecard_id
		 WHERE sc.code = ?
		 ORDER BY v.created_at DESC`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
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

// holdingsBySectorCode returns sector code → count of active stock_holdings
// rows tagged to that sector.
func (s *Service) holdingsBySectorCode(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT su.code, COUNT(h.id)
		  FROM sector_universe su
		  LEFT JOIN stock_holdings h
		    ON h.sector_universe_id = su.id AND h.deleted_at IS NULL
		 GROUP BY su.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

// ----- Seed --------------------------------------------------------------

// SeedIfEmpty inserts the three doctrine documents (Philosophy v1.1,
// Energy-Power v1, Hydrocarbons v1) when their rows are missing. Idempotent
// — checks per-code before inserting.
//
// Markdown is read from internal/scorecards/seed/*.md via go:embed.
//
// applies_to_sectors values match codes in sector_universe (migration 0019).
func (s *Service) SeedIfEmpty(ctx context.Context) error {
	type seed struct {
		Code             string
		DisplayName      string
		ShortDescription string
		File             string
		Version          string
		Status           string
		IsDoctrine       bool
		AppliesTo        []string
	}
	seeds := []seed{
		{
			Code: "master-spec",
			DisplayName: "Master Spec (living)",
			ShortDescription: "Canonical record of how FT works right now. Updated after every shipped change.",
			File: "seed/master-spec.md",
			Version: "1.0",
			Status: "locked",
			IsDoctrine: false, // editable from UI
			AppliesTo: []string{}, // doesn't pollute sector pills
		},
		{
			Code: "philosophy",
			DisplayName: "Cross-Sector Investment Philosophy",
			ShortDescription: "Doctrine — Universal Law + two-layer model. Read-only.",
			File: "seed/philosophy.md",
			Version: "1.1",
			Status: "locked",
			IsDoctrine: true,
			AppliesTo: []string{}, // doctrine applies to all
		},
		{
			Code: "energy-power",
			DisplayName: "Energy — Power Infrastructure",
			ShortDescription: "Power generation + grid for AI / industrial buildout (Jensen 1000x).",
			File: "seed/energy-power.md",
			Version: "1",
			Status: "locked",
			IsDoctrine: false,
			AppliesTo: []string{
				"power_gas_turbines", "power_nuclear_smr", "power_distributed",
				"power_diversified", "grid_transmission",
			},
		},
		{
			Code: "hydrocarbons",
			DisplayName: "Hydrocarbons",
			ShortDescription: "Oil & gas, LNG, refining, midstream, OFS, tanker shipping.",
			File: "seed/hydrocarbons.md",
			Version: "1",
			Status: "locked",
			IsDoctrine: false,
			AppliesTo: []string{"oil_gas_integrated", "gics_energy"},
		},
		{
			Code: "pharma",
			DisplayName: "Pharma",
			ShortDescription: "Branded pharma — innovators with patent-protected revenue. Sub-types: metabolic-obesity / diversified-immunology / oncology / rare-specialty / mega-diversified.",
			File: "seed/pharma.md",
			Version: "1",
			Status: "needs-review", // v1 draft has 5 open decisions per §7
			IsDoctrine: false,
			AppliesTo: []string{"pharma_metabolic", "pharma_immunology", "gics_healthcare"},
		},
	}

	for _, sd := range seeds {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sector_scorecards WHERE code = ?`, sd.Code).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		raw, err := seedFS.ReadFile(sd.File)
		if err != nil {
			return fmt.Errorf("read seed %s: %w", sd.File, err)
		}
		appliesJSON, _ := json.Marshal(sd.AppliesTo)
		isDoc := 0
		if sd.IsDoctrine {
			isDoc = 1
		}
		_, err = s.DB.ExecContext(ctx, `
			INSERT INTO sector_scorecards
			  (code, display_name, short_description, current_version,
			   status, markdown_current, applies_to_sectors, is_doctrine)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sd.Code, sd.DisplayName, sd.ShortDescription, sd.Version,
			sd.Status, string(raw), string(appliesJSON), isDoc)
		if err != nil {
			return fmt.Errorf("insert seed %s: %w", sd.Code, err)
		}
	}
	return nil
}
