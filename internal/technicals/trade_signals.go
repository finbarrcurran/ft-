package technicals

// SC-35 W3 / Phase 2.4 — trade-management signals computed from the weekly
// series. TP1/TP2-hit and the resistance-rejection BOUNCE already live in the
// alert layer (stage_events.go, keyed on the stored stage + resistance levels);
// the missing piece is the *runner* exit: a long-term hold or a post-TP2 trade
// is trailed under successive higher weekly swing lows, and only a BREAK of the
// most recent such swing low is actionable ("weekly uptrend structure broke").
//
// Pure functions; the caller supplies already-aggregated weekly bars.

// LastWeeklySwingLow returns the price of the most recent confirmed weekly
// swing low — a bar whose low is strictly below both its immediate neighbours —
// excluding the final (still-forming / most-recent) bar, which has no right
// neighbour to confirm it. ok=false when there aren't enough bars to confirm
// one. A swing low is the floor the uptrend has been respecting; price closing
// below it is the structural break.
func LastWeeklySwingLow(weekly []Bar) (level float64, ok bool) {
	// Need at least three bars to have one interior bar with both neighbours.
	if len(weekly) < 3 {
		return 0, false
	}
	// Walk backwards from the last *confirmable* interior bar (len-2) so we
	// return the most recent swing low.
	for i := len(weekly) - 2; i >= 1; i-- {
		if weekly[i].Low < weekly[i-1].Low && weekly[i].Low < weekly[i+1].Low {
			return weekly[i].Low, true
		}
	}
	return 0, false
}

// WeeklyStructureBroken reports whether the latest weekly close has broken below
// the most recent confirmed weekly swing low — the runner-exit trigger
// (Phase 2.4). Returns the broken level for the message. ok=false when there is
// no confirmed swing low yet (too little history) or the structure is intact.
func WeeklyStructureBroken(weekly []Bar) (level float64, broken bool) {
	sl, ok := LastWeeklySwingLow(weekly)
	if !ok || len(weekly) == 0 {
		return 0, false
	}
	last := weekly[len(weekly)-1]
	if last.Close < sl {
		return sl, true
	}
	return sl, false
}
