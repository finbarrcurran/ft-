// Package cryptotheses — Spec 9l Crypto Thesis Framework.
//
// types.go (Deliverable D10) is the JSON envelope + enum + validation layer.
// Service + handlers in service.go (D11). Adapter MDs live in seed/ — Phase 1
// they arrive externally (authored by Claude.ai) rather than embedded with
// the binary like 9g, because the adapter MD work stream runs in parallel to
// the framework build.
//
// This file owns:
//   - Enums for adapter type, scorecard type, band, horizon, BTC-beta, Q5
//     mechanism, status (all CHECK-constrained at the DB layer too)
//   - PillarScores type that handles both 9-Q /18 alts and 6-pillar /12 BTC
//   - Q5Detail (named mechanism + $ figure + computed FDV %)
//   - AdapterEnvelope (parsed JSON wrapper around crypto_adapters row)
//   - ThesisEnvelope (parsed JSON wrapper around crypto_theses row)
//   - KillCriterion (one row of an adapter's kill_criteria_json)
//   - Computed-field helpers: ComputeBand, ComputePillarPassGate, ComputeDelta,
//     RecommendedActionFromDelta
//   - Validate() methods that the API layer should call before INSERT/UPDATE

package cryptotheses

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ----- Enums ------------------------------------------------------------

// AdapterType is the 8-way functional taxonomy from Spec 9l Decision #2.
type AdapterType string

const (
	AdapterBTC         AdapterType = "btc"
	AdapterL1          AdapterType = "l1"
	AdapterL2          AdapterType = "l2"
	AdapterDeFi        AdapterType = "defi"
	AdapterInfra       AdapterType = "infra"
	AdapterDePIN       AdapterType = "depin"
	AdapterRWA         AdapterType = "rwa"
	AdapterSpeculative AdapterType = "speculative"
)

var validAdapterTypes = map[AdapterType]bool{
	AdapterBTC: true, AdapterL1: true, AdapterL2: true, AdapterDeFi: true,
	AdapterInfra: true, AdapterDePIN: true, AdapterRWA: true, AdapterSpeculative: true,
}

func (a AdapterType) Valid() bool { return validAdapterTypes[a] }

// ScorecardType — Spec 9l Decision #1. Two distinct scorecards.
type ScorecardType string

const (
	ScorecardAlt18      ScorecardType = "alt_18"      // 9-Q /18 for non-BTC
	ScorecardMonetary12 ScorecardType = "monetary_12" // 6-pillar /12 for BTC
)

func (s ScorecardType) Valid() bool {
	return s == ScorecardAlt18 || s == ScorecardMonetary12
}

func (s ScorecardType) MaxScore() int {
	if s == ScorecardMonetary12 {
		return 12
	}
	return 18
}

// Band — Spec 9l Decision #7. Same 5-band structure for both scorecards,
// different thresholds.
type Band string

const (
	BandStrong     Band = "strong"
	BandAccumulate Band = "accumulate"
	BandHold       Band = "hold"
	BandTrim       Band = "trim"
	BandExit       Band = "exit"
)

func (b Band) Valid() bool {
	switch b {
	case BandStrong, BandAccumulate, BandHold, BandTrim, BandExit:
		return true
	}
	return false
}

// Rank returns ordering for band-comparison (0 = best, 4 = worst).
// Used by cascade trigger logic (band-drop detection).
func (b Band) Rank() int {
	switch b {
	case BandStrong:
		return 0
	case BandAccumulate:
		return 1
	case BandHold:
		return 2
	case BandTrim:
		return 3
	case BandExit:
		return 4
	}
	return -1
}

// HoldingHorizon — Spec 9l Decision #9. Six options including Never-Sell.
// Drives cadence, NOT scoring (Spec 9l Handoff implementation trap #5).
type HoldingHorizon string

const (
	HorizonNeverSell HoldingHorizon = "never_sell"
	HorizonCycle     HoldingHorizon = "cycle"      // 3-4yr
	HorizonMultiYear HoldingHorizon = "multi_year" // 1-3yr
	HorizonMedium    HoldingHorizon = "medium"     // 3-12mo
	HorizonTrade     HoldingHorizon = "trade"      // <3mo
	HorizonTBD       HoldingHorizon = "tbd"
)

