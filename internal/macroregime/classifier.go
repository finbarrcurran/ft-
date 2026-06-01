package macroregime

import "time"

// flatBand is the |momentum| dead-zone treated as "flat" for direction
// labels. Quadrant axes are still resolved binary (ties → the inflationary /
// accelerating side is decided by sign, see Classify).
const flatBand = 0.10

// signalDir converts one indicator into an improving(+1)/deteriorating(-1)/
// flat(0) vote for ITS axis, honouring the Invert flag (UNRATE/ICSA: a
// rising value is growth-negative). Inflation series vote on whether
// inflation is ACCELERATING (RoC of YoY > 0 → +1).
func signalDir(d SeriesDef, roc *float64) int {
	if roc == nil {
		return 0
	}
	v := *roc
	if d.Invert {
		v = -v
	}
	// Per-group flat bands (RoC units differ across series).
	var band float64
	switch d.Group {
	case "regional_fed":
		band = 0.5 // diffusion-index points
	case "cpi":
		band = 0.05 // pct-points of YoY change
	case "output":
		band = 0.1
	case "employment":
		band = 0.05
	default:
		band = 0.0
	}
	if v > band {
		return 1
	}
	if v < -band {
		return -1
	}
	return 0
}

// Classify runs the deterministic 9p classifier over the latest indicator
// readings. ism is the manual override (used as a growth-level anchor when
// fresh; nowcast fallback otherwise — §C). It returns a fully-populated
// RegimeState; missing data degrades confidence rather than failing.
func Classify(indicators map[string]Indicator, ism ISMStatus) RegimeState {
	now := time.Now().Unix()

	// ---- Growth axis: average the directional votes of growth signals ----
	growthVotes := 0
	growthN := 0
	for _, d := range Series {
		if d.Axis != AxisGrowth {
			continue
		}
		ind, ok := indicators[d.ID]
		if !ok || ind.RoC == nil {
			continue
		}
		v := signalDir(d, ind.RoC)
		growthVotes += v
		growthN++
	}
	// Fresh manual ISM tilts the growth read: >50 expansionary, <50 contractionary.
	if ism.Fresh && ism.Value != nil {
		if *ism.Value >= 50 {
			growthVotes++
		} else {
			growthVotes--
		}
		growthN++
	}
	var growthMom float64
	if growthN > 0 {
		growthMom = float64(growthVotes) / float64(growthN)
	}

	// ---- Inflation axis ----
	inflVotes := 0
	inflN := 0
	for _, d := range Series {
		if d.Axis != AxisInflation {
			continue
		}
		ind, ok := indicators[d.ID]
		if !ok || ind.RoC == nil {
			continue
		}
		inflVotes += signalDir(d, ind.RoC)
		inflN++
	}
	var inflMom float64
	if inflN > 0 {
		inflMom = float64(inflVotes) / float64(inflN)
	}

	st := RegimeState{
		GrowthMomentum:    round2(growthMom),
		InflationMomentum: round2(inflMom),
		ComputedAt:        now,
	}

	// Insufficient data on either axis → unclassified.
	if growthN == 0 || inflN == 0 {
		st.Quadrant = "unclassified"
		st.Shorthand = shorthandFor("unclassified")
		st.GrowthDir = "unknown"
		st.InflationDir = "unknown"
		st.Confidence = "low"
		st.ThematicFlags = augmentAndThematics(&st, indicators)
		st.SuggestedJordi = "unclassified"
		return st
	}

	// Binary axis resolution (ties → accel for growth, accel for inflation).
	growthAccel := growthMom >= 0
	inflAccel := inflMom >= 0
	st.GrowthDir = boolDir(growthAccel, "accel", "decel")
	st.InflationDir = boolDir(inflAccel, "accel", "decel")

	switch {
	case growthAccel && !inflAccel:
		st.Quadrant = "Q1" // Goldilocks
	case growthAccel && inflAccel:
		st.Quadrant = "Q2" // Reflation
	case !growthAccel && inflAccel:
		st.Quadrant = "Q3" // Stagflation
	default:
		st.Quadrant = "Q4" // Deflation/Recession
	}
	st.Shorthand = shorthandFor(st.Quadrant)

	// ---- Confidence: |momentum| magnitude + data coverage ----
	st.Confidence = confidenceOf(growthMom, inflMom, growthN, inflN)

	// ---- Augmenting state + thematic overlays ----
	st.ThematicFlags = augmentAndThematics(&st, indicators)

	// ---- Suggested Jordi regime (D8; suggest-only) ----
	st.SuggestedJordi = suggestJordi(st)

	return st
}

// augmentAndThematics fills the rates/liquidity/curve/credit/dollar fields on
// st from the augmenting indicators and returns the derived thematic flags.
func augmentAndThematics(st *RegimeState, indicators map[string]Indicator) []string {
	flags := []string{}

	if ind, ok := indicators["fedfunds"]; ok && ind.RoC != nil {
		switch {
		case *ind.RoC > 0.02:
			st.RatesRegime = "hiking"
		case *ind.RoC < -0.02:
			st.RatesRegime = "cutting"
		default:
			st.RatesRegime = "hold"
		}
	}
	if ind, ok := indicators["m2"]; ok && ind.RoC != nil {
		if *ind.RoC >= 0 {
			st.LiquidityRegime = "expansion"
			flags = append(flags, "liquidity_expansion")
		} else {
			st.LiquidityRegime = "contraction"
			flags = append(flags, "liquidity_contraction")
		}
	}
	if ind, ok := indicators["curve"]; ok && ind.Value != nil {
		if *ind.Value >= 0 {
			st.CurveRegime = "normal"
		} else {
			st.CurveRegime = "inverted"
			flags = append(flags, "curve_inverted")
		}
	}
	if ind, ok := indicators["credit"]; ok && ind.RoC != nil {
		if *ind.RoC > 0 {
			st.CreditRegime = "widening"
			flags = append(flags, "credit_stress")
		} else {
			st.CreditRegime = "tightening"
		}
	}
	if ind, ok := indicators["dxy"]; ok && ind.RoC != nil {
		if *ind.RoC > 0 {
			st.DollarRegime = "strengthening"
		} else {
			st.DollarRegime = "weakening"
		}
	}
	return flags
}

// suggestJordi maps the macro quadrant + augmenting state to a suggested
// 9b Jordi regime (suggest-only; never auto-applied — D8).
func suggestJordi(st RegimeState) string {
	base := "stable"
	switch st.Quadrant {
	case "Q1":
		base = "stable"
	case "Q2":
		base = "stable"
	case "Q3":
		base = "defensive"
	case "Q4":
		base = "defensive"
	default:
		return "unclassified"
	}
	// Stress modifiers tighten the suggestion one notch toward defensive.
	stress := false
	for _, f := range st.ThematicFlags {
		if f == "liquidity_contraction" || f == "credit_stress" || f == "curve_inverted" {
			stress = true
		}
	}
	if stress {
		switch base {
		case "stable":
			return "shifting"
		case "shifting":
			return "defensive"
		}
	}
	return base
}

// confidenceOf blends momentum magnitude with axis data coverage.
func confidenceOf(gMom, iMom float64, gN, iN int) string {
	mag := (absf(gMom) + absf(iMom)) / 2
	coverage := 1.0
	if gN < 3 || iN < 2 {
		coverage = 0.6
	}
	score := mag * coverage
	switch {
	case score >= 0.5 && gN >= 3 && iN >= 2:
		return "high"
	case score >= 0.25:
		return "medium"
	default:
		return "low"
	}
}

func boolDir(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5*sign(v))) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
