// Spec 9c.1 — LLM cost-discipline HTTP endpoints.
//
// Routes:
//   GET  /api/llm/spend          dashboard payload (today + month totals + by-feature/by-model)
//   GET  /api/llm/log            usage log table (filterable)
//   POST /api/llm/pause          {paused: bool}  — global kill switch
//   POST /api/llm/override       {capUsd, durationHours, reason}  — emergency override
//   POST /api/llm/override/clear — cancel an active override
//
// All token-or-cookie so the bot's /llm command can read state.

package server

import (
	"ft/internal/store"
	"net/http"
	"strconv"
	"time"
)

// GET /api/llm/spend
//
// Snapshot used by Settings dashboard + Telegram /llm command. Single
// round-trip; UI does no further math.
func (s *Server) handleLLMSpend(w http.ResponseWriter, r *http.Request) {
	if s.llm == nil {
		writeError(w, http.StatusServiceUnavailable, "llm service not configured")
		return
	}
	today, month := s.llm.CurrentSpend(r.Context())
	caps := s.llm.Caps(r.Context())
	// Last 30 days of daily aggregates for the sparkline.
	daily, _ := s.store.GetLLMUsageDailyRows(r.Context(), 30)
	// Per-feature breakdown for the current month: sum across all rows
	// that fall in this UTC calendar month.
	monthPrefix := time.Now().UTC().Format("2006-01")
	monthByFeature := map[string]float64{}
	monthByModel := map[string]float64{}
	monthCallCount := 0
	monthBlockedCount := 0
	for _, d := range daily {
		if !startsWith(d.Date, monthPrefix) {
			continue
		}
		monthCallCount += d.CallCount
		monthBlockedCount += d.BlockedCount
		for k, v := range d.CostByFeature {
			monthByFeature[k] += v
		}
	}
	// By-model breakdown comes from the raw log (daily aggregate doesn't
	// split by model). Filter to current month.
	monthStart, _ := time.Parse("2006-01-02", monthPrefix+"-01")
	if logs, err := s.store.ListLLMUsage(r.Context(), store.LLMUsageFilter{
		FromTS: monthStart.Unix(),
		Limit:  5000,
	}); err == nil {
		for _, l := range logs {
			monthByModel[l.Model] += l.CostUSD
		}
	}

	// Effective monthly cap accounting for active override.
	effectiveMonthly := caps.MonthlyUSD + caps.OverrideExtraUSD

	writeJSON(w, http.StatusOK, map[string]any{
		"today": today,
		"month": month,
		"caps": map[string]any{
			"monthlyUsd":       caps.MonthlyUSD,
			"dailyUsd":         caps.DailyUSD,
			"effectiveMonthly": effectiveMonthly,
			"hardStopEnabled":  caps.HardStopEnabled,
			"globallyPaused":   caps.GloballyPaused,
			"defaultModel":     caps.DefaultModel,
		},
		"override": map[string]any{
			"extraUsd": caps.OverrideExtraUSD,
			"until":    caps.OverrideUntil,
			"reason":   caps.OverrideReason,
		},
		"daily":     daily,
		"byFeature": monthByFeature,
		"byModel":   monthByModel,
		"counts": map[string]int{
			"month":   monthCallCount,
			"blocked": monthBlockedCount,
		},
	})
}

// GET /api/llm/log
//
// Filterable usage log. Query params:
//   feature= sunday_digest | rescoring | …
//   outcome= success | budget_blocked | paused | error | truncated
//   from=   unix seconds
//   to=     unix seconds
//   limit=  default 200, max 1000
func (s *Server) handleLLMLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)

	rows, err := s.store.ListLLMUsage(r.Context(), store.LLMUsageFilter{
		FeatureID: q.Get("feature"),
		Outcome:   q.Get("outcome"),
		FromTS:    from,
		ToTS:      to,
		Limit:     limit,
	})
	if mapStoreError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": rows, "count": len(rows)})
}

// POST /api/llm/pause
//
// Body: {"paused": true|false}
//
// Flips the global kill switch. When true, every llm.Call returns
// `paused` immediately without making an API call.
func (s *Server) handleLLMPause(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paused bool `json:"paused"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	val := "false"
	if req.Paused {
		val = "true"
	}
	if err := s.store.SetPreference(r.Context(), "llm_globally_paused", val); err != nil {
		mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"globallyPaused": req.Paused})
}

// POST /api/llm/override
//
// Body: {capUsd: number, durationHours: int (1..168), reason: string}
//
// Sets an active emergency override. Max duration 7 days. Spec 9c.1 D9.
func (s *Server) handleLLMOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CapUsd        float64 `json:"capUsd"`
		DurationHours int     `json:"durationHours"`
		Reason        string  `json:"reason"`
	}
	if !decodeJSON(r, w, &req) {
		return
	}
	if req.CapUsd <= 0 {
		writeError(w, http.StatusBadRequest, "capUsd must be > 0")
		return
	}
	if req.DurationHours <= 0 {
		req.DurationHours = 24
	}
	if req.DurationHours > 7*24 {
		writeError(w, http.StatusBadRequest, "durationHours capped at 168 (7 days)")
		return
	}
	if len(req.Reason) < 3 {
		writeError(w, http.StatusBadRequest, "reason required (min 3 chars)")
		return
	}
	until := time.Now().UTC().Add(time.Duration(req.DurationHours) * time.Hour).Format(time.RFC3339)
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_cap_usd", strconv.FormatFloat(req.CapUsd, 'f', 2, 64))
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_until", until)
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_reason", req.Reason)
	writeJSON(w, http.StatusOK, map[string]any{
		"capUsd": req.CapUsd,
		"until":  until,
		"reason": req.Reason,
	})
}

// POST /api/llm/override/clear
func (s *Server) handleLLMOverrideClear(w http.ResponseWriter, r *http.Request) {
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_cap_usd", "0")
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_until", "")
	_ = s.store.SetPreference(r.Context(), "llm_emergency_override_reason", "")
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}

// startsWith — tiny helper since strings.HasPrefix isn't already imported here.
func startsWith(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}

