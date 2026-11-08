package theses

import (
	"context"
	"database/sql"
	"encoding/json"

	"ft/internal/monitor"
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
	// SC-56 — additive report-date provenance (from ticker_calendar); all empty until the calendar job has run.
	EarningsStatus     string  `json:"earningsStatus,omitempty"`     // DATE_KNOWN|DATE_ESTIMATED|REPORTED|NOT_ANNOUNCED|NOT_APPLICABLE|FETCH_ERROR
	EarningsIsEstimate bool    `json:"earningsIsEstimate,omitempty"` //
	EarningsSource     string  `json:"earningsSource,omitempty"`
	EarningsFetchedAt  *int64  `json:"earningsFetchedAt,omitempty"`
	LastReportedDate   *string `json:"lastReportedDate,omitempty"`
	EarningsNote       string  `json:"earningsNote,omitempty"` // fetch error text or the not-applicable reason
	// Ownership — derived at query time by joining to stock_holdings and
	// watchlist. Drives the Theses tab section grouping (Owned / Watchlist
	// / Other). Priority: owned > watchlist; if a ticker is in both,
	// classify as "owned" because monitoring takes precedence over
	// candidate-tracking.
	Ownership string `json:"ownership"` // owned|watchlist|other
	UpdatedAt int64  `json:"updatedAt"`
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
	// Reason is empty for the original case (no thesis row at all); see GapNoLiveThesis / GapArchived.
	Reason string `json:"reason,omitempty"`
}

