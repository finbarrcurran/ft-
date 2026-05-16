package technicals

// R-multiple math + position-size formula. All pure functions; zero IO.
// These are the most-tested helpers in the package — the spec's
// acceptance criteria #5-10 are direct plugs into these.

// SuggestedSL applies the vol-adjusted-stop formula:
//
//	SL = support − (N × ATR(14, weekly))
//
// Where N is the vol-tier multiplier (1.5 / 2.0 / 2.5 / 3.0).
// Returns 0 if any input is invalid.
func SuggestedSL(support, atrWeekly float64, volTier string) float64 {
	if support <= 0 || atrWeekly <= 0 {
		return 0
	}
	return support - (VolTierMultiplier(volTier) * atrWeekly)
}

// SuggestedTP1 sits just under resistance_1 by 0.25 × ATR — Percoco's
// "sell before the level, not at it" guidance to avoid getting picked
// off by sellers stacked at the round number / prior high.
//
// Returns 0 if any input is invalid.
func SuggestedTP1(resistance1, atrWeekly float64) float64 {
	if resistance1 <= 0 || atrWeekly <= 0 {
		return 0
	}
	return resistance1 - (0.25 * atrWeekly)
}

// SuggestedTP2 same shape as TP1 but for resistance_2. Returns 0 if
// resistance_2 isn't set — caller falls back to trailing-stop logic.
func SuggestedTP2(resistance2, atrWeekly float64) float64 {
	if resistance2 <= 0 || atrWeekly <= 0 {
		return 0
	}
	return resistance2 - (0.25 * atrWeekly)
}

// RMultiple = (target − entry) / (entry − stop). Negative when the
// trade is upside-down (e.g., entry ≤ stop). Caller is responsible for
// interpreting negative results as "this trade math is broken".
func RMultiple(entry, stop, target float64) float64 {
	risk := entry - stop
	if risk <= 0 {
		return 0
	}
	return (target - entry) / risk
}

// PositionSize returns (units, sizeUSD, riskUSD) for a trade:
//
//	riskUSD = portfolioValue × perTradeRiskPct / 100
//	units   = riskUSD / (entry − stop)
//	sizeUSD = units × entry
//
// Returns zeros if any input is invalid (entry ≤ stop, non-positive
// portfolio value, etc).
func PositionSize(portfolioValue, perTradeRiskPct, entry, stop float64) (units, sizeUSD, riskUSD float64) {
	if portfolioValue <= 0 || perTradeRiskPct <= 0 || entry <= 0 || stop <= 0 {
		return 0, 0, 0
	}
	diff := entry - stop
	if diff <= 0 {
		return 0, 0, 0
	}
	riskUSD = portfolioValue * (perTradeRiskPct / 100)
	units = riskUSD / diff
	sizeUSD = units * entry
	return
}
