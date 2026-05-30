// Package cryptotheses — D25 Scoring Engine Phase 1 (backend).
//
// scoring.go owns the canonical write-time pillar score + total score + band +
// PPG cap computation for newly created theses authored via D25.
//
// Doctrinal basis:
//   - v0.5 strict rounding rule (per v0.5 §L.9.1):
//       round DOWN if any sub-criterion = 0
//       round DOWN if 2+ sub-criteria ≤ 1 in a pillar with 5+ sub-criteria
//       otherwise round to nearest
//   - v0.5.1 #4 tie-breaking convention (per v0.6.1 §A, locked retroactively
//     2026-05-30 evening): round DOWN on exact 0.5/1.5 ties.
//   - PPG cap (per Spec 9l §15): single one-band-below penalty if Q1/Q2/Q6/Q9 < 1
//     OR if any pillar = 0. NOT stacked — multi-pillar failure = single cap.
//   - VETO trigger (per Spec 9l §20): forces band = Exit regardless of pillar math.
//
// The legacy ApplyV05Rounding helper in types.go is now wired to ComputePillarScore
// (round-down-on-tie). Callers should prefer ComputePillarScore going forward.
//
// JS client-side display mirror lives in web/js/scoring.js (TODO Phase 2);
// MUST match this function byte-for-byte. Drift-prevention test fixtures live
// in cmd/ft-d25-p1-test/scoring_fixtures.go.

package cryptotheses

import (
	"encoding/json"
	"math"
	"strings"
)

// ParsePillarScoresJSON parses both legacy `{"Q1": 1, ...}` and compound
// `{"Q1": {"subs": [...], "score": 1}, ...}` shapes into a map[string]int
// of just the scores. Keys are normalized to uppercase to match storage
// convention. Empty/invalid input returns an empty map (not nil).
//
// Compound shape is the D25-onwards storage shape per Decision 1 ("store
// both"). Legacy shape is preserved for the 12 seed fixtures that lack
// sub-criteria (per v0.6.1 §B implementation note).
func ParsePillarScoresJSON(raw string) map[string]int {
	out := map[string]int{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return out
	}
	for k, v := range top {
		key := strings.ToUpper(k)
		// Try compound first
		var compound struct {
			Score int   `json:"score"`
			Subs  []int `json:"subs"`
		}
		if err := json.Unmarshal(v, &compound); err == nil && compound.Subs != nil {
			out[key] = compound.Score
			continue
		}
		// Fallback to bare int (legacy shape)
		var intScore int
		if err := json.Unmarshal(v, &intScore); err == nil {
			out[key] = intScore
		}
	}
	return out
}

// ParseSubCriteriaJSON extracts the sub-criteria arrays from a compound
// pillar_scores_json. Returns empty map for legacy shape (seed fixtures).
func ParseSubCriteriaJSON(raw string) map[string][]int {
	out := map[string][]int{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return out
	}
	for k, v := range top {
		key := strings.ToUpper(k)
		var compound struct {
			Subs []int `json:"subs"`
		}
		if err := json.Unmarshal(v, &compound); err == nil && compound.Subs != nil {
			out[key] = compound.Subs
		}
	}
	return out
}

// SubCriteria stores raw sub-criterion scores for one pillar (each in {0,1,2}).
type SubCriteria []int

// ComputePillarScore applies v0.5 strict + v0.5.1 #4 tie-breaking to convert
// a slice of sub-criterion scores into a single pillar score in [0,2].
//
// Returns 0 for empty input. Result is clamped to [0,2].
func ComputePillarScore(subs SubCriteria) int {
	if len(subs) == 0 {
		return 0
	}
	sum := 0
	zeros := 0
	loweqOne := 0
	for _, s := range subs {
		sum += s
		if s == 0 {
			zeros++
		}
		if s <= 1 {
			loweqOne++
		}
	}
	avg := float64(sum) / float64(len(subs))

	// v0.5 rule 1: round DOWN if any sub-criterion = 0.
	if zeros > 0 {
		return clamp02(int(math.Floor(avg)))
	}
	// v0.5 rule 2: round DOWN if 2+ sub-criteria ≤ 1 in a 5+ sub-criterion pillar.
	if len(subs) >= 5 && loweqOne >= 2 {
		return clamp02(int(math.Floor(avg)))
	}
	// v0.5.1 #4: round DOWN on exact 0.5/1.5 ties.
	// (Floats that arise from int division can be tested exactly.)
	if avg == 0.5 || avg == 1.5 {
		return clamp02(int(math.Floor(avg)))
	}
	// Default: round to nearest (half-up).
	return clamp02(int(math.Round(avg)))
}

