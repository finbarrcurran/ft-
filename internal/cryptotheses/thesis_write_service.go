// Package cryptotheses — D25 Scoring Engine Phase 1 (backend).
//
// thesis_write_service.go owns the thesis create + update-draft + lock
// workflow. Authoring is two-stage: create draft → edit draft → lock.
//
// Locked theses are immutable in D25 scope. PUT on status='locked' returns
// 400. Fork-to-v2 workflow is post-D25 work.
//
// Theses in status='needs-review' are locked-but-pending-cascade-acknowledgment;
// per v0.6.1 §B, D25 carves these out: PUT + /lock return 400 with explicit
// reason. Resolution is the D26 follow-on UI.

package cryptotheses

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ----- Errors --------------------------------------------------------------

var (
	// ErrThesisNotFound declared in cascade.go (reused here).
	ErrAdapterNotFound         = errors.New("adapter not found")
	ErrCannotEditLocked        = errors.New("cannot edit locked thesis; D25 scope is draft-only")
	ErrCannotEditNeedsReview   = errors.New("cannot edit needs-review thesis; resolve cascade first (D26)")
	ErrNotDraft                = errors.New("thesis not in draft status")
	ErrPillarSubCriteriaNeeded = errors.New("pillar sub-criteria required for all 9 alt_18 pillars (q1-q9)")
	ErrBadSubCriterion         = errors.New("sub-criterion must be in {0,1,2}")
	ErrSelfDependency          = errors.New("thesis cannot depend on itself")
	ErrDuplicateDependency     = errors.New("cascade dependency already exists")
	ErrNonInfraOracleParent    = errors.New("oracle_dependency parent must be Infrastructure adapter")
)

// ErrMissingMandatoryField is returned when a per-adapter-type validator
// rejects a lock attempt. Surfaces field + adapter-specific reason.
type ErrMissingMandatoryField struct {
	Field  string
	Reason string
}

func (e ErrMissingMandatoryField) Error() string {
	return fmt.Sprintf("missing mandatory field %q: %s", e.Field, e.Reason)
}

// ----- VETO conditions -----------------------------------------------------

// VetoConditions captures the 6 universal VETO conditions + an open-ended
// adapter-specific map for the per-adapter kill criteria. Per Spec 9l §20:
//   any condition triggered → band forced to Exit; pillar math + PPG cap
//   are overridden.
type VetoConditions struct {
	// Universal 6 (Spec 9l §20)
	UnlockCliffOver20Pct       bool   `json:"unlockCliffOver20Pct"`
	UnlockCliffNote            string `json:"unlockCliffNote,omitempty"`
	ActiveSecEnforcement       bool   `json:"activeSecEnforcement"`
	ActiveSecEnforcementNote   string `json:"activeSecEnforcementNote,omitempty"`
	HolderConcentrationOver50  bool   `json:"holderConcentrationOver50"`
	HolderConcentrationNote    string `json:"holderConcentrationNote,omitempty"`
	ExploitUnresolved60d       bool   `json:"exploitUnresolved60d"`
	ExploitUnresolved60dNote   string `json:"exploitUnresolved60dNote,omitempty"`
	FounderRug                 bool   `json:"founderRug"`
	FounderRugNote             string `json:"founderRugNote,omitempty"`
	LiquidityPrefilterFail     bool   `json:"liquidityPrefilterFail"`
	LiquidityPrefilterFailNote string `json:"liquidityPrefilterFailNote,omitempty"`

	// Adapter-specific (e.g., RWA NAV-drift > 5%, DePIN service-exploit, etc.)
	AdapterSpecificConditions map[string]bool   `json:"adapterSpecificConditions,omitempty"`
	AdapterSpecificNotes      map[string]string `json:"adapterSpecificNotes,omitempty"`
}

// Triggered returns slugs for any VETO conditions set true. Used by the
// scoring path to determine if band must be forced to Exit.
func (vc VetoConditions) Triggered() []string {
	var out []string
	if vc.UnlockCliffOver20Pct {
		out = append(out, string(VetoUnlockCliff20Pct90d))
	}
	if vc.ActiveSecEnforcement {
		out = append(out, string(VetoSECAction))
	}
	if vc.HolderConcentrationOver50 {
		out = append(out, string(VetoHolderConcentration50))
	}
	if vc.ExploitUnresolved60d {
		out = append(out, string(VetoExploitUnresolved60d))
	}
	if vc.FounderRug {
		out = append(out, string(VetoFounderRug))
	}
	if vc.LiquidityPrefilterFail {
		out = append(out, string(VetoLiquidityPrefilter))
	}
	for slug, on := range vc.AdapterSpecificConditions {
		if on {
			out = append(out, "adapter_specific:"+slug)
		}
	}
	return out
}

// PrimaryReason returns the first triggered VETO slug for the active_veto
// DB column, plus a summarized reason text for active_veto_reason.
func (vc VetoConditions) PrimaryReason() (string, string) {
	triggered := vc.Triggered()
	if len(triggered) == 0 {
		return "", ""
	}
	notes := []string{}
	if vc.UnlockCliffOver20Pct && vc.UnlockCliffNote != "" {
		notes = append(notes, "unlock_cliff: "+vc.UnlockCliffNote)
	}
	if vc.ActiveSecEnforcement && vc.ActiveSecEnforcementNote != "" {
		notes = append(notes, "sec: "+vc.ActiveSecEnforcementNote)
	}
	if vc.HolderConcentrationOver50 && vc.HolderConcentrationNote != "" {
		notes = append(notes, "concentration: "+vc.HolderConcentrationNote)
	}
	if vc.ExploitUnresolved60d && vc.ExploitUnresolved60dNote != "" {
		notes = append(notes, "exploit: "+vc.ExploitUnresolved60dNote)
	}
	if vc.FounderRug && vc.FounderRugNote != "" {
		notes = append(notes, "founder_rug: "+vc.FounderRugNote)
	}
	if vc.LiquidityPrefilterFail && vc.LiquidityPrefilterFailNote != "" {
		notes = append(notes, "liquidity: "+vc.LiquidityPrefilterFailNote)
	}
	for slug, note := range vc.AdapterSpecificNotes {
		if vc.AdapterSpecificConditions[slug] && note != "" {
			notes = append(notes, slug+": "+note)
		}
	}
	reason := strings.Join(triggered, ",")
	if len(notes) > 0 {
		reason += " | " + strings.Join(notes, "; ")
	}
	return triggered[0], reason
}

