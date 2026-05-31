// SC-08 (candidate Spec 9c.2) — two-tier "Proximity Alert" family.
//
// Replaces the old flat-margin SL/TP proximity (removed from alert.go) with a
// progress-based model that is symmetric across the stop and the target:
//
//	progressToSL = (entry − price)/(entry − SL)   // 0 at entry, 1 at the stop
//	progressToTP = (price − entry)/(TP − entry)   // 0 at entry, 1 at the target
//
//	AMBER  fires at progress ≥ 0.75 (tightened to 0.85 when the effective
//	       regime is Shifting or Defensive).
//	RED    fires when price ≤ SL × 1.02 (within 2% of the stop) or
//	       price ≥ TP × 0.98 (within 2% of the target).
//	RED supersedes AMBER on the same side — only the red event is emitted.
//
// Long-only (handover Snag S6). The four kinds get their own notification_log
// alert_kind strings so they dedup independently of the RED/AMBER classifier.
package alert

import (
	"fmt"

	"ft/internal/domain"
	"ft/internal/metrics"
)

// Proximity alert kinds — distinct notification_log dedup keys.
const (
	KindProximityAmberSL = "proximity_amber_sl"
	KindProximityAmberTP = "proximity_amber_tp"
	KindProximityRedSL   = "proximity_red_sl"
	KindProximityRedTP   = "proximity_red_tp"
)

// ProximityEvent is one fired proximity alert. Side is "sl" or "tp"; Tone is
// "amber" or "red". Trigger is the human-readable body fragment.
type ProximityEvent struct {
	Kind    string
	Tone    string
	Side    string
	Trigger string
}

// ProximityEvents evaluates the two-tier proximity family for one holding.
// `tightened` is true when the effective regime is Shifting/Defensive, raising
// the amber threshold from 0.75 to 0.85. Pre-computed metrics are passed in so
// the effective stop is resolved exactly once per holding.
func ProximityEvents(h *domain.StockHolding, m metrics.StockMetrics, tightened bool) []ProximityEvent {
	if h.CurrentPrice == nil || *h.CurrentPrice <= 0 {
		return nil
	}
	px := *h.CurrentPrice
	ticker := ""
	if h.Ticker != nil {
		ticker = *h.Ticker
	}

	amberAt := 0.75
	if tightened {
		amberAt = 0.85
	}

	var out []ProximityEvent

	// ---- Stop-loss side ----
	if m.EffectiveSLPrice != nil && *m.EffectiveSLPrice > 0 {
		sl := *m.EffectiveSLPrice
		switch {
		case px <= sl*1.02: // RED supersedes amber
			out = append(out, ProximityEvent{
				Kind: KindProximityRedSL, Tone: "red", Side: "sl",
				Trigger: fmt.Sprintf("🔴 Proximity Alert — %s within 2%% of stop loss (price %.2f, SL %.2f)", labelOr(ticker, h.Name), px, sl),
			})
		case m.ProgressToSL != nil && *m.ProgressToSL >= amberAt:
			out = append(out, ProximityEvent{
				Kind: KindProximityAmberSL, Tone: "amber", Side: "sl",
				Trigger: fmt.Sprintf("🟡 Proximity Alert — %s %.0f%% of the way to its stop (price %.2f, SL %.2f)", labelOr(ticker, h.Name), *m.ProgressToSL*100, px, sl),
			})
		}
	}

	// ---- Take-profit side ----
	if h.TakeProfit != nil && *h.TakeProfit > 0 {
		tp := *h.TakeProfit
		switch {
		case px >= tp*0.98: // RED supersedes amber
			out = append(out, ProximityEvent{
				Kind: KindProximityRedTP, Tone: "red", Side: "tp",
				Trigger: fmt.Sprintf("🔴 Proximity Alert — %s within 2%% of take profit (price %.2f, TP %.2f)", labelOr(ticker, h.Name), px, tp),
			})
		case m.ProgressToTP != nil && *m.ProgressToTP >= amberAt:
			out = append(out, ProximityEvent{
				Kind: KindProximityAmberTP, Tone: "amber", Side: "tp",
				Trigger: fmt.Sprintf("🟡 Proximity Alert — %s %.0f%% of the way to its target (price %.2f, TP %.2f)", labelOr(ticker, h.Name), *m.ProgressToTP*100, px, tp),
			})
		}
	}

	return out
}

func labelOr(ticker, name string) string {
	if ticker != "" {
		return ticker
	}
	return name
}
