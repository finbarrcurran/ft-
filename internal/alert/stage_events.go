// Spec 9c D14 — new alert family for the Percoco execution layer.
// These are STAGE-driven (advance through pre_tp1 → post_tp1 → runner)
// rather than threshold-driven (RED/AMBER/GREEN on SL/RSI proximity).
//
// Each StageEvent gets its own `notification_log.alert_kind` string for
// 24h dedup. The bot exposes them alongside the existing Compute() output.

package alert

import (
	"fmt"
	"ft/internal/domain"
	"time"
)

// StageEvent is one Percoco-execution-layer signal on a holding.
type StageEvent struct {
	// Kind is the dedup key in notification_log.
	Kind string `json:"kind"`
	// Color is the rendering hint: "green" (TP hits, good news),
	// "amber" (proximity warnings, bounce rejection, time-stop reviews).
	Color string `json:"color"`
	// Trigger is a one-line human-readable message for the Telegram pill.
	Trigger string `json:"trigger"`
	// Stage records the holding's current stage at trigger time.
	Stage string `json:"stage"`
}

// StageEventKinds — exposed for callers that want to enumerate.
const (
	KindTP1Proximity = "tp1_proximity"
	KindTP1Hit       = "tp1_hit"
	KindTP2Proximity = "tp2_proximity"
	KindTP2Hit       = "tp2_hit"
	KindBounce       = "bounce"
	KindTimeStop     = "time_stop"
)

// ComputeStageEvents returns Percoco stage events for a holding, given the
// scaled margin (set by regime overlay per Spec 9b D6).
//
//	pre_tp1  → check AMBER_TP1_PROXIMITY + GREEN_TP1_HIT
//	post_tp1 → check AMBER_TP2_PROXIMITY + GREEN_TP2_HIT + AMBER_BOUNCE
//	runner   → nothing here (handled by trailing-stop logic)
//	stopped  → nothing here (position is closed)
//
// Additionally checks AMBER_TIME_STOP regardless of stage when
// time_stop_review_at is set and reached.
func ComputeStageEvents(h *domain.StockHolding, margin float64) []StageEvent {
	if h == nil || h.CurrentPrice == nil || *h.CurrentPrice <= 0 {
		return nil
	}
	price := *h.CurrentPrice
	var out []StageEvent

	// Stage-dependent triggers.
	switch h.Stage {
	case "pre_tp1":
		if h.TakeProfit != nil && *h.TakeProfit > 0 {
			tp := *h.TakeProfit // legacy field; spec 9c will populate from R1
			// Proximity: within margin of TP and trending UP today.
			lower := tp * (1 - margin)
			if price >= lower && price < tp {
				out = append(out, StageEvent{
					Kind: KindTP1Proximity, Color: "amber", Stage: h.Stage,
					Trigger: fmt.Sprintf("%s within %.1f%% of TP1 $%.2f — plan to sell ~33%%", tickerOrName(h), pct(price, tp), tp),
				})
			} else if price >= tp {
				out = append(out, StageEvent{
					Kind: KindTP1Hit, Color: "green", Stage: h.Stage,
					Trigger: fmt.Sprintf("%s hit TP1 $%.2f — sell ~33%% on eToro now, raise SL to BE. Reply /done to mark.", tickerOrName(h), tp),
				})
			}
		}
	case "post_tp1":
		if h.Resistance2 != nil && *h.Resistance2 > 0 {
			tp2 := *h.Resistance2
			if h.ATRWeekly != nil && *h.ATRWeekly > 0 {
				tp2 -= 0.25 * *h.ATRWeekly
			}
			lower := tp2 * (1 - margin)
			if price >= lower && price < tp2 {
				out = append(out, StageEvent{
					Kind: KindTP2Proximity, Color: "amber", Stage: h.Stage,
					Trigger: fmt.Sprintf("%s within %.1f%% of TP2 $%.2f — plan to sell remainder or trail", tickerOrName(h), pct(price, tp2), tp2),
				})
			} else if price >= tp2 {
				out = append(out, StageEvent{
					Kind: KindTP2Hit, Color: "green", Stage: h.Stage,
					Trigger: fmt.Sprintf("%s hit TP2 $%.2f — close remainder, or switch to trail-by-ATR", tickerOrName(h), tp2),
				})
			}
		}
		// AMBER_BOUNCE — price back below TP1 after touching it post-TP1.
		if h.TakeProfit != nil && *h.TakeProfit > 0 && price < *h.TakeProfit {
			out = append(out, StageEvent{
				Kind: KindBounce, Color: "amber", Stage: h.Stage,
				Trigger: fmt.Sprintf("%s rejected at $%.2f — consider closing remainder (thesis falsified)", tickerOrName(h), *h.TakeProfit),
			})
		}
	}

	// Time-stop review (any stage but stopped).
	if h.Stage != "stopped" && h.TimeStopReviewAt != nil && *h.TimeStopReviewAt != "" {
		t, err := time.Parse("2006-01-02", *h.TimeStopReviewAt)
		if err == nil && !time.Now().UTC().Before(t) {
			// Within ±5% of entry?
			if h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 {
				entry := *h.AvgOpenPrice
				pctOff := (price - entry) / entry
				if pctOff > -0.05 && pctOff < 0.05 {
					out = append(out, StageEvent{
						Kind: KindTimeStop, Color: "amber", Stage: h.Stage,
						Trigger: fmt.Sprintf("%s time-stop review due — held 6 months at ±5%% of entry. Reassess thesis.", tickerOrName(h)),
					})
				}
			}
		}
	}
	return out
}

func tickerOrName(h *domain.StockHolding) string {
	if h.Ticker != nil && *h.Ticker != "" {
		return *h.Ticker
	}
	return h.Name
}

func pct(price, ref float64) float64 {
	if ref <= 0 {
		return 0
	}
	return ((ref - price) / ref) * 100
}