// List returns all rows in the index, ordered by (score desc, ticker asc).
// Optional adapter filter narrows to one folder.
//
// Joins to stock_holdings and watchlist (both with deleted_at filters) so
// every row carries an Ownership tag. Watchlist-only rows render in their
// own section on the Theses tab.
func (e *Engine) List(ctx context.Context, adapter string) ([]Row, error) {
	q := `
		SELECT t.id, t.ticker, t.company_name, t.adapter, t.sub_type,
		       t.score, t.max_score, t.version, t.status, t.locked_date,
		       t.github_url, t.next_earnings_date, t.earnings_urgency,
		       t.updated_at,
		       CASE
		         WHEN EXISTS (SELECT 1 FROM stock_holdings s
		                       WHERE s.ticker = t.ticker AND s.deleted_at IS NULL)
		           THEN 'owned'
		         WHEN EXISTS (SELECT 1 FROM watchlist w
		                       WHERE w.ticker = t.ticker AND w.deleted_at IS NULL)
		           THEN 'watchlist'
		         ELSE 'other'
		       END AS ownership,
		       COALESCE(c.earnings_status, ''), COALESCE(c.earnings_is_estimate, 0), COALESCE(c.earnings_source, ''),
		       c.earnings_fetched_at, c.last_reported_date,
		       CASE WHEN c.earnings_status = 'NOT_APPLICABLE' THEN COALESCE(c.not_applicable_reason, '')
		            WHEN c.earnings_status = 'FETCH_ERROR' THEN COALESCE(c.last_error, '') ELSE '' END
		  FROM theses_index t
		  LEFT JOIN ticker_calendar c ON c.ticker = t.ticker
		 WHERE t.status NOT IN ('superseded', 'archived')`
	args := []any{}
	if adapter != "" {
		q += ` AND t.adapter = ?`
		args = append(args, adapter)
	}
	q += `
		 ORDER BY CASE WHEN t.score IS NULL THEN 1 ELSE 0 END, t.score DESC, t.ticker ASC`

	rows, err := e.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Row{}
	for rows.Next() {
		var r Row
		var company, sub, locked, earn, lastRep sql.NullString
		var score, fetched sql.NullInt64
		var est int
		if err := rows.Scan(&r.ID, &r.Ticker, &company, &r.Adapter, &sub,
			&score, &r.MaxScore, &r.Version, &r.Status, &locked, &r.GitHubURL,
			&earn, &r.EarningsUrgency, &r.UpdatedAt, &r.Ownership,
			&r.EarningsStatus, &est, &r.EarningsSource, &fetched, &lastRep, &r.EarningsNote); err != nil {
			return nil, err
		}
		r.EarningsIsEstimate = est != 0
		if fetched.Valid {
			r.EarningsFetchedAt = &fetched.Int64
		}
		if lastRep.Valid {
			r.LastReportedDate = &lastRep.String
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
	var company, sub, locked, earn, lastRep sql.NullString
	var score sql.NullInt64
	err := e.DB.QueryRowContext(ctx, `
		SELECT id, ticker, company_name, adapter, sub_type, score, max_score,
		       version, status, locked_date, github_url,
		       next_earnings_date, earnings_urgency, updated_at,
		       markdown_content, rendered_html,
		       (SELECT c.last_reported_date FROM ticker_calendar c WHERE c.ticker = theses_index.ticker)
		  FROM theses_index
		 WHERE id = ?`, id).Scan(
		&f.ID, &f.Ticker, &company, &f.Adapter, &sub,
		&score, &f.MaxScore, &f.Version, &f.Status, &locked, &f.GitHubURL,
		&earn, &f.EarningsUrgency, &f.UpdatedAt,
		&f.MarkdownContent, &f.RenderedHTML, &lastRep)
	if err != nil {
		return nil, err
	}
	if lastRep.Valid {
		f.LastReportedDate = &lastRep.String
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

// Gap reasons (GapRow.Reason). The empty string is the original case: no row of any status.
const (
	GapNoLiveThesis   = "no_live_thesis"   // rows exist but none is live (all superseded)
	GapArchived       = "archived"         // no live row and at least one archived row
	GapFileNotIndexed = "file_not_indexed" // SC-55 P3: a valid locked file is in the repo but has no row
)

// Gaps returns the stock_holdings + watchlist rows that have no LIVE thesis (SC-55 P1): a ticker is
// covered only when it has at least one row with status 'locked'. Tickers with no row at all keep
// Reason "" (unchanged output for them); tickers whose rows are all superseded or archived carry
// Reason "no_live_thesis" or "archived". Used by the "stocks owned with no thesis" report at the top
// of the Theses tab and by the MCP get_gap_report tool.
func (e *Engine) Gaps(ctx context.Context) ([]GapRow, error) {
	rows, err := e.DB.QueryContext(ctx, `
		SELECT s.ticker, s.name, 'holding' AS kind, s.category, s.sector,
		       CASE WHEN EXISTS (SELECT 1 FROM theses_index t WHERE t.ticker = s.ticker AND t.status = 'archived') THEN 'archived'
		            WHEN EXISTS (SELECT 1 FROM theses_index t WHERE t.ticker = s.ticker) THEN 'no_live_thesis'
		            ELSE '' END AS reason
		  FROM stock_holdings s
		 WHERE s.deleted_at IS NULL
		   AND s.ticker IS NOT NULL AND s.ticker != ''
		   AND NOT EXISTS (
		         SELECT 1 FROM theses_index t WHERE t.ticker = s.ticker AND t.status = 'locked'
		       )
		UNION ALL
		SELECT w.ticker, COALESCE(w.company_name, w.ticker) AS name,
		       'watchlist' AS kind, NULL AS category, w.sector,
		       CASE WHEN EXISTS (SELECT 1 FROM theses_index t WHERE t.ticker = w.ticker AND t.status = 'archived') THEN 'archived'
		            WHEN EXISTS (SELECT 1 FROM theses_index t WHERE t.ticker = w.ticker) THEN 'no_live_thesis'
		            ELSE '' END AS reason
		  FROM watchlist w
		 WHERE w.deleted_at IS NULL
		   AND w.kind = 'stock'
		   AND NOT EXISTS (
		         SELECT 1 FROM theses_index t WHERE t.ticker = w.ticker AND t.status = 'locked'
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
		if err := rows.Scan(&g.Ticker, &g.Name, &g.Kind, &category, &sector, &g.Reason); err != nil {
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// SC-55 P3: locked files that exist in the repo but are not indexed (from the latest reconciliation).
	have := map[string]bool{}
	for _, g := range out {
		have[g.Ticker] = true
	}
	extra, err := e.fileNotIndexedGaps(ctx, have)
	if err != nil {
		return nil, err
	}
	out = append(out, extra...)
	return out, nil
}

// fileNotIndexedGaps turns the latest reconciliation's file_not_indexed issues into gap rows. The kind is
// holding or watchlist when FT holds or watches the ticker, otherwise "other".
func (e *Engine) fileNotIndexedGaps(ctx context.Context, skip map[string]bool) ([]GapRow, error) {
	var detail string
	err := e.DB.QueryRowContext(ctx, `SELECT detail FROM thesis_reconcile_runs ORDER BY id DESC LIMIT 1`).Scan(&detail)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var issues []monitor.ReconcileIssue
	if json.Unmarshal([]byte(detail), &issues) != nil {
		return nil, nil
	}
	var out []GapRow
	seen := map[string]bool{}
	for _, is := range issues {
		if is.Type != IssueFileNotIndexed || is.Ticker == "" || skip[is.Ticker] || seen[is.Ticker] {
			continue
		}
		seen[is.Ticker] = true
		g := GapRow{Ticker: is.Ticker, Name: is.Ticker, Kind: "other", Reason: GapFileNotIndexed}
		var name sql.NullString
		if err := e.DB.QueryRowContext(ctx, `SELECT name FROM stock_holdings WHERE ticker = ? AND deleted_at IS NULL LIMIT 1`, is.Ticker).Scan(&name); err == nil {
			g.Kind = "holding"
		} else if err := e.DB.QueryRowContext(ctx, `SELECT COALESCE(company_name, ticker) FROM watchlist WHERE ticker = ? AND kind = 'stock' AND deleted_at IS NULL LIMIT 1`, is.Ticker).Scan(&name); err == nil {
			g.Kind = "watchlist"
		}
		if name.Valid && name.String != "" {
			g.Name = name.String
		}
		out = append(out, g)
	}
	return out, nil
}