// AllPillarScores bundles pillar scores by key. Keys are "q1".."q9" for alt_18
// scorecards or "p1".."p6" for monetary_12 BTC scorecard (per BTC adapter MD).
type AllPillarScores map[string]int

// ComputeAllPillars iterates a pillar→sub-criteria map and returns computed
// pillar scores for each. Order-independent.
func ComputeAllPillars(subsByPillar map[string]SubCriteria) AllPillarScores {
	out := make(AllPillarScores, len(subsByPillar))
	for k, v := range subsByPillar {
		out[k] = ComputePillarScore(v)
	}
	return out
}

// ComputeTotal sums all pillar scores.
func ComputeTotal(scores AllPillarScores) int {
	total := 0
	for _, v := range scores {
		total += v
	}
	return total
}

// ComputeBandResult is the output of write-time scoring: raw band, final band
// after PPG cap, and a flag indicating whether the cap was applied.
type ComputeBandResult struct {
	Total           int
	RawBand         Band
	FinalBand       Band
	PPGCapApplied   bool
	PPGFailedGates  []string // {"Q2","Q5","Q8","no_pillar_zero"} for audit
}

// ComputeRawAndFinalBand computes raw band from total score, evaluates PPG
// (using the alt_18 gate Q1/Q2/Q6/Q9 ≥ 1 plus universal no-zero-pillar),
// and applies a single one-band-below cap if any gate failed.
//
// For monetary_12 scorecards (BTC), only the "no pillar = 0" rule applies
// — there is no formal Q1/Q2/Q6/Q9 gate equivalent.
//
// PPG cap is applied for all failing band ranges except Exit (Exit can't drop
// further). Multi-pillar failure produces a SINGLE cap (not stacked) per
// Spec 9l §15.
func ComputeRawAndFinalBand(scores AllPillarScores, sc ScorecardType) ComputeBandResult {
	total := ComputeTotal(scores)
	raw := ComputeBand(total, sc)

	var failedGates []string
	hasZero := false
	for k, v := range scores {
		if v == 0 {
			hasZero = true
			failedGates = append(failedGates, k+"_pillar_zero")
		}
	}
	if sc == ScorecardAlt18 {
		// Specific Q1/Q2/Q6/Q9 ≥ 1 gates.
		for _, k := range []string{"Q1", "Q2", "Q6", "Q9"} {
			if v, ok := scores[k]; ok && v < 1 {
				// already counted as zero above; but mark explicit gate fail too
				failedGates = append(failedGates, k+"_below_one")
			}
		}
	}

	ppgFailed := hasZero
	// Sort failedGates for deterministic output (audit-stable). Cheap sort.
	if len(failedGates) > 1 {
		for i := 0; i < len(failedGates); i++ {
			for j := i + 1; j < len(failedGates); j++ {
				if failedGates[j] < failedGates[i] {
					failedGates[i], failedGates[j] = failedGates[j], failedGates[i]
				}
			}
		}
	}

	final := raw
	capped := false
	if ppgFailed && raw != BandExit {
		final = oneBandBelow(raw)
		capped = true
	}

	return ComputeBandResult{
		Total:          total,
		RawBand:        raw,
		FinalBand:      final,
		PPGCapApplied:  capped,
		PPGFailedGates: failedGates,
	}
}

// ApplyVeto forces band = Exit if any VETO condition is triggered.
// Returns (final band, veto triggered, list of triggered VETO slugs).
//
// Per Spec 9l §20, VETO overrides ALL pillar math and PPG cap logic. It is
// the only mechanism that can produce Exit band regardless of total score.
func ApplyVeto(currentBand Band, conditions VetoConditions) (Band, bool, []string) {
	triggered := conditions.Triggered()
	if len(triggered) > 0 {
		return BandExit, true, triggered
	}
	return currentBand, false, nil
}

// FilterMetaTags strips internal `__meta__:` prefixed tags from a tags slice.
// Public read paths (ThesisRow, ThesisDetail) should call this so the
// transient D25 lock-time state doesn't leak to API consumers.
func FilterMetaTags(tags []string) []string {
	out := tags[:0]
	for _, t := range tags {
		if !strings.HasPrefix(t, "__meta__:") {
			out = append(out, t)
		}
	}
	return out
}

// oneBandBelow returns the band one step worse (toward Exit). Exit is the
// floor; calling on Exit returns Exit (no-op).
func oneBandBelow(b Band) Band {
	switch b {
	case BandStrong:
		return BandAccumulate
	case BandAccumulate:
		return BandHold
	case BandHold:
		return BandTrim
	case BandTrim:
		return BandExit
	case BandExit:
		return BandExit
	}
	return b
}
