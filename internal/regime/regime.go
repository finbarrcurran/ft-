// Package regime owns the Spec 9b classification math + the constants
// alert.go consults to gate alerts.
//
// Two frameworks (Jordi for stocks, Cowen for crypto) each carry one of
// {stable, shifting, defensive, unclassified}. The "effective" regime is the
// more defensive of the two, with a special rule for unclassified:
//
//	stable < shifting < defensive
//	unclassified counts as shifting for alert gating, but the *classified*
//	side wins when one is unclassified and the other isn't.
//
// alert_margin_multiplier is what callers read to scale the proximity-alert
// margin (Spec 3 D12 default 5%). Cached on regime change is fine; we
// recompute on every alert eval since it's a handful of int ops.
package regime

// Regime is the four-state enum.
type Regime string

const (
	Stable       Regime = "stable"
	Shifting     Regime = "shifting"
	Defensive    Regime = "defensive"
	Unclassified Regime = "unclassified"
)

// Valid reports whether s is one of the four enum values.
func Valid(s string) bool {
	switch Regime(s) {
	case Stable, Shifting, Defensive, Unclassified:
		return true
	}
	return false
}

// severity gives an ordering for "more defensive of the two".
// unclassified is treated as shifting per spec.
func severity(r Regime) int {
	switch r {
	case Stable:
		return 1
	case Shifting:
		return 2
	case Unclassified:
		return 2 // treated as shifting for alert gating
	case Defensive:
		return 3
	}
	return 2
}

// Effective returns the consolidated regime per the spec rules:
//   - more defensive of the two wins
//   - when one side is unclassified and the other isn't, the classified one
//     wins (don't penalize unset state too hard)
func Effective(jordi, cowen Regime) Regime {
	switch {
	case jordi == Unclassified && cowen == Unclassified:
		return Unclassified
	case jordi == Unclassified:
		return cowen
	case cowen == Unclassified:
		return jordi
	}
	if severity(cowen) > severity(jordi) {
		return cowen
	}
	return jordi
}

// AlertMarginMultiplier scales the proximity-alert margin (Spec 9b D6).
// Read by alert.go at eval time.
func AlertMarginMultiplier(eff Regime) float64 {
	switch eff {
	case Stable:
		return 1.0 // 5% margin unchanged
	case Shifting, Defensive:
		return 0.6 // 3% effective
	case Unclassified:
		return 0.8 // 4% — middle ground
	}
	return 1.0
}

// GatesWatchlistEntryZone reports whether the regime permits firing
// entry-zone alerts on watchlist entries. Only STABLE permits firing per
// spec D6.
func GatesWatchlistEntryZone(eff Regime) bool {
	return eff == Stable
}

// ----- Cowen weekly-form classification ----------------------------------

// CowenFormInputs is the 8-field weekly capture (plus macro flags + note).
// JSON tags match the API contract / POST body shape.
type CowenFormInputs struct {
	BTCVs200wkMAPct  float64 `json:"btc_vs_200wk_ma_pct"`
	BTCVs200dMAPct   float64 `json:"btc_vs_200d_ma_pct"`
	LogBandThird     string  `json:"log_band_third"`     // "lower" | "middle" | "upper"
	RiskIndicator    float64 `json:"risk_indicator"`     // 0.0 to 1.0
	BTCDominancePct  float64 `json:"btc_dominance_pct"`
	BTCDominance4wk  string  `json:"btc_dominance_4wk"`  // "rising" | "flat" | "falling"
	ETHBTC           float64 `json:"eth_btc"`
	ETHBTC4wk        string  `json:"eth_btc_4wk"`        // "rising" | "flat" | "falling"
	MVRVZBand        string  `json:"mvrv_z_band"`        // "undervalued" | "neutral" | "overvalued" | "extreme_overvalued"
	CyclePhase       int     `json:"cycle_phase"`        // 1..4
	CPITrendingDown  bool    `json:"cpi_trending_down"`
	FedNotHostile    bool    `json:"fed_not_hostile"`
	RecessionRiskLow bool    `json:"recession_risk_low"`
	Note             string  `json:"note,omitempty"`
}

// MacroSupportive returns the AND of the three macro flags. Matches the
// classifier's `macro_supportive` term.
func (in CowenFormInputs) MacroSupportive() bool {
	return in.CPITrendingDown && in.FedNotHostile && in.RecessionRiskLow
}

// ClassifyCowen runs the spec's exact decision tree:
//
//	IF (cycle_phase == 4) OR (risk_indicator > 0.75)
//	   OR (NOT macro_supportive AND risk_indicator > 0.5)
//	  → DEFENSIVE
//	ELSE IF (cycle_phase == 3 AND risk_indicator BETWEEN 0.5 AND 0.75)
//	     OR (cycle_phase == 2 AND prior was phase 3)
//	  → SHIFTING
//	ELSE IF (cycle_phase IN {1, 2}) AND (risk_indicator < 0.5)
//	  → STABLE
//	ELSE
//	  → UNCLASSIFIED
//
// `priorCyclePhase` is the cycle_phase from the most recent prior submission;
// pass 0 if none.
func ClassifyCowen(in CowenFormInputs, priorCyclePhase int) (Regime, string) {
	macroOK := in.MacroSupportive()
	r := in.RiskIndicator
	cp := in.CyclePhase

	switch {
	case cp == 4 || r > 0.75 || (!macroOK && r > 0.5):
		reason := "cycle phase 4"
		if r > 0.75 {
			reason = "risk indicator > 0.75"
		} else if !macroOK && r > 0.5 {
			reason = "risk > 0.5 with non-supportive macro"
		}
		return Defensive, reason
	case (cp == 3 && r >= 0.5 && r <= 0.75) || (cp == 2 && priorCyclePhase == 3):
		reason := "cycle phase 3, risk 0.5–0.75"
		if cp == 2 && priorCyclePhase == 3 {
			reason = "phase 2 after prior phase 3"
		}
		return Shifting, reason
	case (cp == 1 || cp == 2) && r < 0.5:
		return Stable, "early cycle + low risk"
	default:
		return Unclassified, "no rule matched"
	}
}
