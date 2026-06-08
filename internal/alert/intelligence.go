// SC-35 Phase 4 — alert intelligence.
//
// A deterministic (no-LLM, Decision H) decision tree that attaches an *action
// clause* and a *severity* to each raw alert the existing producers emit
// (alert.go RSI/SL, stage_events.go TP/BOUNCE/TIME_STOP, proximity.go SL/TP).
// The verdict is keyed on `event × position_class × trend`, with the hard rule
// that **RSI/momentum never produces a sell/trim on a trend-intact hold**.
//
// Lives server-side; the Telegram bot only renders the action + severity it is
// handed (Decision I). Reuses the existing notification_log 24h dedup.
package alert

import (
	"fmt"
	"strings"

	"ft/internal/domain"
)

// TrendState classifies a holding against its weekly/daily trend MAs.
type TrendState int

const (
	// TrendUnknown — at least one trend MA is absent (thin history). The hard
	// RSI guard treats unknown as "not broken" so it never asserts a sell on a
	// name whose trend it can't actually evaluate.
	TrendUnknown TrendState = iota
	TrendIntact             // price > MA50W AND price > MA200D
	TrendBroken             // price at/below at least one trend line
)

// SC-35 Phase 4 re-classified alert kinds (extend the existing set). Kept as a
// distinct family so notification_log dedup and the bot's rendering can key on
// them. RSIInfoHold is mutable (the mute toggle suppresses it on the proactive
// ping for trend-intact holds).
const (
	KindTP1ProximityV2       = "tp1_proximity"
	KindTP2ProximityV2       = "tp2_proximity"
	KindSLProximityTech      = "sl_proximity_tech"
	KindSLBreachTech         = "sl_breach_tech"
	KindSLBreachVolEnv       = "sl_breach_volenv"
	KindResistanceReject     = "resistance_reject"
	KindWeeklyStructureBreak = "weekly_structure_break"
	KindRSIInfoHold          = "rsi_info_hold"
)

// Severity levels the bot renders. Distinct from the legacy RED/AMBER/GREEN
// AlertStatus: a severity is the *actionability* tier (Decision J re-tiering).
const (
	SeverityInfo  = "info"
	SeverityAmber = "amber"
	SeverityRed   = "red"
)

// Decision is the Phase-4 verdict attached to one alert.
type Decision struct {
	Action      string // human action clause; "" when none applies
	Severity    string // SeverityInfo | SeverityAmber | SeverityRed
	Kind        string // re-classified SC-35 kind (may equal the raw kind)
	RSIOnlyHold bool   // true ⇒ RSI-only on a trend-intact hold (mute candidate)
}

// Trend evaluates the SC-35 Phase-4 trend gate: trend_intact ⇔ price is above
// BOTH the 50-week and 200-day SMAs (the "to-the-moon" guard's spine). Returns
// TrendUnknown when either MA is absent so callers don't assert a regime they
// can't justify.
func Trend(h *domain.StockHolding) TrendState {
	if h.CurrentPrice == nil || h.MA50W == nil || h.MA200D == nil {
		return TrendUnknown
	}
	if *h.CurrentPrice > *h.MA50W && *h.CurrentPrice > *h.MA200D {
		return TrendIntact
	}
	return TrendBroken
}

// positionClass returns the holding's class, defaulting to "hold" (the
// conservative status quo) when unset.
func positionClass(h *domain.StockHolding) string {
	if h.PositionClass == "trade" {
		return "trade"
	}
	return "hold"
}