// ----- Draft input shape ---------------------------------------------------

// DraftThesisInput is the request body for POST /api/crypto/theses (create
// draft) and PUT /api/crypto/theses/{symbol}/{version} (update draft).
//
// All fields are persisted; some are mandatory at lock time but optional in
// draft state. Sub-criteria storage shape per v0.8 / D25: stored alongside
// pillar score in pillar_scores_json (legacy seed fixtures lack subs and
// render as {"q1": <int>, ...} for backward compatibility).
type DraftThesisInput struct {
	// Identity
	Symbol             string `json:"symbol"`
	Version            string `json:"version,omitempty"` // defaults to "v1" if blank
	Name               string `json:"name"`
	CoinGeckoID        string `json:"coinGeckoId,omitempty"`
	AdapterSlug        string `json:"adapterSlug"`
	SubType            string `json:"subType,omitempty"`
	PrimaryChainSymbol string `json:"primaryChainSymbol,omitempty"` // for protocol_host upward
	SecondaryAdapterSlug string `json:"secondaryAdapterSlug,omitempty"`
	SecondarySubType     string `json:"secondarySubType,omitempty"`
	Horizon            string `json:"horizon"`
	BTCBeta            string `json:"btcBeta"`

	// Pillar sub-criteria (variable per pillar/sub-type)
	// Keys: "q1".."q9" for alt_18 OR "p1".."p6" for monetary_12
	SubCriteria map[string][]int `json:"subCriteria"`

	// Q5 mechanism + quantification
	Q5Mechanism       string  `json:"q5Mechanism,omitempty"`
	Q5MechanismNote   string  `json:"q5MechanismNote,omitempty"`
	Q5AnnualUSD       float64 `json:"q5AnnualUSD,omitempty"`
	Q5FDVUSD          float64 `json:"q5FDVUSD,omitempty"`

	// DePIN mandatory at lock
	Q4Q5RYR           *float64 `json:"q4q5Ryr,omitempty"`
	Q5PaidRevenueUSD  *float64 `json:"q5PaidRevenueUSD,omitempty"`
	Q5EmissionsUSD    *float64 `json:"q5EmissionsUSD,omitempty"`
	NetworkAgeMonths  *int     `json:"networkAgeMonths,omitempty"`

	// RWA mandatory at lock
	Q5RABR                  *float64 `json:"q5Rabr,omitempty"`
	Q5VerifiedAssetValueUSD *float64 `json:"q5VerifiedAssetValueUSD,omitempty"`
	Q5TokenSupplyAtParUSD   *float64 `json:"q5TokenSupplyAtParUSD,omitempty"`
	Q5AuditDate             string   `json:"q5AuditDate,omitempty"`
	Q5Auditor               string   `json:"q5Auditor,omitempty"`
	Q6CustodyTier           string   `json:"q6CustodyTier,omitempty"`
	Q6CustodyCadence        string   `json:"q6CustodyCadence,omitempty"`
	Q6CustodyJurisdiction   string   `json:"q6CustodyJurisdiction,omitempty"`

	// Forward cascade declaration (D25 Decision 5 — structured)
	OracleDependencyParentSymbol  string `json:"oracleDependencyParentSymbol,omitempty"`
	OracleDependencyParentVersion string `json:"oracleDependencyParentVersion,omitempty"`

	// VETO checklist state
	VetoConditions VetoConditions `json:"vetoConditions,omitempty"`

	// Q9 + catalyst + tags + liquidity
	Q9TeamNote      string   `json:"q9TeamNote,omitempty"`
	CatalystDate    string   `json:"catalystDate,omitempty"`
	CatalystNote    string   `json:"catalystNote,omitempty"`
	SecondaryTags   []string `json:"secondaryTags,omitempty"`
	LiquidityVenues []string `json:"liquidityVenues,omitempty"`
	LiquidityPassed *bool    `json:"liquidityPassed,omitempty"`

	// Body (markdown)
	MarkdownCurrent string `json:"markdownCurrent,omitempty"`

	// Next review (YYYY-MM-DD)
	NextReviewDate string `json:"nextReviewDate,omitempty"`
}

// validateShape ensures basic field presence + sub-criterion ranges.
// Called for both draft and lock paths; deeper adapter-specific validation
// runs at lock time only.
func (in *DraftThesisInput) validateShape(adapter *Adapter) error {
	if strings.TrimSpace(in.Symbol) == "" {
		return errors.New("symbol required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name required")
	}
	if !HoldingHorizon(in.Horizon).Valid() {
		return fmt.Errorf("horizon %q invalid", in.Horizon)
	}
	if !BTCBeta(in.BTCBeta).Valid() {
		return fmt.Errorf("btcBeta %q invalid", in.BTCBeta)
	}
	if in.Q5Mechanism != "" && !Q5Mechanism(in.Q5Mechanism).Valid() {
		return fmt.Errorf("q5Mechanism %q invalid", in.Q5Mechanism)
	}
	for _, subs := range in.SubCriteria {
		for _, s := range subs {
			if s < 0 || s > 2 {
				return ErrBadSubCriterion
			}
		}
	}
	// Speculative adapter horizon trigger (Migration 0032)
	if adapter != nil && adapter.AdapterType == AdapterSpeculative {
		h := HoldingHorizon(in.Horizon)
		if h == HorizonNeverSell || h == HorizonCycle || h == HorizonMultiYear {
			return fmt.Errorf("Speculative adapter forbids horizon %q (use trade/medium/tbd)", in.Horizon)
		}
	}
	return nil
}

