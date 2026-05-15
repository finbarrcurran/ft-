package domain

// Spec 3 D2 — Stop-loss / take-profit suggestion rules.
//
// These are pure functions. Calling sites:
//   * `Add Stock` / `Add Crypto` modals — pre-fill the SL/TP inputs once the
//     ticker (and, for stocks, the beta) resolves.
//   * Edit modal — same pre-fill behaviour when toggling Vol Tier.
//   * Read-only "Suggested SL / Suggested TP" columns on the holdings tables
//     (Spec 3 D11).
//
// Tuneable: expect the beta thresholds and the crypto tier returns to drift
// after ~3 months of real usage. Single file, simple constants.

// SuggestStockRisk returns (stopLossPct, takeProfitPct) where stopLossPct is
// negative (a drawdown limit) and takeProfitPct is positive (a target).
// `beta` may be nil — falls back to the medium-vol default.
func SuggestStockRisk(beta *float64) (slPct, tpPct float64) {
	if beta == nil {
		return -15.0, 30.0 // medium-vol default
	}
	switch {
	case *beta < 0.8:
		return -10.0, 20.0
	case *beta < 1.2:
		return -15.0, 30.0
	case *beta < 1.8:
		return -25.0, 50.0
	default:
		return -35.0, 70.0
	}
}

// SuggestCryptoRisk returns (stopLossPct, takeProfitPct) by manual vol tier.
// Unknown tiers fall back to medium-vol defaults.
func SuggestCryptoRisk(tier string) (slPct, tpPct float64) {
	switch tier {
	case "low":
		return -25.0, 60.0
	case "medium":
		return -35.0, 100.0
	case "high":
		return -50.0, 200.0
	case "extreme":
		return -65.0, 300.0
	default:
		return -35.0, 100.0
	}
}

// CryptoVolTiers is the canonical ordered list for UI dropdowns and validation.
var CryptoVolTiers = []string{"low", "medium", "high", "extreme"}

// IsValidVolTier returns true if `s` is one of the recognised tiers.
func IsValidVolTier(s string) bool {
	for _, t := range CryptoVolTiers {
		if s == t {
			return true
		}
	}
	return false
}
