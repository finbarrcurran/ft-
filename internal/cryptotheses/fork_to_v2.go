// Package cryptotheses — D27 fork-to-v2 workflow.
//
// When `AcknowledgeCascade` (D26) rejects with `ErrPPGNowFails` because the
// stored pillar scores no longer pass PPG at acknowledgment time, the author
// cannot simply re-lock the thesis — material content changes are needed.
// D27 closes that path by spawning a new draft at version vN+1, inheriting
// everything from vN except status (new = 'draft') and locked_at + history.
//
// Source thesis (vN) transitions to status='forked' (already in CHECK enum
// per Migration 0031). New thesis (vN+1) is a draft the author can edit + lock
// via existing D25 endpoints. Both get `fork_rescore` history rows with
// cross-reference in `event_reason` field per Migration 0031 CHECK enum.
//
// Cascade rows in `crypto_thesis_dependencies` are NOT copied from vN to vN+1
// at fork time — they re-create when vN+1 locks via the existing D25 lock
// auto-cascade path. vN's cascade rows stay pointing at vN (now status=forked).
//
// Unresolved cascade_events on vN are auto-resolved with note "vN forked to
// vN+1" to avoid zombie state.

package cryptotheses

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ----- Errors --------------------------------------------------------------

var (
	ErrCannotForkDraft     = errors.New("cannot fork a draft thesis; only locked or needs-review status is forkable")
	ErrCannotForkForked    = errors.New("cannot fork an already-forked thesis; fork the latest version instead")
	ErrCannotForkInvalid   = errors.New("cannot fork an invalidated thesis")
	ErrVersionUnparseable  = errors.New("cannot parse source thesis version into vN integer form")
)

// ----- Result --------------------------------------------------------------

// ForkResult bundles the outcome of a successful fork-to-v2.
type ForkResult struct {
	SourceThesisID     int64  `json:"sourceThesisId"`
	SourceSymbol       string `json:"sourceSymbol"`
	SourceVersion      string `json:"sourceVersion"`
	SourcePreviousStatus string `json:"sourcePreviousStatus"`
	NewThesisID        int64  `json:"newThesisId"`
	NewSymbol          string `json:"newSymbol"`
	NewVersion         string `json:"newVersion"`
	NewStatus          string `json:"newStatus"`
	CascadeEventsResolvedCount int `json:"cascadeEventsResolvedCount"`
	SourceHistoryRowID int64 `json:"sourceHistoryRowId"`
	NewHistoryRowID    int64 `json:"newHistoryRowId"`
}

// ----- Version arithmetic --------------------------------------------------

var versionRE = regexp.MustCompile(`^v(\d+)$`)

// nextVersion parses "vN" and returns "vN+1". Returns ErrVersionUnparseable
// for any other format.
func nextVersion(v string) (string, error) {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("%w: %q", ErrVersionUnparseable, v)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrVersionUnparseable, v)
	}
	return fmt.Sprintf("v%d", n+1), nil
}

// ----- ForkToV2 ------------------------------------------------------------

