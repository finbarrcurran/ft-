// Package cryptotheses — thesis read service.
//
// Step 7 implementation: cross-thesis table data + per-thesis detail.
// Read-only API for now; thesis CRUD (D12) lands in Step 6's Scoring
// Engine alongside the modal UI.

package cryptotheses

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ThesisRow is the 16-column shape returned by /api/crypto/theses for
// the cross-thesis table. Mirrors Spec 9l v0.2 §"Item 4" + adapter slug
// + parent thesis lookup.
type ThesisRow struct {
	ID                int64           `json:"id"`
	CoinSymbol        string          `json:"coinSymbol"`
	CoinName          string          `json:"coinName"`
	CoingeckoID       string          `json:"coingeckoID,omitempty"`
	AdapterSlug       string          `json:"adapterSlug"`
	AdapterType       AdapterType     `json:"adapterType"`
	ScorecardType     ScorecardType   `json:"scorecardType"`
	SecondaryTags     []string        `json:"secondaryTags"`
	Score             int             `json:"score"`
	MaxScore          int             `json:"maxScore"`
	Band              Band            `json:"band"`
	RawBand           Band            `json:"rawBand"`
	PillarPassGateFailed bool         `json:"pillarPassGateFailed"`
	Status            Status          `json:"status"`
	HoldingHorizon    HoldingHorizon  `json:"holdingHorizon"`
	BTCBeta           BTCBeta         `json:"btcBeta"`
	ParentSymbol      string          `json:"parentSymbol,omitempty"`
	ParentVersion     string          `json:"parentVersion,omitempty"`
	ActiveVeto        string          `json:"activeVeto,omitempty"`
	ActiveVetoReason  string          `json:"activeVetoReason,omitempty"`
	LockedAt          *int64          `json:"lockedAt,omitempty"`
	LastReviewedAt    *int64          `json:"lastReviewedAt,omitempty"`
	NextReviewAt      *int64          `json:"nextReviewAt,omitempty"`
	NextReviewDate    string          `json:"nextReviewDate,omitempty"` // YYYY-MM-DD
	CatalystDate      string          `json:"catalystDate,omitempty"`
	Version           string          `json:"version"`
}

// PillarScores + Q5 detail + Q9 note for the detail view.
type ThesisDetail struct {
	ThesisRow
	PillarScores     map[string]int  `json:"pillarScores"`
	Q5Mechanism      string          `json:"q5Mechanism,omitempty"`
	Q5AnnualUSD      *float64        `json:"q5AnnualUSD,omitempty"`
	Q5FDVUSD         *float64        `json:"q5FDVUSD,omitempty"`
	Q5AccrualPct     *float64        `json:"q5AccrualPct,omitempty"`
	Q9TeamNote       string          `json:"q9TeamNote,omitempty"`
	CatalystNote     string          `json:"catalystNote,omitempty"`
	LiquidityVenues  []string        `json:"liquidityVenues"`
	MarkdownCurrent  string          `json:"markdownCurrent"`
	RenderedHTML     string          `json:"renderedHTML"`
	Dependencies     []DependencyRow `json:"dependencies"`
	History          []HistoryRow    `json:"history"`
}

// DependencyRow exposed via detail view.
type DependencyRow struct {
	DependencyType  DependencyType  `json:"dependencyType"`
	CascadeStrength CascadeStrength `json:"cascadeStrength"`
	Direction       string          `json:"direction"` // "parent_of" or "child_of"
	OtherSymbol     string          `json:"otherSymbol"`
	OtherVersion    string          `json:"otherVersion"`
	Note            string          `json:"note,omitempty"`
}

// HistoryRow is a slim snapshot from crypto_thesis_history.
type HistoryRow struct {
	ID                int64  `json:"id"`
	EventType         string `json:"eventType"`
	EventReason       string `json:"eventReason,omitempty"`
	Total             int    `json:"total"`
	Band              Band   `json:"band"`
	Delta             int    `json:"delta"`
	RecommendedAction string `json:"recommendedAction,omitempty"`
	ActionTaken       string `json:"actionTaken,omitempty"`
	TriggeredBy       string `json:"triggeredBy"`
	CreatedAt         int64  `json:"createdAt"`
}

