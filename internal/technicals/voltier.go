package technicals

// VolTier classifies a holding's weekly volatility into one of four
// buckets used to scale the stop-loss buffer.
//
// Thresholds match Percoco's published guidance (cheat sheet §
// "Vol-adjusted stop loss"):
//
//	ATR/price < 2%   → Low      (N = 1.5)
//	         2-4%   → Medium   (N = 2.0)
//	         4-7%   → High     (N = 2.5)
//	         > 7%   → Extreme  (N = 3.0)
//
// Returns "" if inputs are invalid (caller falls back to user-set
// vol_tier or 'medium' default).
func VolTier(atrWeekly, currentPrice float64) string {
	if atrWeekly <= 0 || currentPrice <= 0 {
		return ""
	}
	pct := (atrWeekly / currentPrice) * 100
	switch {
	case pct < 2.0:
		return "low"
	case pct < 4.0:
		return "medium"
	case pct < 7.0:
		return "high"
	default:
		return "extreme"
	}
}

// VolTierMultiplier returns the N value for the SL formula:
//
//	SL = support − (N × ATR(14, weekly))
//
// Unknown tier defaults to medium (2.0) per Percoco's conservative
// recommendation when classification fails.
func VolTierMultiplier(tier string) float64 {
	switch tier {
	case "low":
		return 1.5
	case "medium":
		return 2.0
	case "high":
		return 2.5
	case "extreme":
		return 3.0
	}
	return 2.0
}