// ValidateForLock runs the deeper adapter-specific mandatory-field checks
// per D25 Phase 1 build doctrine (Decision 4 hard enforcement).
//
// DePIN: q4_q5_ryr + q5_paid_revenue_usd + q5_emissions_usd + network_age_months
// RWA:   q5_rabr + q5_verified_asset_value_usd + q5_token_supply_at_par_usd
//        + q5_audit_date + q5_auditor + q6_custody_tier
//
// Pre-revenue DePIN edge case: q5_paid_revenue_usd = 0.0 is allowed (meaningful
// data point); NULL is not (missing data point).
func (in *DraftThesisInput) ValidateForLock(adapter *Adapter) error {
	if err := in.validateShape(adapter); err != nil {
		return err
	}
	// Pillar sub-criteria required for all expected pillars.
	// Keys normalized to uppercase per storage convention ({"Q1","Q2",...}).
	expectedKeys := []string{"Q1", "Q2", "Q3", "Q4", "Q5", "Q6", "Q7", "Q8", "Q9"}
	if adapter.ScorecardType == ScorecardMonetary12 {
		expectedKeys = []string{"P1", "P2", "P3", "P4", "P5", "P6"}
	}
	// Case-insensitive lookup over the input (accept both "q1" and "Q1").
	normSubs := map[string][]int{}
	for k, v := range in.SubCriteria {
		normSubs[strings.ToUpper(k)] = v
	}
	for _, k := range expectedKeys {
		subs, ok := normSubs[k]
		if !ok || len(subs) == 0 {
			return fmt.Errorf("pillar %s sub-criteria required for lock", k)
		}
	}

	// Q5 mechanism required at lock (any non-empty enum value or 'governance_only')
	if in.Q5Mechanism == "" {
		return errors.New("q5Mechanism required for lock")
	}

	switch adapter.AdapterType {
	case AdapterDePIN:
		if in.Q4Q5RYR == nil {
			return ErrMissingMandatoryField{Field: "q4q5Ryr", Reason: "DePIN adapter requires RYR quantification per adapter §3"}
		}
		if in.Q5PaidRevenueUSD == nil {
			return ErrMissingMandatoryField{Field: "q5PaidRevenueUSD", Reason: "DePIN adapter requires paid revenue quantification per adapter §3 (zero is meaningful data; null is not)"}
		}
		if in.Q5EmissionsUSD == nil {
			return ErrMissingMandatoryField{Field: "q5EmissionsUSD", Reason: "DePIN adapter requires token emissions quantification per adapter §3"}
		}
		if in.NetworkAgeMonths == nil {
			return ErrMissingMandatoryField{Field: "networkAgeMonths", Reason: "DePIN adapter requires network age for bootstrap window determination"}
		}
	case AdapterRWA:
		if in.Q5RABR == nil {
			return ErrMissingMandatoryField{Field: "q5Rabr", Reason: "RWA adapter requires RABR (Reserve-Asset Backing Ratio) per adapter §3"}
		}
		if in.Q5VerifiedAssetValueUSD == nil {
			return ErrMissingMandatoryField{Field: "q5VerifiedAssetValueUSD", Reason: "RWA adapter requires verified asset value (per attestation)"}
		}
		if in.Q5TokenSupplyAtParUSD == nil {
			return ErrMissingMandatoryField{Field: "q5TokenSupplyAtParUSD", Reason: "RWA adapter requires token supply at par value"}
		}
		if strings.TrimSpace(in.Q5AuditDate) == "" {
			return ErrMissingMandatoryField{Field: "q5AuditDate", Reason: "RWA adapter requires attestation date"}
		}
		if strings.TrimSpace(in.Q5Auditor) == "" {
			return ErrMissingMandatoryField{Field: "q5Auditor", Reason: "RWA adapter requires attestation provider"}
		}
		if in.Q6CustodyTier == "" {
			return ErrMissingMandatoryField{Field: "q6CustodyTier", Reason: "RWA adapter requires Custody Verification Tier per adapter §3"}
		}
		if !CustodyTier(in.Q6CustodyTier).Valid() {
			return fmt.Errorf("q6CustodyTier %q invalid", in.Q6CustodyTier)
		}
	}
	return nil
}

// ----- ThesisWriteService --------------------------------------------------

// ThesisWriteService owns the create + edit + lock workflow.
type ThesisWriteService struct {
	DB       *sql.DB
	Adapters *Service
	Cascade  *CascadeService
}

func NewThesisWriteService(db *sql.DB, adapters *Service, cascade *CascadeService) *ThesisWriteService {
	return &ThesisWriteService{DB: db, Adapters: adapters, Cascade: cascade}
}

// LockResult bundles the computed scoring outputs returned after a draft
// is locked successfully.
type LockResult struct {
	ThesisID         int64       `json:"thesisId"`
	Symbol           string      `json:"symbol"`
	Version          string      `json:"version"`
	PillarScores     map[string]int `json:"pillarScores"`
	Total            int         `json:"total"`
	MaxScore         int         `json:"maxScore"`
	RawBand          Band        `json:"rawBand"`
	FinalBand        Band        `json:"finalBand"`
	PPGCapApplied    bool        `json:"ppgCapApplied"`
	PPGFailedGates   []string    `json:"ppgFailedGates,omitempty"`
	VetoTriggered    bool        `json:"vetoTriggered"`
	VetoReasons      []string    `json:"vetoReasons,omitempty"`
	CascadeRowsCreated []string  `json:"cascadeRowsCreated,omitempty"` // human-readable summaries
}

