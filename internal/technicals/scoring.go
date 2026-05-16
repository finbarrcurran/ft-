package technicals

// Auto-scoring of 6 of the 8 Percoco technical-screen questions. Spec
// 9c D9. Questions 7 (chart cleanliness) and 8 (catalyst proximity)
// require manual user input — caller fills those in afterwards.
//
// Each question scores 0 / 1 / 2 with rationale text rendered in the
// UI tooltip ("Why 2? Price $1100 > SMA50W $1050 > SMA200D $980,
// both rising"). Question 4 (risk-reward) is a VETO — its score of 0
// makes the whole trade rejected regardless of other scores. The
// loader honors the veto separately; here we just compute scores.

// AutoScoreInputs is the inputs vector for AutoScorePercoco. Caller
// assembles from holdings + price_history + S/R + ATR data.
type AutoScoreInputs struct {
	CurrentPrice float64

	// Moving averages + slopes ("up" / "flat" / "down").
	SMA50W       float64
	SMA200D      float64
	SMA50WTrend  string
	SMA200DTrend string

	// Monthly trend = sign of (current close vs 12mo prior close).
	MonthlyTrend string // "up" / "flat" / "down"

	// Levels (user-set or auto-proposed).
	Support1    float64
	Resistance1 float64
	Resistance2 float64

	// Volatility.
	ATRWeekly         float64
	ATRPctAvg12mo     float64 // average ATR/price over last 12 months
	ATRPctCurrent     float64 // ATR/price now
	DistanceToSupport float64 // entry's distance to support_1 in *ATRs*

	// Trade math.
	EntryPrice float64
	StopPrice  float64
	TP1        float64
	TP2        float64
}

// AutoScoreResult is the 6 question scores + a per-question rationale
// string for the UI tooltip.
type AutoScoreResult struct {
	Scores     map[string]int    // question_id → 0/1/2
	Rationales map[string]string // question_id → human-readable why
}

// AutoScorePercoco computes the six auto-scorable questions of the
// percoco.json framework. See `internal/frameworks/definitions/
// percoco.json` for the canonical question IDs.
func AutoScorePercoco(in AutoScoreInputs) AutoScoreResult {
	out := AutoScoreResult{
		Scores:     map[string]int{},
		Rationales: map[string]string{},
	}

	// Q1 — trend_alignment: price above 50W & 200D MA, both rising
	out.Scores["trend_alignment"], out.Rationales["trend_alignment"] = scoreTrendAlignment(in)

	// Q2 — higher_tf_context: monthly trend agrees with weekly setup
	out.Scores["higher_tf_context"], out.Rationales["higher_tf_context"] = scoreHigherTF(in)

	// Q3 — clear_sr: all three levels (S1, R1, R2) set
	out.Scores["clear_sr"], out.Rationales["clear_sr"] = scoreClearSR(in)

	// Q4 — risk_reward (VETO): R-multiple ≥ 1.5 to TP1 AND ≥ 3 to TP2
	out.Scores["risk_reward"], out.Rationales["risk_reward"] = scoreRiskReward(in)

	// Q5 — vol_band: current ATR/price near 12mo average
	out.Scores["vol_band"], out.Rationales["vol_band"] = scoreVolBand(in)

	// Q6 — distance_to_sr: entry within 1 ATR of meaningful support
	out.Scores["distance_to_sr"], out.Rationales["distance_to_sr"] = scoreDistanceToSR(in)

	return out
}

// ----- per-question helpers --------------------------------------------

func scoreTrendAlignment(in AutoScoreInputs) (int, string) {
	if in.CurrentPrice == 0 || in.SMA50W == 0 || in.SMA200D == 0 {
		return 0, "MA data unavailable"
	}
	if in.CurrentPrice <= in.SMA50W || in.CurrentPrice <= in.SMA200D {
		return 0, "price below 50W or 200D MA"
	}
	switch {
	case in.SMA50WTrend == "down" || in.SMA200DTrend == "down":
		return 0, "price > MAs but at least one MA trending down"
	case in.SMA50WTrend == "up" && in.SMA200DTrend == "up":
		return 2, "price above both MAs, both rising"
	default:
		// One up, one flat
		return 1, "price above both MAs, one flat"
	}
}

func scoreHigherTF(in AutoScoreInputs) (int, string) {
	wantUp := in.SMA50WTrend == "up"
	switch in.MonthlyTrend {
	case "up":
		if wantUp {
			return 2, "monthly trend matches weekly (both up)"
		}
		return 0, "monthly up but weekly setup down"
	case "down":
		if !wantUp {
			return 2, "monthly trend matches weekly (both down)"
		}
		return 0, "monthly down vs weekly up — counter-trend"
	default:
		return 1, "monthly flat"
	}
}

func scoreClearSR(in AutoScoreInputs) (int, string) {
	switch {
	case in.Support1 > 0 && in.Resistance1 > 0 && in.Resistance2 > 0:
		return 2, "S1, R1, R2 all set"
	case in.Support1 > 0 && in.Resistance1 > 0:
		return 1, "S1 + R1 set, R2 missing"
	default:
		return 0, "S1 or R1 missing"
	}
}

func scoreRiskReward(in AutoScoreInputs) (int, string) {
	rTP1 := RMultiple(in.EntryPrice, in.StopPrice, in.TP1)
	rTP2 := RMultiple(in.EntryPrice, in.StopPrice, in.TP2)
	switch {
	case rTP1 < 1.5:
		return 0, "VETO — R to TP1 below 1.5"
	case rTP2 >= 3.0:
		return 2, "R to TP1 ≥1.5, R to TP2 ≥3.0"
	default:
		return 1, "R to TP1 ≥1.5 but R to TP2 below 3.0"
	}
}

func scoreVolBand(in AutoScoreInputs) (int, string) {
	if in.ATRPctAvg12mo <= 0 || in.ATRPctCurrent <= 0 {
		return 0, "vol history unavailable"
	}
	ratio := in.ATRPctCurrent / in.ATRPctAvg12mo
	switch {
	case ratio >= 0.7 && ratio <= 1.3:
		return 2, "ATR/price near 12mo average"
	case ratio >= 0.5 && ratio <= 1.7:
		return 1, "ATR/price modestly off 12mo average"
	default:
		return 0, "ATR/price at extreme (overheated or compressed)"
	}
}

func scoreDistanceToSR(in AutoScoreInputs) (int, string) {
	if in.DistanceToSupport <= 0.5 {
		return 2, "entry within 0.5 ATR of support"
	}
	if in.DistanceToSupport <= 1.5 {
		return 1, "entry within 1.5 ATR of support"
	}
	return 0, "entry > 1.5 ATR from support"
}
