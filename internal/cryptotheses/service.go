// Package cryptotheses — Spec 9l Crypto Thesis Framework.
//
// service.go (Deliverable D11) is adapter CRUD + version history +
// markdown rendering. Parallels internal/scorecards/service.go (Spec 9g)
// — same shape, crypto-specific fields.
//
// Thesis CRUD (D12) lands in a separate file (thesis.go) once at least
// one adapter MD exists (per the handoff sequencing).

package cryptotheses

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

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

type Service struct {
	DB *sql.DB
}

func New(db *sql.DB) *Service { return &Service{DB: db} }

// Version mirrors the 9g version-history shape.
type Version struct {
	ID            int64   `json:"id"`
	Version       string  `json:"version"`
	Markdown      string  `json:"markdown,omitempty"`
	ChangelogNote *string `json:"changelogNote,omitempty"`
	CreatedAt     int64   `json:"createdAt"`
}

// List returns all adapters (without full markdown, for the left pane).
// Ordering: locked first, needs-review next, draft last; alphabetical by
// display_name within each status group.
func (s *Service) List(ctx context.Context) ([]Adapter, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, slug, display_name, short_description, adapter_type,
		       scorecard_type, current_version, status, primary_data_sources,
		       kill_criteria_json, is_doctrine, github_path, github_url,
		       file_sha, created_at, updated_at, locked_at
		  FROM crypto_adapters
		 ORDER BY CASE status
		            WHEN 'locked' THEN 0
		            WHEN 'needs-review' THEN 1
		            ELSE 2
		          END,
		          display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Adapter{}
	for rows.Next() {
		a, err := scanAdapter(rows, false /* withBody */)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get returns one adapter by slug with full markdown body + rendered HTML.
func (s *Service) Get(ctx context.Context, slug string) (*Adapter, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, slug, display_name, short_description, adapter_type,
		       scorecard_type, current_version, status, primary_data_sources,
		       kill_criteria_json, is_doctrine, github_path, github_url,
		       file_sha, created_at, updated_at, locked_at,
		       markdown_current, rendered_html
		  FROM crypto_adapters WHERE slug = ?`, slug)
	a, err := scanAdapter(row, true /* withBody */)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// UpdateBody replaces markdown_current + rendered_html without a version
// bump. Rejects doctrine rows.
func (s *Service) UpdateBody(ctx context.Context, slug, markdown string) error {
	a, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if a.IsDoctrine {
		return ErrIsDoctrine
	}
	html := Render(markdown)
	_, err = s.DB.ExecContext(ctx, `
		UPDATE crypto_adapters
		   SET markdown_current = ?,
		       rendered_html    = ?,
		       updated_at       = strftime('%s','now')
		 WHERE slug = ?`, markdown, html, slug)
	return err
}

// SaveAsNewVersion archives current body under current version label, then
// bumps current_version + markdown_current + rendered_html. Rejects
// doctrine rows and same-version-as-current attempts.
func (s *Service) SaveAsNewVersion(ctx context.Context, slug, newVersion, newMarkdown, changelogNote string) error {
	a, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if a.IsDoctrine {
		return ErrIsDoctrine
	}
	newVersion = strings.TrimSpace(newVersion)
	if newVersion == "" || newVersion == a.CurrentVersion {
		return errors.New("new version must be non-empty and different from current")
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Archive existing body under its existing version label.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(adapter_id, version) DO NOTHING`,
		a.ID, a.CurrentVersion, a.MarkdownCurrent, sql.NullString{}); err != nil {
		return err
	}

	var note sql.NullString
	if strings.TrimSpace(changelogNote) != "" {
		note = sql.NullString{String: changelogNote, Valid: true}
	}
	// Archive new body under new version label too.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_adapter_versions (adapter_id, version, markdown, changelog_note)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(adapter_id, version) DO NOTHING`,
		a.ID, newVersion, newMarkdown, note); err != nil {
		return err
	}

	html := Render(newMarkdown)
	if _, err := tx.ExecContext(ctx, `
		UPDATE crypto_adapters
		   SET markdown_current = ?, rendered_html = ?, current_version = ?,
		       updated_at = strftime('%s','now')
		 WHERE id = ?`, newMarkdown, html, newVersion, a.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStatus updates the status workflow. Rejects doctrine + invalid status.
// Setting status=locked also stamps locked_at.
func (s *Service) SetStatus(ctx context.Context, slug string, status AdapterStatus) error {
	if !status.Valid() {
		return ErrInvalidStatus
	}
	a, err := s.Get(ctx, slug)
	if err != nil {
		return err
	}
	if a.IsDoctrine {
		return ErrIsDoctrine
	}
	if status == AdapterStatusLocked {
		_, err = s.DB.ExecContext(ctx, `
			UPDATE crypto_adapters
			   SET status = ?, locked_at = strftime('%s','now'),
			       updated_at = strftime('%s','now')
			 WHERE slug = ?`, status, slug)
	} else {
		_, err = s.DB.ExecContext(ctx, `
			UPDATE crypto_adapters
			   SET status = ?, updated_at = strftime('%s','now')
			 WHERE slug = ?`, status, slug)
	}
	return err
}

// VersionHistory returns all archived versions for an adapter, newest first.
func (s *Service) VersionHistory(ctx context.Context, slug string) ([]Version, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT v.id, v.version, v.markdown, v.changelog_note, v.created_at
		  FROM crypto_adapter_versions v
		  JOIN crypto_adapters a ON a.id = v.adapter_id
		 WHERE a.slug = ?
		 ORDER BY v.created_at DESC`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var v Version
		var note sql.NullString
		if err := rows.Scan(&v.ID, &v.Version, &v.Markdown, &note, &v.CreatedAt); err != nil {
			return nil, err
		}
		if note.Valid {
			v.ChangelogNote = &note.String
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetVersion returns one specific archived version's markdown.
func (s *Service) GetVersion(ctx context.Context, slug, version string) (*Version, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT v.id, v.version, v.markdown, v.changelog_note, v.created_at
		  FROM crypto_adapter_versions v
		  JOIN crypto_adapters a ON a.id = v.adapter_id
		 WHERE a.slug = ? AND v.version = ?`, slug, version)
	var v Version
	var note sql.NullString
	if err := row.Scan(&v.ID, &v.Version, &v.Markdown, &note, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if note.Valid {
		v.ChangelogNote = &note.String
	}
	return &v, nil
}

// Create inserts a new adapter row. Used by admin POST endpoint.
func (s *Service) Create(ctx context.Context, a Adapter) (int64, error) {
	if err := a.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if strings.TrimSpace(a.CurrentVersion) == "" {
		a.CurrentVersion = "v1"
	}
	if a.Status == "" {
		a.Status = AdapterStatusDraft
	}
	dataSrcJSON, err := a.MarshalDataSources()
	if err != nil {
		return 0, err
	}
	killJSON, err := a.MarshalKillCriteria()
	if err != nil {
		return 0, err
	}
	html := Render(a.MarkdownCurrent)
	isDoc := 0
	if a.IsDoctrine {
		isDoc = 1
	}
	res, err := s.DB.ExecContext(ctx, `
		INSERT INTO crypto_adapters
		  (slug, display_name, short_description, adapter_type, scorecard_type,
		   current_version, status, markdown_current, rendered_html,
		   primary_data_sources, kill_criteria_json, is_doctrine,
		   github_path, github_url, file_sha)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Slug, a.DisplayName, a.ShortDescription, a.AdapterType, a.ScorecardType,
		a.CurrentVersion, a.Status, a.MarkdownCurrent, html,
		dataSrcJSON, killJSON, isDoc,
		nullableString(a.GithubPath), nullableString(a.GithubURL), nullableString(a.FileSHA))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ----- Seed --------------------------------------------------------------

// SeedIfEmpty inserts placeholder draft rows for the 12 adapters defined in
// Spec 9l §2 #5–#12 plus the Expansion v1 cover note (adapters 9–12:
// stablecoin, privacy, cefi-exchange, ai-agent). Each row gets a minimal stub
// markdown body — the real
// adapter MDs arrive from Claude.ai authoring and replace these via
// UpdateBody / SaveAsNewVersion. Idempotent: only inserts missing slugs.
//
// Phase 1 sequencing rationale: shipping the Repository view + tab before
// the adapter MDs exist lets the user inspect the framework structurally
// without us blocking on Claude.ai's authoring queue. As MDs arrive, they
// land via the existing CRUD endpoints.
func (s *Service) SeedIfEmpty(ctx context.Context) error {
	type seed struct {
		Slug             string
		DisplayName      string
		ShortDescription string
		AdapterType      AdapterType
		ScorecardType    ScorecardType
		DataSources      []string
	}
	seeds := []seed{
		{
			Slug:             "btc",
			DisplayName:      "BTC (Monetary Asset)",
			ShortDescription: "6-pillar /12 Monetary-Asset Scorecard. Cycle / Macro / Flows / Network Health / Sentiment / Technical Regime. Distinct from 9e composite.",
			AdapterType:      AdapterBTC,
			ScorecardType:    ScorecardMonetary12,
			DataSources:      []string{"lookintobitcoin", "fred", "farside_etf", "yahoo_btc"},
		},
		{
			Slug:             "l1",
			DisplayName:      "L1 — Smart Contract Platforms",
			ShortDescription: "9-Q /18. Validator economics, dev activity, ecosystem TVL. Kill: mainnet halt >24h in last 60d.",
			AdapterType:      AdapterL1,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama", "etherscan", "solscan"},
		},
		{
			Slug:             "l2",
			DisplayName:      "L2 — Rollups & Sidechains",
			ShortDescription: "9-Q /18. Sequencer revenue, parent-L1 fit, withdrawal trust model. Cascade dep on parent L1 thesis.",
			AdapterType:      AdapterL2,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama", "l2beat"},
		},
		{
			Slug:             "defi",
			DisplayName:      "DeFi Protocols",
			ShortDescription: "9-Q /18. Fee revenue, TVL durability, real-yield. Kill: real yield negative 90d straight.",
			AdapterType:      AdapterDeFi,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama", "tokenterminal"},
		},
		{
			Slug:             "infra",
			DisplayName:      "Infrastructure",
			ShortDescription: "9-Q /18. Integration count, switching costs, dependency depth. Kill: loss of top-3 integration counterparty in last 90d.",
			AdapterType:      AdapterInfra,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama"},
		},
		{
			Slug:             "depin",
			DisplayName:      "DePIN — Decentralised Physical Infra",
			ShortDescription: "9-Q /18. Real-world resource supplied vs paid demand. Kill: paid demand < emissions cost for 90d.",
			AdapterType:      AdapterDePIN,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama"},
		},
		{
			Slug:             "rwa",
			DisplayName:      "Real World Assets",
			ShortDescription: "9-Q /18. Regulated-asset backing, custodian quality, yield source. Kill: underlying asset custodian regulatory action.",
			AdapterType:      AdapterRWA,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama"},
		},
		{
			Slug:             "speculative",
			DisplayName:      "Speculative (Meme / Narrative)",
			ShortDescription: "9-Q /18 with looser fundamentals + stricter Q8 technicals. Weekly re-score override regardless of horizon.",
			AdapterType:      AdapterSpeculative,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko"},
		},
		// Expansion v1 (cover note 2026-06-01): adapters 9–12. Uncalibrated
		// draft templates — no thesis may be locked on these until first-use
		// calibration. Each carries a category-specific mandatory gate.
		{
			Slug:             "stablecoin",
			DisplayName:      "Stablecoin (Peg-Utility)",
			ShortDescription: "/10 safety/utility screen (S1–S5), NOT the 9-Q conviction scorecard. PSRQ gate (peg stability + reserve quality). Screens which stablecoin is safe to park trade capital in.",
			AdapterType:      AdapterStablecoin,
			ScorecardType:    ScorecardSafety10,
			DataSources:      []string{"coingecko", "defillama"},
		},
		{
			Slug:             "privacy",
			DisplayName:      "Privacy (Monetary)",
			ShortDescription: "9-Q /18, monetary-weighted (BTC-scorecard DNA). RDR gate (regulatory/delisting risk) caps liquidity/accessibility. Privacy moat is upside; delisting is the ceiling.",
			AdapterType:      AdapterPrivacy,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko"},
		},
		{
			Slug:             "cefi-exchange",
			DisplayName:      "CeFi / Exchange Token",
			ShortDescription: "9-Q /18, equity-proxy on a centralized exchange business. CCR gate (counterparty/centralization risk) is the structural ceiling (FTT lesson). BNB → CeFi primary + L1 advisory.",
			AdapterType:      AdapterCeFiExchange,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama"},
		},
		{
			Slug:             "ai-agent",
			DisplayName:      "AI Agent / Autonomous Compute",
			ShortDescription: "9-Q /18 (provisional). RAUR gate (real agent utility ratio) — a high bar; most 'AI agent' tokens fail it and stay Speculative ai-narrative. Strongest uncalibrated warning of the four.",
			AdapterType:      AdapterAIAgent,
			ScorecardType:    ScorecardAlt18,
			DataSources:      []string{"coingecko", "defillama"},
		},
	}

	for _, sd := range seeds {
		var count int
		if err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM crypto_adapters WHERE slug = ?`, sd.Slug).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		body := placeholderBody(sd.Slug, sd.DisplayName, sd.ShortDescription, sd.ScorecardType)
		html := Render(body)
		dataSrcJSON, _ := json.Marshal(sd.DataSources)
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO crypto_adapters
			  (slug, display_name, short_description, adapter_type, scorecard_type,
			   current_version, status, markdown_current, rendered_html,
			   primary_data_sources, kill_criteria_json, is_doctrine)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sd.Slug, sd.DisplayName, sd.ShortDescription, sd.AdapterType, sd.ScorecardType,
			"v0-placeholder", AdapterStatusDraft, body, html,
			string(dataSrcJSON), "[]", 0)
		if err != nil {
			return fmt.Errorf("seed crypto adapter %s: %w", sd.Slug, err)
		}
	}
	return nil
}

