package llm

import (
	"context"
	"time"
)

// todayCostUSD reads llm_usage_daily for today (UTC). Returns 0 if no row.
func (s *Service) todayCostUSD(ctx context.Context) float64 {
	date := s.clock().UTC().Format("2006-01-02")
	cost, _ := s.Store.GetLLMUsageDailyCost(ctx, date)
	return cost
}

// monthCostUSD sums llm_usage_daily for the current UTC calendar month.
func (s *Service) monthCostUSD(ctx context.Context) float64 {
	now := s.clock().UTC()
	monthPrefix := now.Format("2006-01")
	total, _ := s.Store.GetLLMUsageMonthCost(ctx, monthPrefix)
	return total
}

// CurrentSpend returns the live (today, month) totals for the Settings
// dashboard. Exported for handlers; uses the same read path as the gate.
func (s *Service) CurrentSpend(ctx context.Context) (today, month float64) {
	return s.todayCostUSD(ctx), s.monthCostUSD(ctx)
}

// Caps returns the user_preferences-driven hard caps for the Settings UI.
type EffectiveCaps struct {
	MonthlyUSD       float64
	DailyUSD         float64
	OverrideExtraUSD float64
	OverrideUntil    string // RFC3339 or empty
	OverrideReason   string
	HardStopEnabled  bool
	GloballyPaused   bool
	DefaultModel     string
}

func (s *Service) Caps(ctx context.Context) EffectiveCaps {
	overrideExtra := 0.0
	overrideUntil, _ := s.Store.GetPreference(ctx, "llm_emergency_override_until")
	if overrideUntil != "" {
		t, err := time.Parse(time.RFC3339, overrideUntil)
		if err == nil && s.clock().Before(t) {
			overrideExtra = s.floatPref(ctx, "llm_emergency_override_cap_usd", 0)
		}
	}
	reason, _ := s.Store.GetPreference(ctx, "llm_emergency_override_reason")
	defModel, _ := s.Store.GetPreference(ctx, "llm_default_model")
	return EffectiveCaps{
		MonthlyUSD:       s.floatPref(ctx, "llm_budget_monthly_usd", 5.0),
		DailyUSD:         s.floatPref(ctx, "llm_budget_daily_usd", 0.50),
		OverrideExtraUSD: overrideExtra,
		OverrideUntil:    overrideUntil,
		OverrideReason:   reason,
		HardStopEnabled:  s.boolPref(ctx, "llm_hard_stop_enabled", true),
		GloballyPaused:   s.boolPref(ctx, "llm_globally_paused", false),
		DefaultModel:     defModel,
	}
}
