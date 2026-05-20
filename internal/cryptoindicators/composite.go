package cryptoindicators

// Composite computation: weighted mean of bucket sub-scores → -100..+100
// composite, mapped to one of five action bands.
//
// Per Spec 9e §D4:
//
//   For each bucket b in {cowen, pal, universal, sentiment}:
//       sub_score_b = mean(score for active indicators in bucket b)
//   composite_raw = Σ (sub_score_b × weight_b) for buckets WITH active
//                   indicators (empty buckets are dropped and the
//                   remaining weights re-normalise to sum to 1).
//   composite_display = composite_raw × 100
//
// Action bands:
//
//   ≥  60  →  strong_accumulate
//   ≥  20  →  accumulate
//   >  -20 →  neutral
//   >  -60 →  caution
//   else   →  distribute_wait
//
// Band thresholds live here (not in JSON) — they're framework-level
// decisions, not per-indicator tunables. Re-tune after Phase 2 backtest
// if distribution skews.

// ActiveScore is one indicator's contribution to the composite. Caller
// (the snapshot/service layer) sets Bucket from the IndicatorDef and
// Score from the scoring engine. Stale or fetch-errored indicators are
// simply omitted from the slice — they don't appear here at all.
type ActiveScore struct {
	IndicatorID string
	Bucket      string
	Score       float64 // in [-1, +1]
}

// Weights is the per-bucket allocation. Default 25/20/35/20 per spec;
// stored in the crypto_indicator_weights table (1 row) so the user can
// retune via SQL in v1 (Settings UI deferred to Phase 2).
type Weights struct {
	Cowen     float64
	Pal       float64
	Universal float64
	Sentiment float64
}

// DefaultWeights matches the seed in migration 0024.
func DefaultWeights() Weights {
	return Weights{Cowen: 0.25, Pal: 0.20, Universal: 0.35, Sentiment: 0.20}
}

// CompositeResult bundles the final composite + bucket sub-scores +
// action band. All sub-scores are in [-1, +1]; composite is in [-100,
// +100]. Buckets with no active indicators have SubScores[b] = nil.
type CompositeResult struct {
	Composite  float64           // -100..+100
	SubScores  map[string]*float64 // bucket → mean score (nil if empty)
	ActionBand string            // strong_accumulate | accumulate | neutral | caution | distribute_wait
	// EffectiveWeights records the weights actually applied (after
	// redistributing empty buckets). For the gauge tooltip + the
	// snapshot row.
	EffectiveWeights map[string]float64
}

// ComputeComposite aggregates active indicator scores per bucket, then
// weights the bucket sub-scores into the -100..+100 composite. Buckets
// with zero active indicators are dropped and their weight is
// proportionally redistributed to the remaining buckets.
func ComputeComposite(active []ActiveScore, w Weights) CompositeResult {
	// 1. Sum & count scores per bucket.
	sums := map[string]float64{}
	counts := map[string]int{}
	for _, a := range active {
		sums[a.Bucket] += a.Score
		counts[a.Bucket]++
	}

	subScores := map[string]*float64{
		"cowen": nil, "pal": nil, "universal": nil, "sentiment": nil,
	}
	for b, c := range counts {
		if c == 0 {
			continue
		}
		mean := sums[b] / float64(c)
		subScores[b] = &mean
	}

	// 2. Build active-weight map from DefaultWeights, dropping empty buckets.
	rawWeights := map[string]float64{
		"cowen": w.Cowen, "pal": w.Pal,
		"universal": w.Universal, "sentiment": w.Sentiment,
	}
	totalActiveWeight := 0.0
	for b, ws := range rawWeights {
		if subScores[b] != nil {
			totalActiveWeight += ws
		}
	}

	// 3. Renormalise so active weights sum to 1. If no buckets are
	//    active at all, return zero composite + neutral band.
	effective := map[string]float64{}
	composite := 0.0
	if totalActiveWeight > 0 {
		for b, ws := range rawWeights {
			if subScores[b] == nil {
				effective[b] = 0
				continue
			}
			renorm := ws / totalActiveWeight
			effective[b] = renorm
			composite += *subScores[b] * renorm
		}
	}

	// 4. Scale to -100..+100 + map band.
	display := composite * 100.0
	return CompositeResult{
		Composite:        roundTo(display, 1),
		SubScores:        subScores,
		ActionBand:       BandFor(display),
		EffectiveWeights: effective,
	}
}

// BandFor maps a composite score (-100..+100) to one of five action bands.
func BandFor(composite float64) string {
	switch {
	case composite >= 60:
		return "strong_accumulate"
	case composite >= 20:
		return "accumulate"
	case composite > -20:
		return "neutral"
	case composite > -60:
		return "caution"
	default:
		return "distribute_wait"
	}
}

// roundTo rounds to `decimals` decimal places. Cheap helper; avoids
// importing math.
func roundTo(v float64, decimals int) float64 {
	mul := 1.0
	for i := 0; i < decimals; i++ {
		mul *= 10
	}
	if v >= 0 {
		return float64(int64(v*mul+0.5)) / mul
	}
	return float64(int64(v*mul-0.5)) / mul
}
