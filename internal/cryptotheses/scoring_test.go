// scoring_test.go — drift-prevention coverage for D25 write-time scoring.
//
// Fixtures pinned to the 12 locked theses (AAVE/EIGEN are most relevant
// because they exercise v0.5.1 #4 tie-breaking and multi-zero PPG cap).
// Any future change to ComputePillarScore that breaks these tests would
// silently fork the framework — fail loud here.

package cryptotheses

import (
	"reflect"
	"testing"
)

func TestComputePillarScore_AAVE_Q1_TieBreak(t *testing.T) {
	// AAVE v1 Q1: sub-criteria 1, 2, 2, 1 → avg 1.5 → tie → round DOWN → 1.
	// Per v0.6.1 §A empirical lock.
	got := ComputePillarScore(SubCriteria{1, 2, 2, 1})
	if got != 1 {
		t.Fatalf("AAVE Q1 tie-break: got %d, want 1", got)
	}
}

func TestComputePillarScore_EIGEN_Q1_TieBreak(t *testing.T) {
	// EIGEN v1 Q1: sub-criteria 1, 2, 2, 1 → avg 1.5 → tie → round DOWN → 1.
	got := ComputePillarScore(SubCriteria{1, 2, 2, 1})
	if got != 1 {
		t.Fatalf("EIGEN Q1 tie-break: got %d, want 1", got)
	}
}

func TestComputePillarScore_EIGEN_Q2_ZeroRoundDown(t *testing.T) {
	// EIGEN v1 Q2: sub-criteria 0, 0, 1, 0, 1 → avg 0.4 → any zero → round DOWN → 0.
	got := ComputePillarScore(SubCriteria{0, 0, 1, 0, 1})
	if got != 0 {
		t.Fatalf("EIGEN Q2: got %d, want 0", got)
	}
}

func TestComputePillarScore_EIGEN_Q8_TripleZero(t *testing.T) {
	// EIGEN v1 Q8: sub-criteria 0, 0, 1, 1, 2, 0, 1, 1 → avg 0.75 → zeros → 0.
	got := ComputePillarScore(SubCriteria{0, 0, 1, 1, 2, 0, 1, 1})
	if got != 0 {
		t.Fatalf("EIGEN Q8: got %d, want 0", got)
	}
}

func TestComputePillarScore_LINK_Q6_FivePlusTwoOnes(t *testing.T) {
	// LINK v1 Q6 (re-locked under v0.5): sub-criteria 2, 1, 2, 2, 2, 2 (6-pillar).
	// avg 1.83, only 1 sub at 1 → round to NEAREST 2.
	got := ComputePillarScore(SubCriteria{2, 1, 2, 2, 2, 2})
	if got != 2 {
		t.Fatalf("LINK Q6: got %d, want 2", got)
	}
}

func TestComputePillarScore_FivePlusTwoLowOnes(t *testing.T) {
	// 5-pillar with 2+ sub-criteria ≤ 1 → round DOWN.
	// 1, 2, 1, 2, 2 → avg 1.6 → 2 subs ≤ 1 → round DOWN → 1.
	got := ComputePillarScore(SubCriteria{1, 2, 1, 2, 2})
	if got != 1 {
		t.Fatalf("5+ pillar 2 lows: got %d, want 1", got)
	}
}

func TestComputePillarScore_RoundUpClean(t *testing.T) {
	// 2, 2, 2, 1 → avg 1.75 → no tie, no zero, 4-pillar → round NEAREST → 2.
	got := ComputePillarScore(SubCriteria{2, 2, 2, 1})
	if got != 2 {
		t.Fatalf("clean round up: got %d, want 2 (avg 1.75)", got)
	}
}

func TestComputePillarScore_HalfTieRoundDown(t *testing.T) {
	// 0.5 exact tie: 1, 1, 0, 0 has zero → 0 regardless.
	// To get clean 0.5 tie without zero: not possible with subs in {0,1,2}
	// without zeros. Document the property: 0.5 tie always co-occurs with
	// zeros (caught by rule 1). 1.5 tie can occur without zeros (AAVE Q1).
	// Sanity: 1, 1, 1, 1 → avg 1 → exact value 1 → returns 1 (not tie).
	if got := ComputePillarScore(SubCriteria{1, 1, 1, 1}); got != 1 {
		t.Fatalf("all-ones: got %d, want 1", got)
	}
}

func TestComputePillarScore_EmptyZero(t *testing.T) {
	if got := ComputePillarScore(SubCriteria{}); got != 0 {
		t.Fatalf("empty: got %d, want 0", got)
	}
}

func TestComputePillarScore_ClampHigh(t *testing.T) {
	// 5+ pillar with all 2s — should round to 2.
	if got := ComputePillarScore(SubCriteria{2, 2, 2, 2, 2}); got != 2 {
		t.Fatalf("all 2s: got %d, want 2", got)
	}
}

func TestComputeRawAndFinalBand_EIGEN_TripleZeroSingleCap(t *testing.T) {
	// EIGEN: Q1=1, Q2=0, Q3=2, Q4=1, Q5=0, Q6=1, Q7=2, Q8=0, Q9=2 = 9
	// Three pillars at 0 → SINGLE band cap, not stacked.
	// Raw 9 = Hold (alt_18 7-9); one band below = Trim.
	scores := AllPillarScores{
		"Q1": 1, "Q2": 0, "Q3": 2, "Q4": 1, "Q5": 0,
		"Q6": 1, "Q7": 2, "Q8": 0, "Q9": 2,
	}
	res := ComputeRawAndFinalBand(scores, ScorecardAlt18)
	if res.Total != 9 {
		t.Errorf("total: got %d, want 9", res.Total)
	}
	if res.RawBand != BandHold {
		t.Errorf("raw band: got %s, want hold", res.RawBand)
	}
	if res.FinalBand != BandTrim {
		t.Errorf("final band: got %s, want trim (one-below-hold)", res.FinalBand)
	}
	if !res.PPGCapApplied {
		t.Error("PPG cap should be applied")
	}
	// Multi-pillar zero produces a SINGLE band drop, not 3 separate drops.
	if res.FinalBand == BandExit {
		t.Error("multi-zero must NOT cascade to Exit — PPG cap is single-band-below per Spec 9l §15")
	}
}