// CreateDraft inserts a new draft thesis row. Computes preview pillar scores
// from any sub-criteria provided (for live UI display), but doesn't run
// adapter-specific mandatory-field validation (that's lock-time only).
func (s *ThesisWriteService) CreateDraft(ctx context.Context, in *DraftThesisInput) (int64, error) {
	if in.Version == "" {
		in.Version = "v1"
	}
	adapter, err := s.Adapters.Get(ctx, in.AdapterSlug)
	if err != nil {
		return 0, fmt.Errorf("%w: adapterSlug=%s", ErrAdapterNotFound, in.AdapterSlug)
	}
	if err := in.validateShape(adapter); err != nil {
		return 0, err
	}
	var secondaryAdapterID sql.NullInt64
	if in.SecondaryAdapterSlug != "" {
		sa, err := s.Adapters.Get(ctx, in.SecondaryAdapterSlug)
		if err == nil {
			secondaryAdapterID = sql.NullInt64{Int64: sa.ID, Valid: true}
		}
	}

	// Compute pillar scores from sub-criteria for storage (write-time per Decision 1)
	subsTyped := map[string]SubCriteria{}
	for k, v := range in.SubCriteria {
		subsTyped[strings.ToUpper(k)] = SubCriteria(v) // normalize to Q1.. for storage
	}
	scores := ComputeAllPillars(subsTyped)
	bandRes := ComputeRawAndFinalBand(scores, adapter.ScorecardType)
	pillarJSON := buildPillarScoresJSON(subsTyped, scores)

	ppgFailedInt := 0
	if bandRes.PPGCapApplied {
		ppgFailedInt = 1
	}
	// D25 Phase 1: persist transient lock-time fields (primary_chain_symbol,
	// oracle_dependency_parent_*, veto_conditions) via __meta__: prefix tags
	// in secondary_tags_json. Avoids Migration 0034 (schema change). Read path
	// filters __meta__: tags from the public ThesisRow.SecondaryTags slice
	// (handled in scanThesisRow via filterMetaTags helper in scoring.go).
	tagsWithMeta := append([]string{}, in.SecondaryTags...)
	tagsWithMeta = append(tagsWithMeta, encodeMetaTags(in)...)
	tagsJSON, _ := json.Marshal(tagsWithMeta)
	if len(tagsWithMeta) == 0 {
		tagsJSON = []byte("[]")
	}
	venuesJSON, _ := json.Marshal(in.LiquidityVenues)
	if len(in.LiquidityVenues) == 0 {
		venuesJSON = []byte("[]")
	}

	var q5Pct float64
	if in.Q5FDVUSD > 0 {
		q5Pct = (in.Q5AnnualUSD / in.Q5FDVUSD) * 100
	}

	now := time.Now().Unix()
	var nextReview sql.NullInt64
	if in.NextReviewDate != "" {
		if t, err := time.Parse("2006-01-02", in.NextReviewDate); err == nil {
			nextReview = sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
		}
	}

	res, err := s.DB.ExecContext(ctx, `
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
			next_review_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?, ?, ?, ?)`,
		strings.ToUpper(in.Symbol), in.Name, nullableString(in.CoinGeckoID),
		adapter.ID, secondaryAdapterID, adapter.ScorecardType,
		pillarJSON, bandRes.Total, adapter.ScorecardType.MaxScore(), string(bandRes.FinalBand), ppgFailedInt,
		nullableString(in.Q5Mechanism), nullFloat(in.Q5AnnualUSD), nullFloat(in.Q5FDVUSD), nullFloat(q5Pct),
		nullableString(in.Q9TeamNote), nullableString(in.CatalystDate), nullableString(in.CatalystNote),
		in.Horizon, in.BTCBeta, string(tagsJSON),
		boolToInt(in.LiquidityPassed), string(venuesJSON),
		nullFloatPtr(in.Q4Q5RYR), nullFloatPtr(in.Q5PaidRevenueUSD), nullFloatPtr(in.Q5EmissionsUSD), nullIntPtr(in.NetworkAgeMonths),
		nullFloatPtr(in.Q5RABR), nullFloatPtr(in.Q5VerifiedAssetValueUSD), nullFloatPtr(in.Q5TokenSupplyAtParUSD),
		nullableString(in.Q5AuditDate), nullableString(in.Q5Auditor),
		nullableString(in.Q6CustodyTier), nullableString(in.Q6CustodyCadence), nullableString(in.Q6CustodyJurisdiction),
		in.Version, in.MarkdownCurrent, Render(in.MarkdownCurrent),
		nextReview, now, now)
	if err != nil {
		return 0, fmt.Errorf("insert draft thesis: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// UpdateDraft overwrites a draft thesis's fields. Returns ErrCannotEditLocked
// if status is 'locked' and ErrCannotEditNeedsReview if 'needs-review'.
func (s *ThesisWriteService) UpdateDraft(ctx context.Context, symbol, version string, in *DraftThesisInput) error {
	var id int64
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, status FROM crypto_theses WHERE coin_symbol=? AND version=?`,
		strings.ToUpper(symbol), version).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrThesisNotFound
	}
	if err != nil {
		return err
	}
	if status == "locked" {
		return ErrCannotEditLocked
	}
	if status == "needs-review" {
		return ErrCannotEditNeedsReview
	}
	if status != "draft" {
		return ErrNotDraft
	}
	adapter, err := s.Adapters.Get(ctx, in.AdapterSlug)
	if err != nil {
		return fmt.Errorf("%w: adapterSlug=%s", ErrAdapterNotFound, in.AdapterSlug)
	}
	if err := in.validateShape(adapter); err != nil {
		return err
	}
	subsTyped := map[string]SubCriteria{}
	for k, v := range in.SubCriteria {
		subsTyped[strings.ToUpper(k)] = SubCriteria(v)
	}
	scores := ComputeAllPillars(subsTyped)
	bandRes := ComputeRawAndFinalBand(scores, adapter.ScorecardType)
	pillarJSON := buildPillarScoresJSON(subsTyped, scores)
	ppgFailedInt := 0
	if bandRes.PPGCapApplied {
		ppgFailedInt = 1
	}
	tagsWithMeta := append([]string{}, in.SecondaryTags...)
	tagsWithMeta = append(tagsWithMeta, encodeMetaTags(in)...)
	tagsJSON, _ := json.Marshal(tagsWithMeta)
	if len(tagsWithMeta) == 0 {
		tagsJSON = []byte("[]")
	}
	venuesJSON, _ := json.Marshal(in.LiquidityVenues)
	if len(in.LiquidityVenues) == 0 {
		venuesJSON = []byte("[]")
	}
	var q5Pct float64
	if in.Q5FDVUSD > 0 {
		q5Pct = (in.Q5AnnualUSD / in.Q5FDVUSD) * 100
	}
	var nextReview sql.NullInt64
	if in.NextReviewDate != "" {
		if t, err := time.Parse("2006-01-02", in.NextReviewDate); err == nil {
			nextReview = sql.NullInt64{Int64: t.UTC().Unix(), Valid: true}
		}
	}
	now := time.Now().Unix()
	_, err = s.DB.ExecContext(ctx, `
		UPDATE crypto_theses SET
			coin_name = ?, coingecko_id = ?,
			pillar_scores_json = ?, total_score = ?, band = ?, pillar_pass_gate_failed = ?,
			q5_mechanism = ?, q5_annual_usd = ?, q5_fdv_usd = ?, q5_accrual_pct = ?,
			q9_team_note = ?, catalyst_date = ?, catalyst_note = ?,
			holding_horizon = ?, btc_beta = ?, secondary_tags_json = ?,
			liquidity_passed = ?, liquidity_venues_json = ?,
			q4_q5_ryr = ?, q5_paid_revenue_usd = ?, q5_emissions_usd = ?, network_age_months = ?,
			q5_rabr = ?, q5_verified_asset_value_usd = ?, q5_token_supply_at_par_usd = ?,
			q5_audit_date = ?, q5_auditor = ?,
			q6_custody_tier = ?, q6_custody_cadence = ?, q6_custody_jurisdiction = ?,
			markdown_current = ?, rendered_html = ?,
			next_review_at = ?, updated_at = ?
		WHERE id = ?`,
		in.Name, nullableString(in.CoinGeckoID),
		pillarJSON, bandRes.Total, string(bandRes.FinalBand), ppgFailedInt,
		nullableString(in.Q5Mechanism), nullFloat(in.Q5AnnualUSD), nullFloat(in.Q5FDVUSD), nullFloat(q5Pct),
		nullableString(in.Q9TeamNote), nullableString(in.CatalystDate), nullableString(in.CatalystNote),
		in.Horizon, in.BTCBeta, string(tagsJSON),
		boolToInt(in.LiquidityPassed), string(venuesJSON),
		nullFloatPtr(in.Q4Q5RYR), nullFloatPtr(in.Q5PaidRevenueUSD), nullFloatPtr(in.Q5EmissionsUSD), nullIntPtr(in.NetworkAgeMonths),
		nullFloatPtr(in.Q5RABR), nullFloatPtr(in.Q5VerifiedAssetValueUSD), nullFloatPtr(in.Q5TokenSupplyAtParUSD),
		nullableString(in.Q5AuditDate), nullableString(in.Q5Auditor),
		nullableString(in.Q6CustodyTier), nullableString(in.Q6CustodyCadence), nullableString(in.Q6CustodyJurisdiction),
		in.MarkdownCurrent, Render(in.MarkdownCurrent),
		nextReview, now, id)
	return err
}

// DeleteDraft removes a draft. Returns ErrCannotEditLocked / ErrCannotEditNeedsReview
// for non-draft states.
func (s *ThesisWriteService) DeleteDraft(ctx context.Context, symbol, version string) error {
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status FROM crypto_theses WHERE coin_symbol=? AND version=?`,
		strings.ToUpper(symbol), version).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrThesisNotFound
	}
	if err != nil {
		return err
	}
	if status == "locked" {
		return ErrCannotEditLocked
	}
	if status == "needs-review" {
		return ErrCannotEditNeedsReview
	}
	if status != "draft" {
		return ErrNotDraft
	}
	_, err = s.DB.ExecContext(ctx,
		`DELETE FROM crypto_theses WHERE coin_symbol=? AND version=? AND status='draft'`,
		strings.ToUpper(symbol), version)
	return err
}

// ListDrafts returns all theses in status='draft'.
func (s *ThesisWriteService) ListDrafts(ctx context.Context) ([]ThesisRow, error) {
	q := `
		SELECT t.id, t.coin_symbol, t.coin_name, COALESCE(t.coingecko_id,''),
		       a.slug, a.adapter_type, t.scorecard_type, t.secondary_tags_json,
		       COALESCE(sa.slug,''), COALESCE(sa.adapter_type,''),
		       t.total_score, t.max_score, t.band, t.pillar_pass_gate_failed,
		       t.status, t.holding_horizon, t.btc_beta,
		       COALESCE(t.active_veto,''), COALESCE(t.active_veto_reason,''),
		       t.locked_at, t.last_reviewed_at, t.next_review_at,
		       COALESCE(t.catalyst_date,''), t.version
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		  LEFT JOIN crypto_adapters sa ON sa.id = t.secondary_adapter_id
		 WHERE t.status = 'draft'
		 ORDER BY t.updated_at DESC`
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
	return out, rows.Err()
}

// Lock transitions a draft → locked. Runs validators, recomputes scoring,
// applies PPG cap, applies VETO override, creates upward + forward cascade
// rows. Returns LockResult or an error.
func (s *ThesisWriteService) Lock(ctx context.Context, symbol, version string) (*LockResult, error) {
	symbol = strings.ToUpper(symbol)

	// Pull draft row + adapter
	var id, adapterID int64
	var status, adapterSlug, btcBeta, primaryChainHint string
	err := s.DB.QueryRowContext(ctx, `
		SELECT t.id, t.status, t.primary_adapter_id, a.slug, t.btc_beta,
		       COALESCE(t.catalyst_date, '')
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 WHERE t.coin_symbol = ? AND t.version = ?`, symbol, version).Scan(&id, &status, &adapterID, &adapterSlug, &btcBeta, &primaryChainHint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrThesisNotFound
	}
	if err != nil {
		return nil, err
	}
	if status == "locked" {
		return nil, ErrCannotEditLocked
	}
	if status == "needs-review" {
		return nil, ErrCannotEditNeedsReview
	}
	if status != "draft" {
		return nil, ErrNotDraft
	}

	// Reconstruct the input from the row (we'll re-validate against current shape)
	in, err := s.reloadAsInput(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("reload: %w", err)
	}
	adapter, err := s.Adapters.Get(ctx, adapterSlug)
	if err != nil {
		return nil, fmt.Errorf("adapter %s: %w", adapterSlug, err)
	}
	if err := in.ValidateForLock(adapter); err != nil {
		return nil, err
	}

	// Recompute scoring authoritatively
	subsTyped := map[string]SubCriteria{}
	for k, v := range in.SubCriteria {
		subsTyped[strings.ToUpper(k)] = SubCriteria(v)
	}
	scores := ComputeAllPillars(subsTyped)
	bandRes := ComputeRawAndFinalBand(scores, adapter.ScorecardType)
	pillarJSON := buildPillarScoresJSON(subsTyped, scores)

	// VETO override
	finalBand, vetoTriggered, vetoReasons := ApplyVeto(bandRes.FinalBand, in.VetoConditions)
	vetoPrimary, vetoReasonText := in.VetoConditions.PrimaryReason()

	ppgFailedInt := 0
	if bandRes.PPGCapApplied {
		ppgFailedInt = 1
	}

	// Begin tx
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	q5Pct := 0.0
	if in.Q5FDVUSD > 0 {
		q5Pct = (in.Q5AnnualUSD / in.Q5FDVUSD) * 100
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE crypto_theses SET
			pillar_scores_json = ?,
			total_score = ?,
			band = ?,
			pillar_pass_gate_failed = ?,
			q5_accrual_pct = ?,
			active_veto = ?,
			active_veto_reason = ?,
			veto_tripped_at = ?,
			status = 'locked',
			locked_at = ?,
			last_reviewed_at = ?,
			updated_at = ?
		 WHERE id = ?`,
		pillarJSON, bandRes.Total, string(finalBand), ppgFailedInt,
		nullFloat(q5Pct),
		nullableString(vetoPrimary), nullableString(vetoReasonText),
		conditionalUnix(vetoTriggered, now),
		now, now, now, id); err != nil {
		return nil, fmt.Errorf("lock update: %w", err)
	}

	// Upward cascade auto-create
	rowsCreated := []string{}

	// protocol_host upward — based on primaryChainSymbol field
	if in.PrimaryChainSymbol != "" && !strings.EqualFold(in.PrimaryChainSymbol, symbol) {
		parentID, err := lookupThesisID(ctx, tx, in.PrimaryChainSymbol)
		if err == nil {
			res, _ := tx.ExecContext(ctx, `
				INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
				VALUES (?, ?, 'protocol_host', 'moderate', ?, 'system')
				ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
				parentID, id, fmt.Sprintf("Auto-created on D25 lock per adapter §3 — %s on %s.", adapter.Slug, in.PrimaryChainSymbol))
			if res != nil {
				if n, _ := res.RowsAffected(); n > 0 {
					rowsCreated = append(rowsCreated, fmt.Sprintf("%s → %s [protocol_host, moderate]", strings.ToUpper(in.PrimaryChainSymbol), symbol))
				}
			}
		}
	}

	// btc_beta_implicit upward — skip if btc_beta = 'reference' (self-cascade prevention)
	if btcBeta != "reference" {
		btcID, err := lookupThesisID(ctx, tx, "BTC")
		if err == nil && btcID != id {
			res, _ := tx.ExecContext(ctx, `
				INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
				VALUES (?, ?, 'btc_beta_implicit', 'weak', ?, 'system')
				ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
				btcID, id, fmt.Sprintf("Auto-created on D25 lock from btc_beta=%s tag.", btcBeta))
			if res != nil {
				if n, _ := res.RowsAffected(); n > 0 {
					rowsCreated = append(rowsCreated, fmt.Sprintf("BTC → %s [btc_beta_implicit, weak]", symbol))
				}
			}
		}
	}

	// Forward oracle_dependency — D25 Decision 5 structured field
	if in.OracleDependencyParentSymbol != "" {
		parentSymbol := strings.ToUpper(in.OracleDependencyParentSymbol)
		parentVersion := in.OracleDependencyParentVersion
		if parentVersion == "" {
			parentVersion = "v1"
		}
		var parentID int64
		var parentAdapterType string
		err := tx.QueryRowContext(ctx, `
			SELECT t.id, a.adapter_type
			  FROM crypto_theses t
			  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
			 WHERE t.coin_symbol = ? AND t.version = ?`, parentSymbol, parentVersion).Scan(&parentID, &parentAdapterType)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("oracle_dependency parent %s v%s not found", parentSymbol, parentVersion)
		}
		if err != nil {
			return nil, err
		}
		if parentID == id {
			return nil, ErrSelfDependency
		}
		if parentAdapterType != string(AdapterInfra) {
			return nil, ErrNonInfraOracleParent
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO crypto_thesis_dependencies (parent_thesis_id, child_thesis_id, dependency_type, cascade_strength, note, created_by)
			VALUES (?, ?, 'oracle_dependency', 'moderate', ?, 'system')
			ON CONFLICT(parent_thesis_id, child_thesis_id, dependency_type) DO NOTHING`,
			parentID, id, fmt.Sprintf("Auto-created on D25 lock per Decision 5 — %s declares dependency on Infrastructure parent %s.", symbol, parentSymbol))
		if err != nil {
			return nil, fmt.Errorf("insert oracle_dependency: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			rowsCreated = append(rowsCreated, fmt.Sprintf("%s → %s [oracle_dependency, moderate]", parentSymbol, symbol))
		} else {
			return nil, ErrDuplicateDependency
		}
	}

	// initial_lock history row
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crypto_thesis_history (thesis_id, event_type, event_reason, pillar_scores_json, total_score, band, delta, triggered_by)
		VALUES (?, 'initial_lock', 'd25_authored', ?, ?, ?, 0, 'user')`,
		id, pillarJSON, bandRes.Total, string(finalBand)); err != nil {
		return nil, fmt.Errorf("history insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &LockResult{
		ThesisID:           id,
		Symbol:             symbol,
		Version:            version,
		PillarScores:       map[string]int(scores),
		Total:              bandRes.Total,
		MaxScore:           adapter.ScorecardType.MaxScore(),
		RawBand:            bandRes.RawBand,
		FinalBand:          finalBand,
		PPGCapApplied:      bandRes.PPGCapApplied,
		PPGFailedGates:     bandRes.PPGFailedGates,
		VetoTriggered:      vetoTriggered,
		VetoReasons:        vetoReasons,
		CascadeRowsCreated: rowsCreated,
	}, nil
}

// reloadAsInput reconstructs a DraftThesisInput from a persisted draft row,
// used by Lock() to re-validate against current shape before transitioning.
func (s *ThesisWriteService) reloadAsInput(ctx context.Context, id int64) (*DraftThesisInput, error) {
	var in DraftThesisInput
	var coingecko, q5Mech, q9Note, catalystDate, catalystNote sql.NullString
	var q5Annual, q5FDV sql.NullFloat64
	var tagsJSON, venuesJSON, pillarJSON string
	var liquidityPassed int
	var ryr, paidRev, emissionsUSD, rabr, vaValue, supParUSD sql.NullFloat64
	var ageMonths sql.NullInt64
	var auditDate, auditor, custodyTier, custodyCadence, custodyJur sql.NullString
	var version, mdCurrent, btcBeta, horizon string
	var coinSymbol, coinName, adapterSlug string

	err := s.DB.QueryRowContext(ctx, `
		SELECT t.coin_symbol, t.coin_name, t.coingecko_id, a.slug,
		       t.pillar_scores_json,
		       t.q5_mechanism, t.q5_annual_usd, t.q5_fdv_usd,
		       t.q9_team_note, t.catalyst_date, t.catalyst_note,
		       t.holding_horizon, t.btc_beta, t.secondary_tags_json,
		       t.liquidity_passed, t.liquidity_venues_json,
		       t.q4_q5_ryr, t.q5_paid_revenue_usd, t.q5_emissions_usd, t.network_age_months,
		       t.q5_rabr, t.q5_verified_asset_value_usd, t.q5_token_supply_at_par_usd,
		       t.q5_audit_date, t.q5_auditor,
		       t.q6_custody_tier, t.q6_custody_cadence, t.q6_custody_jurisdiction,
		       t.version, t.markdown_current
		  FROM crypto_theses t
		  JOIN crypto_adapters a ON a.id = t.primary_adapter_id
		 WHERE t.id = ?`, id).Scan(
		&coinSymbol, &coinName, &coingecko, &adapterSlug,
		&pillarJSON,
		&q5Mech, &q5Annual, &q5FDV,
		&q9Note, &catalystDate, &catalystNote,
		&horizon, &btcBeta, &tagsJSON,
		&liquidityPassed, &venuesJSON,
		&ryr, &paidRev, &emissionsUSD, &ageMonths,
		&rabr, &vaValue, &supParUSD,
		&auditDate, &auditor,
		&custodyTier, &custodyCadence, &custodyJur,
		&version, &mdCurrent)
	if err != nil {
		return nil, err
	}
	in.Symbol = coinSymbol
	in.Name = coinName
	in.CoinGeckoID = coingecko.String
	in.AdapterSlug = adapterSlug
	in.Version = version
	in.Horizon = horizon
	in.BTCBeta = btcBeta
	in.MarkdownCurrent = mdCurrent
	in.Q5Mechanism = q5Mech.String
	if q5Annual.Valid {
		in.Q5AnnualUSD = q5Annual.Float64
	}
	if q5FDV.Valid {
		in.Q5FDVUSD = q5FDV.Float64
	}
	in.Q9TeamNote = q9Note.String
	in.CatalystDate = catalystDate.String
	in.CatalystNote = catalystNote.String
	var allTags []string
	_ = json.Unmarshal([]byte(tagsJSON), &allTags)
	in.SecondaryTags, _ = decodeMetaTags(allTags, &in)
	_ = json.Unmarshal([]byte(venuesJSON), &in.LiquidityVenues)
	lp := liquidityPassed != 0
	in.LiquidityPassed = &lp
	if ryr.Valid {
		v := ryr.Float64
		in.Q4Q5RYR = &v
	}
	if paidRev.Valid {
		v := paidRev.Float64
		in.Q5PaidRevenueUSD = &v
	}
	if emissionsUSD.Valid {
		v := emissionsUSD.Float64
		in.Q5EmissionsUSD = &v
	}
	if ageMonths.Valid {
		v := int(ageMonths.Int64)
		in.NetworkAgeMonths = &v
	}
	if rabr.Valid {
		v := rabr.Float64
		in.Q5RABR = &v
	}
	if vaValue.Valid {
		v := vaValue.Float64
		in.Q5VerifiedAssetValueUSD = &v
	}
	if supParUSD.Valid {
		v := supParUSD.Float64
		in.Q5TokenSupplyAtParUSD = &v
	}
	in.Q5AuditDate = auditDate.String
	in.Q5Auditor = auditor.String
	in.Q6CustodyTier = custodyTier.String
	in.Q6CustodyCadence = custodyCadence.String
	in.Q6CustodyJurisdiction = custodyJur.String

	// Parse pillar_scores_json — handles both legacy + compound shapes via
	// the shared ParseSubCriteriaJSON helper. Legacy shape (seed fixtures)
	// returns an empty SubCriteria map; ValidateForLock will reject because
	// no sub-criteria are present, which is correct: legacy seed fixtures
	// cannot be re-locked through D25 (they're already locked at fixture time).
	in.SubCriteria = ParseSubCriteriaJSON(pillarJSON)
	return &in, nil
}

// buildPillarScoresJSON produces the new shape `{"q1": {"subs":[...], "score": N}, ...}`.
// If sub-criteria are empty for a pillar, falls back to legacy `{"q1": N}` shape
// for that pillar to maintain backward compatibility with seed fixtures.
func buildPillarScoresJSON(subs map[string]SubCriteria, scores AllPillarScores) string {
	out := map[string]any{}
	for k, sc := range scores {
		if s, ok := subs[k]; ok && len(s) > 0 {
			out[k] = map[string]any{
				"subs":  []int(s),
				"score": sc,
			}
		} else {
			out[k] = sc
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// ----- small helpers -------------------------------------------------------

// encodeMetaTags serializes transient lock-time fields (primary chain,
// oracle dep parent, VETO conditions) as __meta__: prefixed tags. Stored
// alongside user-visible secondary_tags in secondary_tags_json; filtered
// out by the public read path (FilterMetaTags in scoring.go).
//
// Format examples:
//   __meta__:primary_chain=ETH
//   __meta__:oracle_dep_parent=LINK:v1
//   __meta__:veto=founder_rug,founder_rug_note=test rug
//   __meta__:veto_adapter=specific_key:on
func encodeMetaTags(in *DraftThesisInput) []string {
	var out []string
	if in.PrimaryChainSymbol != "" {
		out = append(out, "__meta__:primary_chain="+strings.ToUpper(in.PrimaryChainSymbol))
	}
	if in.OracleDependencyParentSymbol != "" {
		ver := in.OracleDependencyParentVersion
		if ver == "" {
			ver = "v1"
		}
		out = append(out, "__meta__:oracle_dep_parent="+strings.ToUpper(in.OracleDependencyParentSymbol)+":"+ver)
	}
	// VETO encoding — a single tag per triggered condition + notes
	if in.VetoConditions.UnlockCliffOver20Pct {
		out = append(out, "__meta__:veto=unlock_cliff_20pct_90d")
		if in.VetoConditions.UnlockCliffNote != "" {
			out = append(out, "__meta__:veto_note=unlock_cliff_20pct_90d:"+in.VetoConditions.UnlockCliffNote)
		}
	}
	if in.VetoConditions.ActiveSecEnforcement {
		out = append(out, "__meta__:veto=sec_action")
		if in.VetoConditions.ActiveSecEnforcementNote != "" {
			out = append(out, "__meta__:veto_note=sec_action:"+in.VetoConditions.ActiveSecEnforcementNote)
		}
	}
	if in.VetoConditions.HolderConcentrationOver50 {
		out = append(out, "__meta__:veto=holder_concentration_50pct")
		if in.VetoConditions.HolderConcentrationNote != "" {
			out = append(out, "__meta__:veto_note=holder_concentration_50pct:"+in.VetoConditions.HolderConcentrationNote)
		}
	}
	if in.VetoConditions.ExploitUnresolved60d {
		out = append(out, "__meta__:veto=exploit_unresolved_60d")
		if in.VetoConditions.ExploitUnresolved60dNote != "" {
			out = append(out, "__meta__:veto_note=exploit_unresolved_60d:"+in.VetoConditions.ExploitUnresolved60dNote)
		}
	}
	if in.VetoConditions.FounderRug {
		out = append(out, "__meta__:veto=founder_rug")
		if in.VetoConditions.FounderRugNote != "" {
			out = append(out, "__meta__:veto_note=founder_rug:"+in.VetoConditions.FounderRugNote)
		}
	}
	if in.VetoConditions.LiquidityPrefilterFail {
		out = append(out, "__meta__:veto=liquidity_prefilter_fail")
		if in.VetoConditions.LiquidityPrefilterFailNote != "" {
			out = append(out, "__meta__:veto_note=liquidity_prefilter_fail:"+in.VetoConditions.LiquidityPrefilterFailNote)
		}
	}
	for slug, on := range in.VetoConditions.AdapterSpecificConditions {
		if on {
			out = append(out, "__meta__:veto_adapter="+slug)
			if note := in.VetoConditions.AdapterSpecificNotes[slug]; note != "" {
				out = append(out, "__meta__:veto_adapter_note="+slug+":"+note)
			}
		}
	}
	return out
}

// decodeMetaTags strips __meta__: tags from an []string of tags and parses
// them back into the DraftThesisInput. Returns the non-meta tags + has-meta flag.
func decodeMetaTags(tags []string, in *DraftThesisInput) ([]string, bool) {
	if in.VetoConditions.AdapterSpecificConditions == nil {
		in.VetoConditions.AdapterSpecificConditions = map[string]bool{}
	}
	if in.VetoConditions.AdapterSpecificNotes == nil {
		in.VetoConditions.AdapterSpecificNotes = map[string]string{}
	}
	var keep []string
	hadMeta := false
	for _, t := range tags {
		if !strings.HasPrefix(t, "__meta__:") {
			keep = append(keep, t)
			continue
		}
		hadMeta = true
		body := strings.TrimPrefix(t, "__meta__:")
		switch {
		case strings.HasPrefix(body, "primary_chain="):
			in.PrimaryChainSymbol = strings.TrimPrefix(body, "primary_chain=")
		case strings.HasPrefix(body, "oracle_dep_parent="):
			parts := strings.SplitN(strings.TrimPrefix(body, "oracle_dep_parent="), ":", 2)
			in.OracleDependencyParentSymbol = parts[0]
			if len(parts) == 2 {
				in.OracleDependencyParentVersion = parts[1]
			}
		case strings.HasPrefix(body, "veto_note="):
			parts := strings.SplitN(strings.TrimPrefix(body, "veto_note="), ":", 2)
			if len(parts) == 2 {
				switch parts[0] {
				case "unlock_cliff_20pct_90d":
					in.VetoConditions.UnlockCliffNote = parts[1]
				case "sec_action":
					in.VetoConditions.ActiveSecEnforcementNote = parts[1]
				case "holder_concentration_50pct":
					in.VetoConditions.HolderConcentrationNote = parts[1]
				case "exploit_unresolved_60d":
					in.VetoConditions.ExploitUnresolved60dNote = parts[1]
				case "founder_rug":
					in.VetoConditions.FounderRugNote = parts[1]
				case "liquidity_prefilter_fail":
					in.VetoConditions.LiquidityPrefilterFailNote = parts[1]
				}
			}
		case strings.HasPrefix(body, "veto_adapter_note="):
			parts := strings.SplitN(strings.TrimPrefix(body, "veto_adapter_note="), ":", 2)
			if len(parts) == 2 {
				in.VetoConditions.AdapterSpecificNotes[parts[0]] = parts[1]
			}
		case strings.HasPrefix(body, "veto_adapter="):
			in.VetoConditions.AdapterSpecificConditions[strings.TrimPrefix(body, "veto_adapter=")] = true
		case strings.HasPrefix(body, "veto="):
			switch strings.TrimPrefix(body, "veto=") {
			case "unlock_cliff_20pct_90d":
				in.VetoConditions.UnlockCliffOver20Pct = true
			case "sec_action":
				in.VetoConditions.ActiveSecEnforcement = true
			case "holder_concentration_50pct":
				in.VetoConditions.HolderConcentrationOver50 = true
			case "exploit_unresolved_60d":
				in.VetoConditions.ExploitUnresolved60d = true
			case "founder_rug":
				in.VetoConditions.FounderRug = true
			case "liquidity_prefilter_fail":
				in.VetoConditions.LiquidityPrefilterFail = true
			}
		}
	}
	return keep, hadMeta
}

func lookupThesisID(ctx context.Context, tx *sql.Tx, symbol string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM crypto_theses WHERE coin_symbol = ? AND version = 'v1' AND status IN ('locked','needs-review')`,
		strings.ToUpper(symbol)).Scan(&id)
	return id, err
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullFloatPtr(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func nullIntPtr(i *int) any {
	if i == nil {
		return nil
	}
	return *i
}

func boolToInt(b *bool) int {
	if b != nil && *b {
		return 1
	}
	return 0
}

func conditionalUnix(when bool, ts int64) any {
	if !when {
		return nil
	}
	return ts
}
