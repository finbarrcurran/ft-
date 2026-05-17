package performance

import "fmt"

// AssignCohorts returns every cohort key this trade belongs to. One trade
// can be in many cohorts simultaneously (overlapping). The frontend's
// breakdown grid renders rows per cohort key; this function decides
// membership.
//
// Cohort schema (Spec 9d D5):
//
//	"all"
//	"kind:stock" | "kind:crypto"
//	"setup:A_breakout_retest" | "setup:B_support_bounce" | "setup:C_continuation"
//	"jordi:13-16" | "jordi:9-12" | "jordi:le-8"
//	"cowen:13-16" | "cowen:9-12" | "cowen:le-8"
//	"percoco:13-16" | "percoco:9-12" | "percoco:le-8"
//	"regime:stable" | "regime:shifting" | "regime:defensive" | "regime:unclassified"
//	"hold:short" (<7 days) | "hold:medium" (7..60) | "hold:long" (>60)
//	"exit:tp1_hit" | "exit:tp2_hit" | "exit:sl_hit" | "exit:bounce_close" | ...
//
// Bucket boundaries are constants here so they can be adjusted in one
// place after enough live data shows whether finer/coarser splits reveal
// more signal (spec 9d "Open items").
func AssignCohorts(t ClosedTrade) []string {
	cohorts := []string{"all", "kind:" + t.Kind}

	if t.SetupType != "" {
		cohorts = append(cohorts, "setup:"+t.SetupType)
	}
	if t.RegimeEffective != "" {
		cohorts = append(cohorts, "regime:"+t.RegimeEffective)
	}
	if t.ExitReason != "" {
		cohorts = append(cohorts, "exit:"+t.ExitReason)
	}

	if c := scoreBucket(t.JordiScore); c != "" {
		cohorts = append(cohorts, "jordi:"+c)
	}
	if c := scoreBucket(t.CowenScore); c != "" {
		cohorts = append(cohorts, "cowen:"+c)
	}
	if c := scoreBucket(t.PercocoScore); c != "" {
		cohorts = append(cohorts, "percoco:"+c)
	}

	cohorts = append(cohorts, "hold:"+holdingBucket(t.HoldingPeriodDays))

	return cohorts
}

// scoreBucket returns the cohort suffix for a framework score (0..16).
// "13-16" = A-grade (passes 13/16 threshold).
// "9-12"  = B-grade (close to threshold).
// "le-8"  = below-threshold trades that were taken anyway.
// Returns "" when score is nil (trade pre-dates Spec 4 scoring).
func scoreBucket(s *int) string {
	if s == nil {
		return ""
	}
	switch {
	case *s >= 13:
		return "13-16"
	case *s >= 9:
		return "9-12"
	default:
		return "le-8"
	}
}

// holdingBucket categorises by trade duration.
//
//	<= 7 days   short (scalp / news driven)
//	8..60 days  medium (standard swing)
//	> 60 days   long (thesis / multi-leg)
func holdingBucket(days int) string {
	switch {
	case days <= 7:
		return "short"
	case days <= 60:
		return "medium"
	default:
		return "long"
	}
}

// GroupByCohort returns a map of cohort_key → []ClosedTrade. Used by the
// snapshot generator (one ComputeMetrics call per cohort).
func GroupByCohort(trades []ClosedTrade) map[string][]ClosedTrade {
	out := map[string][]ClosedTrade{}
	for _, t := range trades {
		for _, c := range AssignCohorts(t) {
			out[c] = append(out[c], t)
		}
	}
	return out
}

// CohortDisplayLabel converts the machine key to a human label for the UI.
func CohortDisplayLabel(key string) string {
	switch key {
	case "all":
		return "All trades"
	case "kind:stock":
		return "Stocks"
	case "kind:crypto":
		return "Crypto"
	case "setup:A_breakout_retest":
		return "Setup A — breakout-retest"
	case "setup:B_support_bounce":
		return "Setup B — support bounce"
	case "setup:C_continuation":
		return "Setup C — continuation"
	case "regime:stable":
		return "Regime — STABLE"
	case "regime:shifting":
		return "Regime — SHIFTING"
	case "regime:defensive":
		return "Regime — DEFENSIVE"
	case "regime:unclassified":
		return "Regime — UNCLASSIFIED"
	case "hold:short":
		return "Held ≤7 days"
	case "hold:medium":
		return "Held 8–60 days"
	case "hold:long":
		return "Held >60 days"
	case "jordi:13-16":
		return "Jordi 13–16 (A)"
	case "jordi:9-12":
		return "Jordi 9–12 (B)"
	case "jordi:le-8":
		return "Jordi ≤8"
	case "cowen:13-16":
		return "Cowen 13–16 (A)"
	case "cowen:9-12":
		return "Cowen 9–12 (B)"
	case "cowen:le-8":
		return "Cowen ≤8"
	case "percoco:13-16":
		return "Percoco 13–16 (A)"
	case "percoco:9-12":
		return "Percoco 9–12 (B)"
	case "percoco:le-8":
		return "Percoco ≤8"
	}
	// Exit reason cohorts share a prefix.
	if len(key) > 5 && key[:5] == "exit:" {
		return "Exit — " + key[5:]
	}
	return key
}

// Categorise is the public form of cohort-key construction for handlers
// that need to display the score-bucket label for a given numeric score.
// Returns "" when score is nil.
func Categorise(score *int) string {
	return scoreBucket(score)
}

// MonotonicCheck reports whether expectancy increases monotonically with
// score bucket within one framework. Used by the methodology-calibration
// panel (Spec 9d D6): if 13-16 expectancy > 9-12 > ≤8, the framework is
// well-calibrated. Otherwise the panel surfaces a warning.
//
// Pass the three buckets' Expectancy values (≤8, 9-12, 13-16 in that
// order). Returns (true, "") when monotonic; (false, reason) otherwise.
func MonotonicCheck(le8, mid, top float64) (bool, string) {
	if le8 > mid {
		return false, fmt.Sprintf("≤8 cohort (%.2fR) outperforms 9-12 (%.2fR)", le8, mid)
	}
	if mid > top {
		return false, fmt.Sprintf("9-12 cohort (%.2fR) outperforms 13-16 (%.2fR)", mid, top)
	}
	return true, ""
}