// ForkToV2 spawns a new draft thesis at version vN+1, inheriting all editable
// content from the source vN. Source transitions to status='forked'.
//
// Note parameter is appended to the `event_reason` on both history rows; an
// empty note is replaced with a default explanation.
func (s *ThesisWriteService) ForkToV2(ctx context.Context, symbol, version, note string) (*ForkResult, error) {
	symbol = strings.ToUpper(symbol)

	// Pull source thesis full row
	var sourceID int64
	var sourceStatus, pillarJSON, q5Mech, q9Note, catalystDate, catalystNote string
	var tagsJSON, venuesJSON, mdCurrent, btcBeta, horizon, coinName string
	var coingeckoID, auditDate, auditor, custodyTier, custodyCadence, custodyJur sql.NullString
	var primaryAdapterID int64
	var secondaryAdapterID sql.NullInt64
	var scorecard string
	var totalScore, maxScore, ppgFailed, liquidityPassed int
	var band string
	var q5Annual, q5FDV, q5Pct, ryr, paidRev, emissionsUSD, rabr, vaValue, supParUSD sql.NullFloat64
	var networkAgeMonths sql.NullInt64

	err := s.DB.QueryRowContext(ctx, `
		SELECT t.id, t.status, t.coin_name, t.coingecko_id,
		       t.primary_adapter_id, t.secondary_adapter_id, t.scorecard_type,
		       t.pillar_scores_json, t.total_score, t.max_score, t.band, t.pillar_pass_gate_failed,
		       COALESCE(t.q5_mechanism,''), t.q5_annual_usd, t.q5_fdv_usd, t.q5_accrual_pct,
		       COALESCE(t.q9_team_note,''),
		       COALESCE(t.catalyst_date,''), COALESCE(t.catalyst_note,''),
		       t.holding_horizon, t.btc_beta,
		       t.secondary_tags_json, t.liquidity_passed, t.liquidity_venues_json,
		       t.q4_q5_ryr, t.q5_paid_revenue_usd, t.q5_emissions_usd, t.network_age_months,
		       t.q5_rabr, t.q5_verified_asset_value_usd, t.q5_token_supply_at_par_usd,
		       t.q5_audit_date, t.q5_auditor,
		       t.q6_custody_tier, t.q6_custody_cadence, t.q6_custody_jurisdiction,
		       t.markdown_current
		  FROM crypto_theses t
		 WHERE t.coin_symbol = ? AND t.version = ?`,
		symbol, version).Scan(
		&sourceID, &sourceStatus, &coinName, &coingeckoID,
		&primaryAdapterID, &secondaryAdapterID, &scorecard,
		&pillarJSON, &totalScore, &maxScore, &band, &ppgFailed,
		&q5Mech, &q5Annual, &q5FDV, &q5Pct,
		&q9Note,
		&catalystDate, &catalystNote,
		&horizon, &btcBeta,
		&tagsJSON, &liquidityPassed, &venuesJSON,
		&ryr, &paidRev, &emissionsUSD, &networkAgeMonths,
		&rabr, &vaValue, &supParUSD,
		&auditDate, &auditor,
		&custodyTier, &custodyCadence, &custodyJur,
		&mdCurrent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrThesisNotFound
	}
	if err != nil {
		return nil, err
	}

	// Validate source status
	switch sourceStatus {
	case "draft":
		return nil, ErrCannotForkDraft
	case "forked":
		return nil, ErrCannotForkForked
	case "invalidated":
		return nil, ErrCannotForkInvalid
	case "locked", "needs-review":
		// allowed
	default:
		return nil, fmt.Errorf("unexpected source status %q", sourceStatus)
	}

	// Compute new version
	newVersion, err := nextVersion(version)
	if err != nil {
		return nil, err
	}

	// Check new version doesn't already exist
	var exists int
	_ = s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM crypto_theses WHERE coin_symbol=? AND version=?`,
		symbol, newVersion).Scan(&exists)
	if exists > 0 {
		return nil, fmt.Errorf("%w: %s %s already exists", ErrVersionExists, symbol, newVersion)
	}

	// Normalize note
	forkNote := strings.TrimSpace(note)
	if forkNote == "" {
		forkNote = fmt.Sprintf("forked from %s to spawn new draft for material amendments", version)
	}

	// Begin tx
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()

	// 1. Auto-resolve any unresolved cascade_events on source
	resRes, err := tx.ExecContext(ctx, `
		UPDATE cascade_events
		   SET resolved_at = ?, resolution_note = ?
		 WHERE affected_thesis_id = ? AND resolved_at IS NULL`,
		now,
		fmt.Sprintf("source %s %s forked to %s; events auto-resolved", symbol, version, newVersion),
		sourceID)
	if err != nil {
		return nil, fmt.Errorf("auto-resolve source cascade events: %w", err)
	}
	eventsResolved, _ := resRes.RowsAffected()

	// 2. Insert new draft thesis (vN+1) with copied content
	insRes, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_theses (
			coin_symbol, coin_name, coingecko_id,
			primary_adapter_id, secondary_adapter_id, scorecard_type,
			pillar_scores_json, total_score, max_score, band, pillar_pass_gate_failed,
			q5_mechanism, q5_annual_usd, q5_fdv_usd, q5_accrual_pct,
			q9_team_note, catalyst_date, catalyst_note,
			holding_horizon, btc_beta, secondary_tags_json,
			liquidity_passed, liquidity_venues_json,
			q4_q5_ryr, q5_paid_revenue_usd, q5_emissions_usd, network_age_months,
			q5_rabr, q5_verified_asset_value_usd, q5_token_supply_at_par_usd,
			q5_audit_date, q5_auditor,
			q6_custody_tier, q6_custody_cadence, q6_custody_jurisdiction,
			status, version, markdown_current, rendered_html,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?)`,
		symbol, coinName, coingeckoID,
		primaryAdapterID, secondaryAdapterID, scorecard,
		pillarJSON, totalScore, maxScore, band, ppgFailed,
		nullableString(q5Mech), q5Annual, q5FDV, q5Pct,
		nullableString(q9Note), nullableString(catalystDate), nullableString(catalystNote),
		horizon, btcBeta, tagsJSON,
		liquidityPassed, venuesJSON,
		ryr, paidRev, emissionsUSD, networkAgeMonths,
		rabr, vaValue, supParUSD,
		auditDate, auditor,
		custodyTier, custodyCadence, custodyJur,
		newVersion, mdCurrent, Render(mdCurrent),
		now, now)
	if err != nil {
		return nil, fmt.Errorf("insert new draft v%s: %w", newVersion, err)
	}
	newID, _ := insRes.LastInsertId()

	// 3. Write fork_rescore history row on SOURCE (vN)
	srcHistRes, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_thesis_history
		  (thesis_id, event_type, event_reason, pillar_scores_json, total_score, band, delta, recommended_action, triggered_by)
		VALUES (?, 'fork_rescore', ?, ?, ?, ?, 0, 'override', 'user')`,
		sourceID,
		fmt.Sprintf("fork_source: %s spawned %s — %s", version, newVersion, forkNote),
		pillarJSON, totalScore, band)
	if err != nil {
		return nil, fmt.Errorf("source history: %w", err)
	}
	sourceHistID, _ := srcHistRes.LastInsertId()

	// 4. Write fork_rescore history row on TARGET (vN+1)
	tgtHistRes, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_thesis_history
		  (thesis_id, event_type, event_reason, pillar_scores_json, total_score, band, delta, recommended_action, triggered_by)
		VALUES (?, 'fork_rescore', ?, ?, ?, ?, 0, 'none', 'user')`,
		newID,
		fmt.Sprintf("fork_target: %s spawned from %s — %s", newVersion, version, forkNote),
		pillarJSON, totalScore, band)
	if err != nil {
		return nil, fmt.Errorf("target history: %w", err)
	}
	newHistID, _ := tgtHistRes.LastInsertId()

	// 5. Transition source status to 'forked'
	if _, err = tx.ExecContext(ctx, `
		UPDATE crypto_theses
		   SET status = 'forked',
		       updated_at = ?
		 WHERE id = ?`, now, sourceID); err != nil {
		return nil, fmt.Errorf("transition source to forked: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &ForkResult{
		SourceThesisID:             sourceID,
		SourceSymbol:               symbol,
		SourceVersion:              version,
		SourcePreviousStatus:       sourceStatus,
		NewThesisID:                newID,
		NewSymbol:                  symbol,
		NewVersion:                 newVersion,
		NewStatus:                  "draft",
		CascadeEventsResolvedCount: int(eventsResolved),
		SourceHistoryRowID:         sourceHistID,
		NewHistoryRowID:            newHistID,
	}, nil
}