func TestComputeRawAndFinalBand_AAVE_Q8Zero_AccumulateCap(t *testing.T) {
	// AAVE: Q1=1, Q2=1, Q3=2, Q4=1, Q5=2, Q6=2, Q7=2, Q8=0, Q9=2 = 13
	// Q8=0 fails no-zero gate → cap one below.
	// Raw 13 = Strong (alt_18 13+); one below = Accumulate.
	scores := AllPillarScores{
		"Q1": 1, "Q2": 1, "Q3": 2, "Q4": 1, "Q5": 2,
		"Q6": 2, "Q7": 2, "Q8": 0, "Q9": 2,
	}
	res := ComputeRawAndFinalBand(scores, ScorecardAlt18)
	if res.Total != 13 {
		t.Errorf("total: got %d, want 13", res.Total)
	}
	if res.RawBand != BandStrong {
		t.Errorf("raw band: got %s, want strong", res.RawBand)
	}
	if res.FinalBand != BandAccumulate {
		t.Errorf("final band: got %s, want accumulate (cap)", res.FinalBand)
	}
	if !res.PPGCapApplied {
		t.Error("PPG cap should be applied (Q8=0)")
	}
}

func TestComputeRawAndFinalBand_LINK_CleanStrong(t *testing.T) {
	// LINK: Q1=2, Q2=2, Q3=2, Q4=2, Q5=2, Q6=2, Q7=2, Q8=0, Q9=2 — wait,
	// actual LINK Q8 ≠ 0. Let's use the locked re-scored values.
	// LINK v1 (post-v0.5 re-lock): pillar sums to 16. No zero pillars.
	// PPG passes; band = Strong; no cap.
	scores := AllPillarScores{
		"Q1": 2, "Q2": 1, "Q3": 2, "Q4": 1, "Q5": 2,
		"Q6": 2, "Q7": 2, "Q8": 2, "Q9": 2,
	}
	res := ComputeRawAndFinalBand(scores, ScorecardAlt18)
	if res.Total != 16 {
		t.Errorf("total: got %d, want 16", res.Total)
	}
	if res.RawBand != BandStrong {
		t.Errorf("raw band: got %s, want strong", res.RawBand)
	}
	if res.FinalBand != BandStrong {
		t.Errorf("final band: got %s, want strong (no cap)", res.FinalBand)
	}
	if res.PPGCapApplied {
		t.Error("no PPG cap should apply")
	}
}

func TestComputeRawAndFinalBand_BUIDL_CleanStrong18_Ish(t *testing.T) {
	// BUIDL: 17/18 clean Strong with no zero pillars.
	scores := AllPillarScores{
		"Q1": 2, "Q2": 2, "Q3": 2, "Q4": 2, "Q5": 2,
		"Q6": 2, "Q7": 2, "Q8": 1, "Q9": 2,
	}
	res := ComputeRawAndFinalBand(scores, ScorecardAlt18)
	if res.Total != 17 {
		t.Errorf("total: got %d, want 17", res.Total)
	}
	if res.RawBand != BandStrong {
		t.Errorf("raw band: got %s, want strong", res.RawBand)
	}
	if res.FinalBand != BandStrong {
		t.Errorf("final band: got %s, want strong", res.FinalBand)
	}
	if res.PPGCapApplied {
		t.Error("no PPG cap should apply")
	}
}

func TestComputeRawAndFinalBand_LUNC_ExitFloor(t *testing.T) {
	// LUNC: 3/18 already at Exit. Multi-zero cap can't drop below Exit.
	scores := AllPillarScores{
		"Q1": 0, "Q2": 1, "Q3": 0, "Q4": 0, "Q5": 1,
		"Q6": 0, "Q7": 0, "Q8": 1, "Q9": 0,
	}
	res := ComputeRawAndFinalBand(scores, ScorecardAlt18)
	if res.Total != 3 {
		t.Errorf("total: got %d, want 3", res.Total)
	}
	if res.RawBand != BandExit {
		t.Errorf("raw band: got %s, want exit", res.RawBand)
	}
	if res.FinalBand != BandExit {
		t.Errorf("final band: got %s, want exit (no further drop)", res.FinalBand)
	}
}

func TestComputeAllPillars_OrderIndependent(t *testing.T) {
	subs := map[string]SubCriteria{
		"Q1": {1, 2, 2, 1},
		"Q2": {0, 0, 1, 0, 1},
		"Q3": {2, 2, 1, 2, 2},
	}
	got := ComputeAllPillars(subs)
	want := AllPillarScores{"Q1": 1, "Q2": 0, "Q3": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestOneBandBelow(t *testing.T) {
	cases := []struct {
		in, want Band
	}{
		{BandStrong, BandAccumulate},
		{BandAccumulate, BandHold},
		{BandHold, BandTrim},
		{BandTrim, BandExit},
		{BandExit, BandExit},
	}
	for _, c := range cases {
		if got := oneBandBelow(c.in); got != c.want {
			t.Errorf("oneBandBelow(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}