func placeholderBody(slug, displayName, shortDesc string, sc ScorecardType) string {
	scorecard := "9-Q Crypto Operating Scorecard (/18)"
	switch sc {
	case ScorecardMonetary12:
		scorecard = "6-pillar Monetary-Asset Scorecard (/12)"
	case ScorecardSafety10:
		scorecard = "5-pillar Safety/Utility Screen (/10)"
	}
	return fmt.Sprintf(`# %s — Adapter Placeholder

> **Status:** Placeholder. Full adapter MD pending external authoring.
> **Scorecard:** %s
> **Adapter slug:** %s

## Summary

%s

## Pending

The locked adapter MD will replace this placeholder via the Repository UI
once authored. Until then this row exists so the framework engine has a
valid adapter_id to reference during thesis creation and the Repository
view renders the slot.

*Authored automatically by SeedIfEmpty on first run. Do not edit by hand —
overwrite via the Edit button on the Repository tab when the real adapter
MD is ready.*
`, displayName, scorecard, slug, shortDesc)
}

// ----- Scan helpers -----------------------------------------------------

// scanner is the small interface implemented by both *sql.Row and *sql.Rows
// — lets scanAdapter handle either.
type scanner interface {
	Scan(dest ...any) error
}

func scanAdapter(sc scanner, withBody bool) (Adapter, error) {
	var a Adapter
	var dataSrcJSON, killJSON string
	var isDoctrine int
	var lockedAt sql.NullInt64
	var githubPath, githubURL, fileSHA sql.NullString

	if withBody {
		if err := sc.Scan(
			&a.ID, &a.Slug, &a.DisplayName, &a.ShortDescription, &a.AdapterType,
			&a.ScorecardType, &a.CurrentVersion, &a.Status, &dataSrcJSON,
			&killJSON, &isDoctrine, &githubPath, &githubURL, &fileSHA,
			&a.CreatedAt, &a.UpdatedAt, &lockedAt,
			&a.MarkdownCurrent, &a.RenderedHTML,
		); err != nil {
			return a, err
		}
	} else {
		if err := sc.Scan(
			&a.ID, &a.Slug, &a.DisplayName, &a.ShortDescription, &a.AdapterType,
			&a.ScorecardType, &a.CurrentVersion, &a.Status, &dataSrcJSON,
			&killJSON, &isDoctrine, &githubPath, &githubURL, &fileSHA,
			&a.CreatedAt, &a.UpdatedAt, &lockedAt,
		); err != nil {
			return a, err
		}
	}

	_ = json.Unmarshal([]byte(dataSrcJSON), &a.PrimaryDataSources)
	_ = json.Unmarshal([]byte(killJSON), &a.KillCriteria)
	a.IsDoctrine = isDoctrine != 0
	if lockedAt.Valid {
		v := lockedAt.Int64
		a.LockedAt = &v
	}
	a.GithubPath = githubPath.String
	a.GithubURL = githubURL.String
	a.FileSHA = fileSHA.String
	return a, nil
}

func nullableString(s string) sql.NullString {
	if strings.TrimSpace(s) == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