func (h HoldingHorizon) Valid() bool {
	switch h {
	case HorizonNeverSell, HorizonCycle, HorizonMultiYear,
		HorizonMedium, HorizonTrade, HorizonTBD:
		return true
	}
	return false
}

// RescoreCadenceDays returns the default re-score cadence implied by the
// horizon (Spec 9l Decision #10). Speculative adapter override is applied
// downstream in the cron, not here.
func (h HoldingHorizon) RescoreCadenceDays() int {
	switch h {
	case HorizonTrade:
		return 7
	case HorizonMedium:
		return 14
	case HorizonMultiYear, HorizonCycle, HorizonNeverSell, HorizonTBD:
		return 30
	}
	return 30
}

// BTCBeta — Spec 9l Decision #22. Correlation awareness tag.
type BTCBeta string

const (
	BetaHigh      BTCBeta = "high"
	BetaMedium    BTCBeta = "medium"
	BetaLow       BTCBeta = "low"
	BetaInverse   BTCBeta = "inverse"
	BetaReference BTCBeta = "reference" // Spec 9l v0.6 §A.5 / Migration 0033 — self-reference (BTC v1 only)
)

func (b BTCBeta) Valid() bool {
	switch b {
	case BetaHigh, BetaMedium, BetaLow, BetaInverse, BetaReference:
		return true
	}
	return false
}

// Q5Mechanism — Spec 9l Decision #5. Named accrual mechanism enum prevents
// gaming Q5 with free text. No mechanism → score 0 for Q5 by definition.
type Q5Mechanism string

const (
	// v0.1 original set
	Q5FeeBurn        Q5Mechanism = "fee_burn"
	Q5FeeShare       Q5Mechanism = "fee_share"
	Q5Buyback        Q5Mechanism = "buyback"
	Q5StakingYield   Q5Mechanism = "staking_yield"
	Q5GovernanceOnly Q5Mechanism = "governance_only"
	Q5None           Q5Mechanism = "none"
	Q5Other          Q5Mechanism = "other"
	// Migration 0033 additions (Spec 9l v0.4 §B + v0.5 §H)
	Q5DirectAssetClaim        Q5Mechanism = "direct_asset_claim"          // RWA — 1:1 backed claim (BUIDL)
	Q5RequiredForService      Q5Mechanism = "required_for_service"        // Infrastructure — required for service (LINK)
	Q5DSRSurplus              Q5Mechanism = "dsr_surplus"                 // DeFi stablecoin-issuer (Sky/MKR)
	Q5BurnAndMint             Q5Mechanism = "burn_and_mint"               // DePIN BME / Speculative transaction-tax (RNDR, LUNC)
	Q5BuybackStake            Q5Mechanism = "buyback_stake"               // DeFi/Infra buyback held to treasury
	Q5RealYieldStaking        Q5Mechanism = "real_yield_staking"          // Distinct from staking_yield — paid from real fees (AAVE)
	Q5GovernanceWithFeeSwitch Q5Mechanism = "governance_with_fee_switch"  // UNI-class — governance controls dormant fee switch
)

func (q Q5Mechanism) Valid() bool {
	switch q {
	case Q5FeeBurn, Q5FeeShare, Q5Buyback, Q5StakingYield,
		Q5GovernanceOnly, Q5None, Q5Other,
		Q5DirectAssetClaim, Q5RequiredForService, Q5DSRSurplus,
		Q5BurnAndMint, Q5BuybackStake, Q5RealYieldStaking,
		Q5GovernanceWithFeeSwitch:
		return true
	}
	return false
}

// CustodyTier — Spec 9l v0.6 §A.4 / Migration 0033 (RWA adapter §3).
// Tier 3 structurally caps Q6 below Strong Conviction band-eligible.
type CustodyTier string

const (
	CustodyTier1 CustodyTier = "tier_1" // monthly Big-4 OR daily admin by G-SIB
	CustodyTier2 CustodyTier = "tier_2" // quarterly mid-firm
	CustodyTier3 CustodyTier = "tier_3" // annual / none — caps Q6 below Strong
)

func (c CustodyTier) Valid() bool {
	switch c {
	case CustodyTier1, CustodyTier2, CustodyTier3:
		return true
	}
	return false
}

// Status — Spec 9l Decision #23 (watching added) + Decision #24 (forked).
type Status string