// Decide maps one raw emitted alert (its kind + triggers + the holding's class
// and trend) onto the Phase-4 decision matrix, returning the action clause and
// severity. `rawKind` is the producer's kind (e.g. "amber", "tp1_proximity",
// "bounce", "proximity_red_sl"); `triggers` is its human-readable body, used to
// disambiguate the base RSI/SL alert.
func Decide(h *domain.StockHolding, rawKind string, triggers []string, trend TrendState) Decision {
	class := positionClass(h)
	body := strings.ToLower(strings.Join(triggers, " "))

	switch rawKind {

	// ---- Base alert.go RED/AMBER: RSI-momentum or SL-distance driven --------
	case string(domain.AlertRed), string(domain.AlertAmber):
		rsiDriven := strings.Contains(body, "rsi")
		slDriven := strings.Contains(body, "stop loss") || strings.Contains(body, "stop-loss")

		// RSI/momentum with no level/stop component → the matrix RSI row.
		if rsiDriven && !slDriven {
			return decideRSI(class, trend)
		}
		// SL-distance driven (or mixed) → treat as a stop event by class.
		return decideStopDistance(h, class, rawKind)

	// ---- Take-profit proximity / hits --------------------------------------
	case KindTP1Proximity, KindTP1Hit, KindProximityAmberTP:
		if class == "trade" {
			return Decision{
				Kind:     KindTP1ProximityV2,
				Severity: SeverityAmber,
				Action:   appendRSINote(h, "Approaching TP1 — plan: scale ~33% and raise SL to breakeven."),
			}
		}
		return Decision{
			Kind:     KindTP1ProximityV2,
			Severity: SeverityInfo,
			Action:   "At R1 (next resistance). Runner — no action; R2 is the next level up.",
		}

	case KindTP2Proximity, KindTP2Hit, KindProximityRedTP:
		if class == "trade" {
			return Decision{
				Kind:     KindTP2ProximityV2,
				Severity: SeverityAmber,
				Action:   "Approaching TP2 — close remainder, or trail SL under the last weekly swing low.",
			}
		}
		return Decision{
			Kind:     KindTP2ProximityV2,
			Severity: SeverityInfo,
			Action:   "At R2 (next resistance) — informational; no fixed take-profit on a hold.",
		}

	// ---- Weekly-close rejection at resistance (BOUNCE) ----------------------
	case KindBounce:
		switch {
		case class == "trade":
			return Decision{Kind: KindResistanceReject, Severity: SeverityAmber,
				Action: "Rejected at resistance on the weekly close — consider closing remainder / tighten SL."}
		case trend == TrendBroken:
			return Decision{Kind: KindResistanceReject, Severity: SeverityAmber,
				Action: "Rejected at resistance; price near/below the 50-week — thesis-relevant, review."}
		default:
			return Decision{Kind: KindResistanceReject, Severity: SeverityInfo,
				Action: "Rejected at resistance — trend intact, informational."}
		}

	// ---- SL proximity / breach ---------------------------------------------
	case KindProximityAmberSL:
		if class == "trade" {
			return Decision{Kind: KindSLProximityTech, Severity: SeverityAmber,
				Action: "Approaching the technical stop — exit per plan if it breaks."}
		}
		return Decision{Kind: KindSLProximityTech, Severity: SeverityAmber,
			Action: "Approaching your vol-envelope catastrophe stop — thesis-check, not an auto-exit."}

	case KindProximityRedSL:
		if class == "trade" {
			return Decision{Kind: KindSLBreachTech, Severity: SeverityRed,
				Action: "Technical SL hit — exit per plan."}
		}
		return Decision{Kind: KindSLBreachVolEnv, Severity: SeverityRed,
			Action: "Down near your catastrophe stop — thesis-check trigger, NOT an auto-exit."}

	// ---- Weekly higher-low broken (runner structure break) ------------------
	case KindWeeklyStructureBreak:
		if class == "trade" {
			return Decision{Kind: KindWeeklyStructureBreak, Severity: SeverityRed,
				Action: "Trailing weekly-swing-low broken — exit the runner."}
		}
		return Decision{Kind: KindWeeklyStructureBreak, Severity: SeverityRed,
			Action: "Weekly uptrend structure broke — review whether the long-term thesis still holds."}

	// ---- Time-stop review --------------------------------------------------
	case KindTimeStop:
		return Decision{Kind: KindTimeStop, Severity: SeverityAmber,
			Action: "Time-stop review due — reassess the thesis."}
	}

	// Unknown kind: pass through with no action, neutral-info severity.
	return Decision{Kind: rawKind, Severity: SeverityInfo}
}

// decideRSI implements the matrix RSI row. The hard rule lives here: a
// trend-intact (or trend-unknown) hold never gets a sell/trim — only an
// informational note that is a mute candidate.
func decideRSI(class string, trend TrendState) Decision {
	if class == "trade" {
		return Decision{Kind: KindRSIInfoHold, Severity: SeverityInfo,
			Action: "RSI stretched — watching TP1; no action yet."}
	}
	// hold
	if trend == TrendBroken {
		return Decision{Kind: "rsi_review", Severity: SeverityAmber,
			Action: "RSI elevated AND price below the 50-week — momentum + trend both weakening, review thesis."}
	}
	// trend intact or unknown → informational, mutable.
	return Decision{Kind: KindRSIInfoHold, Severity: SeverityInfo, RSIOnlyHold: true,
		Action: "Momentum stretched — trend intact. Informational, no action."}
}

// decideStopDistance handles a base RED/AMBER driven by distance-to-stop.
func decideStopDistance(h *domain.StockHolding, class, rawKind string) Decision {
	red := rawKind == string(domain.AlertRed)
	sev := SeverityAmber
	if red {
		sev = SeverityRed
	}
	if class == "trade" {
		if red {
			return Decision{Kind: KindSLBreachTech, Severity: sev, Action: "At/through the technical stop — exit per plan."}
		}
		return Decision{Kind: KindSLProximityTech, Severity: sev, Action: "Approaching the technical stop."}
	}
	if red {
		return Decision{Kind: KindSLBreachVolEnv, Severity: sev,
			Action: "Near your catastrophe stop — thesis-check trigger, not an auto-exit."}
	}
	return Decision{Kind: KindSLProximityTech, Severity: sev,
		Action: "Approaching your vol-envelope stop — review."}
}

// appendRSINote tacks an "(RSI N confirms extension)" clause onto a trade TP1
// action when the holding is also overbought, per the matrix note.
func appendRSINote(h *domain.StockHolding, action string) string {
	if h.RSI14 != nil && *h.RSI14 >= 65 {
		return action + fmt.Sprintf(" (RSI %.0f confirms extension)", *h.RSI14)
	}
	return action
}