// ----- Service ----------------------------------------------------------

// ThesisService wraps DB ops for thesis reads.
type ThesisService struct {
	DB *sql.DB
}

func NewThesisService(db *sql.DB) *ThesisService { return &ThesisService{DB: db} }

// ListAll returns all theses joined with adapter + parent symbol for the
// cross-thesis table. Default ordering: status (locked first), then
// band rank (Strong → Exit), then coin_symbol.
func (s *ThesisService) ListAll(ctx context.Context) ([]ThesisRow, error) {
	q := `
		SELECT t.id, t.coin_symbol, t.coin_name, COALESCE(t.coingecko_id,''),
		       a.slug, a.adapter_type, t.scorecard_type, t.secondary_tags_json,
		       t.total_score, t.max_score, t.band, t.pillar_pass_gate_failed,
		       t.status, t.holding_horizon, t.btc_beta,
		       COALESCE(t.active_veto,''), COALESCE(t.active_veto_reason,''),
		       t.locked_at, t.last_reviewed_at, t.next_review_at,
		       COALESCE(t.catalyst_date,''), t.version
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 ORDER BY
		   CASE t.status
		     WHEN 'locked' THEN 0
		     WHEN 'needs-review' THEN 1
		     WHEN 'watching' THEN 2
		     WHEN 'draft' THEN 3
		     ELSE 4
		   END,
		   CASE t.band
		     WHEN 'strong' THEN 0 WHEN 'accumulate' THEN 1 WHEN 'hold' THEN 2 WHEN 'trim' THEN 3 ELSE 4
		   END,
		   t.coin_symbol`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ThesisRow{}
	for rows.Next() {
		r, err := scanThesisRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Resolve parent symbol via platform_parent / protocol_host cascade for each.
	if err := s.attachParents(ctx, out); err != nil {
		return nil, err
	}
	// Compute raw band from raw score (caller knows pillar_pass_gate_failed).
	for i := range out {
		out[i].RawBand = ComputeBand(out[i].Score, out[i].ScorecardType)
		if t := out[i].NextReviewAt; t != nil {
			out[i].NextReviewDate = time.Unix(*t, 0).UTC().Format("2006-01-02")
		}
	}
	return out, nil
}

// Get returns one thesis by (coin_symbol, version) with full detail.
func (s *ThesisService) Get(ctx context.Context, symbol, version string) (*ThesisDetail, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT t.id, t.coin_symbol, t.coin_name, COALESCE(t.coingecko_id,''),
		       a.slug, a.adapter_type, t.scorecard_type, t.secondary_tags_json,
		       t.total_score, t.max_score, t.band, t.pillar_pass_gate_failed,
		       t.status, t.holding_horizon, t.btc_beta,
		       COALESCE(t.active_veto,''), COALESCE(t.active_veto_reason,''),
		       t.locked_at, t.last_reviewed_at, t.next_review_at,
		       COALESCE(t.catalyst_date,''), t.version,
		       t.pillar_scores_json, COALESCE(t.q5_mechanism,''),
		       t.q5_annual_usd, t.q5_fdv_usd, t.q5_accrual_pct,
		       COALESCE(t.q9_team_note,''), COALESCE(t.catalyst_note,''),
		       t.liquidity_venues_json, t.markdown_current, t.rendered_html
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 WHERE t.coin_symbol = ? AND t.version = ?`, symbol, version)

	var d ThesisDetail
	var pillarJSON, venuesJSON string
	var q5Annual, q5FDV, q5Pct sql.NullFloat64
	r, err := scanDetailRow(row, &d, &pillarJSON, &q5Annual, &q5FDV, &q5Pct, &venuesJSON)
	if err != nil {
		return nil, err
	}
	d.ThesisRow = r
	d.RawBand = ComputeBand(d.Score, d.ScorecardType)
	if d.NextReviewAt != nil {
		d.NextReviewDate = time.Unix(*d.NextReviewAt, 0).UTC().Format("2006-01-02")
	}
	_ = json.Unmarshal([]byte(pillarJSON), &d.PillarScores)
	_ = json.Unmarshal([]byte(venuesJSON), &d.LiquidityVenues)
	if q5Annual.Valid {
		v := q5Annual.Float64
		d.Q5AnnualUSD = &v
	}
	if q5FDV.Valid {
		v := q5FDV.Float64
		d.Q5FDVUSD = &v
	}
	if q5Pct.Valid {
		v := q5Pct.Float64
		d.Q5AccrualPct = &v
	}

	// Attach parent via cascade lookup.
	parents := []ThesisRow{d.ThesisRow}
	if err := s.attachParents(ctx, parents); err == nil {
		d.ThesisRow.ParentSymbol = parents[0].ParentSymbol
		d.ThesisRow.ParentVersion = parents[0].ParentVersion
	}

	// Dependencies (both directions).
	depRows, err := s.DB.QueryContext(ctx, `
		SELECT d.dependency_type, d.cascade_strength,
		       CASE WHEN d.parent_thesis_id = ? THEN 'parent_of' ELSE 'child_of' END AS direction,
		       o.coin_symbol, o.version, COALESCE(d.note,'')
		  FROM crypto_thesis_dependencies d
		  JOIN crypto_theses o ON o.id = CASE WHEN d.parent_thesis_id = ? THEN d.child_thesis_id ELSE d.parent_thesis_id END
		 WHERE d.parent_thesis_id = ? OR d.child_thesis_id = ?
		 ORDER BY d.id`, d.ID, d.ID, d.ID, d.ID)
	if err == nil {
		defer depRows.Close()
		for depRows.Next() {
			var dr DependencyRow
			if err := depRows.Scan(&dr.DependencyType, &dr.CascadeStrength, &dr.Direction,
				&dr.OtherSymbol, &dr.OtherVersion, &dr.Note); err != nil {
				return nil, err
			}
			d.Dependencies = append(d.Dependencies, dr)
		}
	}

	// History rows.
	histRows, err := s.DB.QueryContext(ctx, `
		SELECT id, event_type, COALESCE(event_reason,''),
		       total_score, band, delta,
		       COALESCE(recommended_action,''), COALESCE(action_taken,''),
		       triggered_by, created_at
		  FROM crypto_thesis_history
		 WHERE thesis_id = ?
		 ORDER BY created_at DESC`, d.ID)
	if err == nil {
		defer histRows.Close()
		for histRows.Next() {
			var h HistoryRow
			if err := histRows.Scan(&h.ID, &h.EventType, &h.EventReason,
				&h.Total, &h.Band, &h.Delta, &h.RecommendedAction, &h.ActionTaken,
				&h.TriggeredBy, &h.CreatedAt); err != nil {
				return nil, err
			}
			d.History = append(d.History, h)
		}
	}

	return &d, nil
}

// attachParents resolves the parent symbol+version for each row via the
// platform_parent (preferred) or protocol_host dependency.
func (s *ThesisService) attachParents(ctx context.Context, rows []ThesisRow) error {
	if len(rows) == 0 {
		return nil
	}
	idToIdx := make(map[int64]int, len(rows))
	ids := make([]any, len(rows))
	placeholders := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		idToIdx[r.ID] = i
		placeholders[i] = "?"
	}
	q := fmt.Sprintf(`
		SELECT d.child_thesis_id, p.coin_symbol, p.version, d.dependency_type
		  FROM crypto_thesis_dependencies d
		  JOIN crypto_theses p ON p.id = d.parent_thesis_id
		 WHERE d.dependency_type IN ('platform_parent','protocol_host')
		   AND d.child_thesis_id IN (%s)`, strings.Join(placeholders, ","))
	pRows, err := s.DB.QueryContext(ctx, q, ids...)
	if err != nil {
		return err
	}
	defer pRows.Close()
	// Prefer platform_parent over protocol_host when both exist.
	type pair struct{ symbol, version, dtype string }
	best := map[int64]pair{}
	for pRows.Next() {
		var childID int64
		var sym, ver, dtype string
		if err := pRows.Scan(&childID, &sym, &ver, &dtype); err != nil {
			return err
		}
		current, ok := best[childID]
		if !ok || (current.dtype == "protocol_host" && dtype == "platform_parent") {
			best[childID] = pair{symbol: sym, version: ver, dtype: dtype}
		}
	}
	for id, p := range best {
		if idx, ok := idToIdx[id]; ok {
			rows[idx].ParentSymbol = p.symbol
			rows[idx].ParentVersion = p.version
		}
	}
	return nil
}

// ----- Scan helpers -----------------------------------------------------

func scanThesisRow(sc scanner) (ThesisRow, error) {
	var r ThesisRow
	var lockedAt, lastReviewedAt, nextReviewAt sql.NullInt64
	var tagsJSON string
	var pgFailed int
	if err := sc.Scan(
		&r.ID, &r.CoinSymbol, &r.CoinName, &r.CoingeckoID,
		&r.AdapterSlug, &r.AdapterType, &r.ScorecardType, &tagsJSON,
		&r.Score, &r.MaxScore, &r.Band, &pgFailed,
		&r.Status, &r.HoldingHorizon, &r.BTCBeta,
		&r.ActiveVeto, &r.ActiveVetoReason,
		&lockedAt, &lastReviewedAt, &nextReviewAt,
		&r.CatalystDate, &r.Version); err != nil {
		return r, err
	}
	r.PillarPassGateFailed = pgFailed != 0
	_ = json.Unmarshal([]byte(tagsJSON), &r.SecondaryTags)
	if lockedAt.Valid {
		v := lockedAt.Int64
		r.LockedAt = &v
	}
	if lastReviewedAt.Valid {
		v := lastReviewedAt.Int64
		r.LastReviewedAt = &v
	}
	if nextReviewAt.Valid {
		v := nextReviewAt.Int64
		r.NextReviewAt = &v
	}
	return r, nil
}

func scanDetailRow(sc scanner, d *ThesisDetail,
	pillarJSON *string, q5Annual, q5FDV, q5Pct *sql.NullFloat64, venuesJSON *string,
) (ThesisRow, error) {
	var r ThesisRow
	var lockedAt, lastReviewedAt, nextReviewAt sql.NullInt64
	var tagsJSON string
	var pgFailed int
	if err := sc.Scan(
		&r.ID, &r.CoinSymbol, &r.CoinName, &r.CoingeckoID,
		&r.AdapterSlug, &r.AdapterType, &r.ScorecardType, &tagsJSON,
		&r.Score, &r.MaxScore, &r.Band, &pgFailed,
		&r.Status, &r.HoldingHorizon, &r.BTCBeta,
		&r.ActiveVeto, &r.ActiveVetoReason,
		&lockedAt, &lastReviewedAt, &nextReviewAt,
		&r.CatalystDate, &r.Version,
		pillarJSON, &d.Q5Mechanism,
		q5Annual, q5FDV, q5Pct,
		&d.Q9TeamNote, &d.CatalystNote,
		venuesJSON, &d.MarkdownCurrent, &d.RenderedHTML); err != nil {
		return r, err
	}
	r.PillarPassGateFailed = pgFailed != 0
	_ = json.Unmarshal([]byte(tagsJSON), &r.SecondaryTags)
	if lockedAt.Valid {
		v := lockedAt.Int64
		r.LockedAt = &v
	}
	if lastReviewedAt.Valid {
		v := lastReviewedAt.Int64
		r.LastReviewedAt = &v
	}
	if nextReviewAt.Valid {
		v := nextReviewAt.Int64
		r.NextReviewAt = &v
	}
	return r, nil
}