const (
	StatusDraft       Status = "draft"
	StatusLocked      Status = "locked"
	StatusNeedsReview Status = "needs-review"
	StatusWatching    Status = "watching"
	StatusInvalidated Status = "invalidated"
	StatusForked      Status = "forked"
)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusLocked, StatusNeedsReview,
		StatusWatching, StatusInvalidated, StatusForked:
		return true
	}
	return false
}

// AdapterStatus is the narrower status set used by crypto_adapters
// (no watching/invalidated/forked — adapters don't take those states).
type AdapterStatus string

const (
	AdapterStatusDraft       AdapterStatus = "draft"
	AdapterStatusLocked      AdapterStatus = "locked"
	AdapterStatusNeedsReview AdapterStatus = "needs-review"
)

func (s AdapterStatus) Valid() bool {
	switch s {
	case AdapterStatusDraft, AdapterStatusLocked, AdapterStatusNeedsReview:
		return true
	}
	return false
}

// DependencyType — Spec 9l v0.2 §E cascade graph edge enum.
type DependencyType string

const (
	DepPlatformParent     DependencyType = "platform_parent"
	DepProtocolHost       DependencyType = "protocol_host"
	DepOracleDependency   DependencyType = "oracle_dependency"
	DepNarrativeCorrelated DependencyType = "narrative_correlated"
	DepBTCBetaImplicit    DependencyType = "btc_beta_implicit"
)

func (d DependencyType) Valid() bool {
	switch d {
	case DepPlatformParent, DepProtocolHost, DepOracleDependency,
		DepNarrativeCorrelated, DepBTCBetaImplicit:
		return true
	}
	return false
}

// CascadeStrength is the soft-vs-hard weighting on a dependency edge.
type CascadeStrength string

const (
	StrengthStrong   CascadeStrength = "strong"
	StrengthModerate CascadeStrength = "moderate"
	StrengthWeak     CascadeStrength = "weak"
)

func (c CascadeStrength) Valid() bool {
	switch c {
	case StrengthStrong, StrengthModerate, StrengthWeak:
		return true
	}
	return false
}

// ----- Pillar scores -----------------------------------------------------

// PillarScores wraps both 9-Q (Q1..Q9) and 6-pillar (M1..M6) shapes.
// We use a generic map for flexibility but provide helpers that enforce
// schema-correctness based on the ScorecardType.
//
// JSON shape:
//   alts:  {"Q1":2,"Q2":2,"Q3":1,"Q4":1,"Q5":2,"Q6":2,"Q7":1,"Q8":2,"Q9":2}
//   btc:   {"M1":2,"M2":1,"M3":2,"M4":2,"M5":1,"M6":2}
type PillarScores map[string]int

// Validate checks all required keys are present, all values in [0,2],
// no extra keys.
func (p PillarScores) Validate(sc ScorecardType) error {
	required := requiredPillars(sc)
	if len(p) != len(required) {
		return fmt.Errorf("expected %d pillars for %s, got %d", len(required), sc, len(p))
	}
	for _, key := range required {
		v, ok := p[key]
		if !ok {
			return fmt.Errorf("missing pillar %s", key)
		}
		if v < 0 || v > 2 {
			return fmt.Errorf("pillar %s out of range [0,2]: got %d", key, v)
		}
	}
	// Reject extra keys.
	for k := range p {
		if !contains(required, k) {
			return fmt.Errorf("unexpected pillar %s for %s scorecard", k, sc)
		}
	}
	return nil
}

// Total sums all pillar values. Caller should validate first.
func (p PillarScores) Total() int {
	sum := 0
	for _, v := range p {
		sum += v
	}
	return sum
}

