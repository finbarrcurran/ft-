package theses

import (
	"context"
	"database/sql"
)

// Row is the shape returned for the Theses tab table. Excludes the full
// markdown body / rendered HTML — fetched separately via Get(id).
type Row struct {
	ID               int64   `json:"id"`
	Ticker           string  `json:"ticker"`
	CompanyName      *string `json:"companyName,omitempty"`
	Adapter          string  `json:"adapter"`
	SubType          *string `json:"subType,omitempty"`
	Score            *int    `json:"score,omitempty"`
	MaxScore         int     `json:"maxScore"`
	Version          int     `json:"version"`
	Status           string  `json:"status"`
	LockedDate       *string `json:"lockedDate,omitempty"`
	GitHubURL        string  `json:"githubUrl"`
	NextEarningsDate *string `json:"nextEarningsDate,omitempty"`
	EarningsUrgency  string  `json:"earningsUrgency"` // none|amber|red|revision_needed
	UpdatedAt        int64   `json:"updatedAt"`
}

// Full extends Row with markdown + HTML for the inline viewer.
type Full struct {
	Row
	MarkdownContent string `json:"markdownContent"`
	RenderedHTML    string `json:"renderedHtml"`
}

// GapRow is one ticker the user owns or watches that doesn't have a thesis
// row in the index.
type GapRow struct {
	Ticker   string  `json:"ticker"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"` // 'holding' | 'watchlist'
	Category *string `json:"category,omitempty"`
	Sector   *string `json:"sector,omitempty"`
}

// List returns all rows in the index, ordered by (score desc, ticker asc).
// Optional adapter filter narrows to one folder.
func (e *Engine) List(ctx context.Context, adapter string) ([]Row, error) {
	q := `
		SELECT id, ticker, company_name, adapter, sub_type, score, max_score,
		       version, status, locked_date, github_url,
		       next_earnings_date, earnings_urgency, updated_at
		  FROM theses_index`
	args := []any{}
	if adapter != "" {
		q += ` WHERE adapter = ?`
		args = append(args, adapter)
	}
	q += `
		 ORDER BY CASE WHEN score IS NULL THEN 1 ELSE 0 END, score DESC, ticker ASC`

	rows, err := e.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Row{}
	for rows.Next() {
		var r Row
		var company, sub, locked, earn sql.NullString
		var score sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Ticker, &company, &r.Adapter, &sub,
			&score, &r.MaxScore, &r.Version, &r.Status, &locked, &r.GitHubURL,
			&earn, &r.EarningsUrgency, &r.UpdatedAt); err != nil {
			return nil, err
		}
		if company.Valid {
			r.CompanyName = &company.String
		}
		if sub.Valid {
			r.SubType = &sub.String
		}
		if score.Valid {
			v := int(score.Int64)
			r.Score = &v
		}
		if locked.Valid {
			r.LockedDate = &locked.String
		}
		if earn.Valid {
			r.NextEarningsDate = &earn.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns the full thesis (header + body + rendered HTML) by id.
func (e *Engine) Get(ctx context.Context, id int64) (*Full, error) {
	var f Full
	var company, sub, locked, earn sql.NullString
	var score sql.NullInt64
	err := e.DB.QueryRowContext(ctx, `
		SELECT id, ticker, company_name, adapter, sub_type, score, max_score,
		       version, status, locked_date, github_url,
		       next_earnings_date, earnings_urgency, updated_at,
		       markdown_content, rendered_html
		  FROM theses_index
		 WHERE id = ?`, id).Scan(
		&f.ID, &f.Ticker, &company, &f.Adapter, &sub,
		&score, &f.MaxScore, &f.Version, &f.Status, &locked, &f.GitHubURL,
		&earn, &f.EarningsUrgency, &f.UpdatedAt,
		&f.MarkdownContent, &f.RenderedHTML)
	if err != nil {
		return nil, err
	}
	if company.Valid {
		f.CompanyName = &company.String
	}
	if sub.Valid {
		f.SubType = &sub.String
	}
	if score.Valid {
		v := int(score.Int64)
		f.Score = &v
	}
	if locked.Valid {
		f.LockedDate = &locked.String
	}
	if earn.Valid {
		f.NextEarningsDate = &earn.String
	}
	return &f, nil
}

// Gaps returns the list of stock_holdings + watchlist rows where the ticker
// has no entry in theses_index. Useful for the "stocks owned with no
// thesis" report at the top of the Theses tab.
func (e *Engine) Gaps(ctx context.Context) ([]GapRow, error) {
	rows, err := e.DB.QueryContext(ctx, `
		SELECT s.ticker, s.name, 'holding' AS kind, s.category, s.sector
		  FROM stock_holdings s
		 WHERE s.deleted_at IS NULL
		   AND s.ticker IS NOT NULL AND s.ticker != ''
		   AND NOT EXISTS (
		         SELECT 1 FROM theses_index t WHERE t.ticker = s.ticker
		       )
		UNION ALL
		SELECT w.ticker, COALESCE(w.company_name, w.ticker) AS name,
		       'watchlist' AS kind, NULL AS category, w.sector
		  FROM watchlist w
		 WHERE w.deleted_at IS NULL
		   AND w.kind = 'stock'
		   AND NOT EXISTS (
		         SELECT 1 FROM theses_index t WHERE t.ticker = w.ticker
		       )
		 ORDER BY ticker ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []GapRow{}
	for rows.Next() {
		var g GapRow
		var category, sector sql.NullString
		if err := rows.Scan(&g.Ticker, &g.Name, &g.Kind, &category, &sector); err != nil {
			return nil, err
		}
		if category.Valid {
			g.Category = &category.String
		}
		if sector.Valid {
			g.Sector = &sector.String
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