func requiredPillars(sc ScorecardType) []string {
	if sc == ScorecardMonetary12 {
		return []string{"M1", "M2", "M3", "M4", "M5", "M6"}
	}
	return []string{"Q1", "Q2", "Q3", "Q4", "Q5", "Q6", "Q7", "Q8", "Q9"}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ----- Q5 detail ---------------------------------------------------------

// Q5Detail is the structured Q5 record required by Decision #5.
// AccrualPct is computed from AnnualUSD / FDVUSD * 100; null when
// FDV is zero or mechanism is None.
type Q5Detail struct {
	Mechanism  Q5Mechanism `json:"mechanism"`
	AnnualUSD  float64     `json:"annualUSD"`
	FDVUSD     float64     `json:"fdvUSD"`
	AccrualPct *float64    `json:"accrualPct,omitempty"`
	Note       string      `json:"note,omitempty"`
}

func (q *Q5Detail) Compute() {
	if q.Mechanism == Q5None || q.FDVUSD <= 0 {
		q.AccrualPct = nil
		return
	}
	pct := (q.AnnualUSD / q.FDVUSD) * 100
	q.AccrualPct = &pct
}

func (q Q5Detail) Validate() error {
	if !q.Mechanism.Valid() {
		return fmt.Errorf("q5 mechanism invalid: %q", q.Mechanism)
	}
	if q.Mechanism != Q5None && q.Mechanism != Q5GovernanceOnly {
		if q.AnnualUSD < 0 {
			return errors.New("q5 annualUSD must be non-negative")
		}
		if q.FDVUSD <= 0 {
			return errors.New("q5 fdvUSD must be positive when mechanism is active")
		}
	}
	return nil
}

// ----- Adapter envelope -------------------------------------------------

// KillCriterion is one entry in an adapter's kill_criteria_json array.
// Adapter MDs define these; the engine evaluates them against per-coin
// live data when re-scoring (Spec 9l §2 #21).
type KillCriterion struct {
	Slug        string `json:"slug"`        // 'mainnet_halt_24h', 'real_yield_negative_90d'
	Description string `json:"description"` // human label for UI/log
	Pillar      string `json:"pillar"`      // which pillar this maps to (Q6, Q3, ...)
	Threshold   string `json:"threshold"`   // raw threshold expression for v1; structured later
}

// Adapter is the row shape returned by Service.List / Get.
type Adapter struct {
	ID                 int64           `json:"id"`
	Slug               string          `json:"slug"`
	DisplayName        string          `json:"displayName"`
	ShortDescription   string          `json:"shortDescription"`
	AdapterType        AdapterType     `json:"adapterType"`
	ScorecardType      ScorecardType   `json:"scorecardType"`
	CurrentVersion     string          `json:"currentVersion"`
	Status             AdapterStatus   `json:"status"`
	MarkdownCurrent    string          `json:"markdownCurrent,omitempty"`
	RenderedHTML       string          `json:"renderedHTML,omitempty"`
	PrimaryDataSources []string        `json:"primaryDataSources"`
	KillCriteria       []KillCriterion `json:"killCriteria"`
	IsDoctrine         bool            `json:"isDoctrine"`
	GithubPath         string          `json:"githubPath,omitempty"`
	GithubURL          string          `json:"githubURL,omitempty"`
	FileSHA            string          `json:"fileSHA,omitempty"`
	CreatedAt          int64           `json:"createdAt"` // unix epoch
	UpdatedAt          int64           `json:"updatedAt"`
	LockedAt           *int64          `json:"lockedAt,omitempty"`
}

// Validate checks enum fields. Markdown body validated by markdown layer.
func (a Adapter) Validate() error {
	if strings.TrimSpace(a.Slug) == "" {
		return errors.New("slug required")
	}
	if !a.AdapterType.Valid() {
		return fmt.Errorf("adapterType invalid: %q", a.AdapterType)
	}
	if !a.ScorecardType.Valid() {
		return fmt.Errorf("scorecardType invalid: %q", a.ScorecardType)
	}
	// BTC adapter MUST use monetary_12; everything else MUST use alt_18.
	if a.AdapterType == AdapterBTC && a.ScorecardType != ScorecardMonetary12 {
		return errors.New("BTC adapter must use monetary_12 scorecard")
	}
	if a.AdapterType != AdapterBTC && a.ScorecardType != ScorecardAlt18 {
		return fmt.Errorf("non-BTC adapter %s must use alt_18 scorecard", a.AdapterType)
	}
	if !a.Status.Valid() {
		return fmt.Errorf("status invalid: %q", a.Status)
	}
	for i, kc := range a.KillCriteria {
		if strings.TrimSpace(kc.Slug) == "" {
			return fmt.Errorf("kill criterion %d missing slug", i)
		}
	}
	return nil
}

// MarshalKillCriteria returns the JSON string ready for INSERT/UPDATE.
func (a Adapter) MarshalKillCriteria() (string, error) {
	if a.KillCriteria == nil {
		return "[]", nil
	}
	b, err := json.Marshal(a.KillCriteria)
	return string(b), err
}

// MarshalDataSources serialises the data source slug array.
func (a Adapter) MarshalDataSources() (string, error) {
	if a.PrimaryDataSources == nil {
		return "[]", nil
	}
	b, err := json.Marshal(a.PrimaryDataSources)
	return string(b), err
}

// ----- Band computation -------------------------------------------------

// ComputeBand returns the band for a score given the scorecard type.
// Per Spec 9l Decision #7:
//   alts /18:  13-18 Strong, 10-12 Accumulate, 7-9 Hold, 4-6 Trim, 0-3 Exit
//   BTC /12:   9-12 Strong,  7-8 Accumulate,   5-6 Hold, 3-4 Trim, 0-2 Exit
func ComputeBand(score int, sc ScorecardType) Band {
	if sc == ScorecardMonetary12 {
		switch {
		case score >= 9:
			return BandStrong
		case score >= 7:
			return BandAccumulate
		case score >= 5:
			return BandHold
		case score >= 3:
			return BandTrim
		default:
			return BandExit
		}
	}
	// alt_18
	switch {
	case score >= 13:
		return BandStrong
	case score >= 10:
		return BandAccumulate
	case score >= 7:
		return BandHold
	case score >= 4:
		return BandTrim
	default:
		return BandExit
	}
}

// ComputePillarPassGate returns true if the pillar pass gate PASSES.
// Per Spec 9l Decision #12:
//   - For alts: Q1 >= 1, Q2 >= 1, Q6 >= 1, Q9 >= 1, no pillar = 0
//   - For BTC: no formal pass gate defined; we apply "no pillar = 0" only
//
// A failed pass gate caps the band at next-step-down from Strong
// (handled in the band-application layer, not here).
func ComputePillarPassGate(scores PillarScores, sc ScorecardType) bool {
	for _, v := range scores {
		if v == 0 {
			return false
		}
	}
	if sc == ScorecardMonetary12 {
		// "no pillar = 0" is the only formal BTC gate criterion.
		return true
	}
	// alt_18 gate
	for _, key := range []string{"Q1", "Q2", "Q6", "Q9"} {
		if scores[key] < 1 {
			return false
		}
	}
	return true
}

// ApplyPassGate caps the band one step down if the gate failed and the
// computed band is Strong. Other bands are unaffected.
func ApplyPassGate(band Band, gatePassed bool) Band {
	if gatePassed || band != BandStrong {
		return band
	}
	return BandAccumulate
}

// ----- Delta + recommended action ---------------------------------------

// RecommendedAction maps a score delta to the action enum per Spec 9l
// Decision #8 score-decay ladder.
type RecommendedAction string

const (
	ActionNone     RecommendedAction = "none"
	ActionLogOnly  RecommendedAction = "log_only"
	ActionTrim25   RecommendedAction = "trim_25"
	ActionTrim50   RecommendedAction = "trim_50"
	ActionExit     RecommendedAction = "exit"
	ActionOverride RecommendedAction = "override"
)

// RecommendedActionFromDelta implements the score-decay ladder:
//   Δ ≥ 0: none
//   Δ = -1: log_only
//   Δ = -2: trim_25
//   Δ = -3: trim_50
//   Δ <= -4: exit
func RecommendedActionFromDelta(delta int) RecommendedAction {
	switch {
	case delta >= 0:
		return ActionNone
	case delta == -1:
		return ActionLogOnly
	case delta == -2:
		return ActionTrim25
	case delta == -3:
		return ActionTrim50
	default:
		return ActionExit
	}
}

// BandDropped reports whether new band is worse than old band. Used by
// cascade trigger logic (Spec 9l v0.2 §E).
func BandDropped(oldBand, newBand Band) bool {
	return newBand.Rank() > oldBand.Rank()
}

// ----- VETO conditions --------------------------------------------------

// VetoReason slugs. Universal VETOs + adapter-specific kill criteria
// share this enum surface (caller distinguishes via Source field).
type VetoReason string

const (
	VetoUnlockCliff20Pct90d   VetoReason = "unlock_cliff_20pct_90d"
	VetoSECAction             VetoReason = "sec_action"
	VetoHolderConcentration50 VetoReason = "holder_concentration_50pct"
	VetoExploitUnresolved60d  VetoReason = "exploit_unresolved_60d"
	VetoFounderRug            VetoReason = "founder_rug"
	VetoLiquidityPrefilter    VetoReason = "liquidity_prefilter_fail"
	VetoAdapterSpecific       VetoReason = "adapter_specific" // detail in active_veto_reason
)

func (v VetoReason) Valid() bool {
	switch v {
	case VetoUnlockCliff20Pct90d, VetoSECAction, VetoHolderConcentration50,
		VetoExploitUnresolved60d, VetoFounderRug, VetoLiquidityPrefilter,
		VetoAdapterSpecific:
		return true
	}
	return false
}

// ----- Allocation panel -------------------------------------------------

// Allocation is the user-facing allocation panel state — Spec 9l Decision #21.
type Allocation struct {
	PctStocks float64 `json:"pctStocks"`
	PctBTC    float64 `json:"pctBTC"`
	PctETH    float64 `json:"pctETH"`
	PctAlts   float64 `json:"pctAlts"`
	PctCash   float64 `json:"pctCash"`
	Note      string  `json:"note,omitempty"`
	UpdatedAt int64   `json:"updatedAt,omitempty"`
}

// Validate ensures all percentages are in [0,100] and sum to 100 within
// rounding tolerance (DB CHECK enforces 0.01 tolerance too).
func (a Allocation) Validate() error {
	for label, v := range map[string]float64{
		"stocks": a.PctStocks, "btc": a.PctBTC, "eth": a.PctETH,
		"alts": a.PctAlts, "cash": a.PctCash,
	} {
		if v < 0 || v > 100 {
			return fmt.Errorf("pct_%s out of range [0,100]: got %g", label, v)
		}
	}
	sum := a.PctStocks + a.PctBTC + a.PctETH + a.PctAlts + a.PctCash
	if diff := sum - 100; diff > 0.01 || diff < -0.01 {
		return fmt.Errorf("allocation percentages must sum to 100, got %g", sum)
	}
	return nil
}

// ----- Errors ------------------------------------------------------------

var (
	ErrNotFound       = errors.New("crypto adapter or thesis not found")
	ErrIsDoctrine     = errors.New("adapter is doctrine; UI edit blocked")
	ErrInvalidStatus  = errors.New("status invalid for this transition")
	ErrVersionExists  = errors.New("version already exists in history")
	ErrValidation     = errors.New("validation failed")
	ErrLiquidityGate  = errors.New("coin failed liquidity pre-filter (not on Kraken/Coinbase/Binance)")
	ErrPillarMismatch = errors.New("pillar score shape does not match scorecard type")
)

// DriftThresholdPct — Spec 9l v0.2 §B. Locked at ±15% (crypto noisier
// than equity fundamentals; original ±10% inherited from stock context).
const DriftThresholdPct = 0.15

// ApplyV05Rounding implements the canonical sub-criterion → pillar rounding
// rule per **Spec 9l v0.5 §L.9.1** (supersedes v0.4 §C, 2026-05-30 PM late).
//
//	"Round down if any sub-criterion = 0,
//	 OR if 2+ sub-criteria ≤ 1 in a pillar with 5+ sub-criteria.
//	 Otherwise round to nearest."
//
// Verified against all 8 Phase 1 locked theses: only LINK v1 needed
// re-locking (Q6 + Q9 from 1 → 2; 14/18 → 16/18, Strong Conviction band
// unchanged). v0.5 §L.9.5 records the audit; §L.9.4 the SQL.
//
// 4-sub-criterion pillar interpretation: **Option B (strict 5+ reading)**
// per v0.5 §L.9.2. Rule does NOT extend to 4-pillar; "round to nearest"
// default applies. Future extension would be v0.5.1 patch territory.
//
// Used by the Scoring Engine (D25, Phase 1+2) when computing pillar scores
// from individual sub-criterion inputs. D25 ComputePillarScore (scoring.go)
// is the canonical wrapper; this helper kept as direct-callable reference.
//
// **v0.5.1 #4 tie-breaking** (per v0.6.1 §A, locked retroactively 2026-05-30
// evening): round DOWN on exact 0.5/1.5 ties. Forced by empirical application
// in AAVE Q1 + EIGEN Q1 (both sub-criteria [1,2,2,1], avg 1.5 → pillar = 1).
//
// Sub-criteria expected in [0, 2]; result clamped to [0, 2].
func ApplyV05Rounding(subCriteria []int) int {
	return ComputePillarScore(SubCriteria(subCriteria))
}

func clamp02(v int) int {
	if v < 0 {
		return 0
	}
	if v > 2 {
		return 2
	}
	return v
}
