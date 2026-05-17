// The single entry point for every LLM call in FT. Spec 9c.1 D2/D3/D4.
//
// Grep `llm.Call(` to find every place FT spends tokens.

package llm

import (
	"context"
	"errors"
	"fmt"
	"ft/internal/store"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Tool is a placeholder type. v1 forbids tool use (no agentic loops), so
// any non-empty Tools slice in a CallRequest is rejected by the gate.
// Defined here so future agentic specs can flesh it out without breaking
// the Call() signature.
type Tool struct {
	Name        string
	Description string
}

// CallRequest is the input to llm.Call. FeatureID is required; everything
// else is optional with documented defaults.
type CallRequest struct {
	// FeatureID tags this call in usage logs. Required.
	// Recognised: "sunday_digest", "rescoring", "alert_text", "jarvis_query".
	// New features should add their own ID + a kill switch in user_preferences.
	FeatureID string

	// FeatureContext is free text — ticker, holding_id, request hash, etc.
	// Persisted for debug; first 200 chars also stored as request_summary.
	FeatureContext string

	// Model overrides the default ("" → user_preferences.llm_default_model).
	// Non-default models REQUIRE AllowUpgrade=true.
	Model        string
	AllowUpgrade bool

	// Prompt content. SystemPrompt + CacheableContext are wrapped with
	// cache_control: ephemeral. UserPrompt is NOT cached (per-call varying).
	SystemPrompt     string
	CacheableContext string
	UserPrompt       string

	// MaxOutputTokens — capped at user_preferences cap. Defaults to 1024.
	MaxOutputTokens int

	// Tools must be empty in v1 (no agentic loops). Future: tool-use spec
	// will plumb this through with a separate per-call cap.
	Tools []Tool
}

// CallResponse is what every caller of llm.Call receives.
type CallResponse struct {
	Text         string
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite   int
	CostUSD      float64
	LatencyMs    int
	// Outcome:
	//   "success"        — Anthropic returned a response
	//   "budget_blocked" — gate rejected before any API call
	//   "paused"         — global or per-feature kill switch on
	//   "error"          — API error or transport failure
	//   "truncated"      — response cut off at max_output_tokens
	Outcome string
}

// Errors returned by Call. Callers can use errors.Is() to distinguish.
var (
	ErrBudgetBlocked      = errors.New("llm: monthly or daily budget would be exceeded")
	ErrFeatureDisabled    = errors.New("llm: feature kill switch is off")
	ErrGloballyPaused     = errors.New("llm: globally paused via /llm pause or Settings")
	ErrMissingFeatureID   = errors.New("llm: FeatureID is required")
	ErrUpgradeRequired    = errors.New("llm: non-default model requires AllowUpgrade=true")
	ErrToolUseNotAllowed  = errors.New("llm: tool use is disabled in v1 (no agentic loops)")
	ErrInputTooLarge      = errors.New("llm: input exceeds max_input_tokens_per_call")
	ErrOutputCapTooLarge  = errors.New("llm: requested max_output_tokens exceeds cap")
	ErrUnknownModel       = errors.New("llm: model not in ModelRates table")
	ErrAPIKeyMissing      = errors.New("llm: ANTHROPIC_API_KEY not configured")
)

// Service wraps everything Call() needs. Constructed once at server boot
// (in cmd/ft/main.go) and passed to handlers as needed. Single-instance.
type Service struct {
	Store        *store.Store
	HTTPClient   *http.Client
	APIKey       string // from FT_ANTHROPIC_API_KEY env, populated at boot
	APIBaseURL   string // override for tests; default "https://api.anthropic.com"
	clock        func() time.Time
}

// NewService constructs a Service. APIKey can be "" — Call() will then
// reject with ErrAPIKeyMissing. The package compiles and the budget gate
// works even without a key; the only thing missing is actual API calls.
func NewService(st *store.Store, apiKey string) *Service {
	return &Service{
		Store:      st,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		APIKey:     apiKey,
		APIBaseURL: "https://api.anthropic.com",
		clock:      time.Now,
	}
}

// Call is THE entry point. Order of operations matches spec D3:
//
//	1. Validate request shape
//	2. Check global pause + per-feature kill switch
//	3. Resolve model + apply hard caps
//	4. Check emergency override + compute effective budget
//	5. Read today's spend + this-month's spend
//	6. Predict this call's cost
//	7. Hard-stop check (would this push us over?)
//	8. Threshold alert check (first crossing of 50/75/90/100%)
//	9. Make the API call (or skip if APIKey missing — test/install mode)
//	10. Compute real cost from token counts
//	11. Log to llm_usage_log + update llm_usage_daily
//	12. Return CallResponse
func (s *Service) Call(ctx context.Context, req CallRequest) (CallResponse, error) {
	started := s.clock()
	out := CallResponse{Outcome: "error"}

	// ----- 1. Validate request shape ----------------------------------
	if strings.TrimSpace(req.FeatureID) == "" {
		return out, ErrMissingFeatureID
	}
	if len(req.Tools) > 0 {
		return out, ErrToolUseNotAllowed
	}

	// ----- 2. Global / per-feature kill switches ----------------------
	if v, _ := s.Store.GetPreference(ctx, "llm_globally_paused"); v == "true" {
		s.logBlocked(ctx, req, "globally_paused")
		out.Outcome = "paused"
		return out, ErrGloballyPaused
	}
	if !s.featureEnabled(ctx, req.FeatureID) {
		s.logBlocked(ctx, req, "feature_disabled")
		out.Outcome = "paused"
		return out, ErrFeatureDisabled
	}

	// ----- 3. Resolve model + hard caps -------------------------------
	defaultModel, _ := s.Store.GetPreference(ctx, "llm_default_model")
	if defaultModel == "" {
		defaultModel = "claude-haiku-4-5"
	}
	model := req.Model
	if model == "" {
		model = defaultModel
	}
	if model != defaultModel && !req.AllowUpgrade {
		requireUpgrade, _ := s.Store.GetPreference(ctx, "llm_require_explicit_upgrade")
		if requireUpgrade != "false" {
			return out, ErrUpgradeRequired
		}
	}
	if !IsKnownModel(model) {
		return out, ErrUnknownModel
	}

	maxOut := req.MaxOutputTokens
	if maxOut <= 0 {
		maxOut = 1024
	}
	maxOutCap := s.intPref(ctx, "llm_max_output_tokens_per_call", 2000)
	if maxOut > maxOutCap {
		return out, ErrOutputCapTooLarge
	}

	// Rough input-token estimate: 4 chars ≈ 1 token. This is good enough
	// for the gate; real count comes back in the response.
	approxInputTokens := approxTokens(req.SystemPrompt + req.CacheableContext + req.UserPrompt)
	maxInCap := s.intPref(ctx, "llm_max_input_tokens_per_call", 20000)
	if approxInputTokens > maxInCap {
		return out, ErrInputTooLarge
	}

	// ----- 4-7. Budget gate ------------------------------------------
	monthlyCap := s.floatPref(ctx, "llm_budget_monthly_usd", 5.0)
	dailyCap := s.floatPref(ctx, "llm_budget_daily_usd", 0.50)

	// Emergency override: extends effective monthly cap until expiry.
	overrideUntilStr, _ := s.Store.GetPreference(ctx, "llm_emergency_override_until")
	overrideCapStr, _ := s.Store.GetPreference(ctx, "llm_emergency_override_cap_usd")
	overrideExtraCap := 0.0
	if overrideUntilStr != "" {
		untilT, err := time.Parse(time.RFC3339, overrideUntilStr)
		if err == nil && s.clock().Before(untilT) {
			if v, err := strconv.ParseFloat(overrideCapStr, 64); err == nil {
				overrideExtraCap = v
			}
		}
	}

	todaySpend := s.todayCostUSD(ctx)
	monthSpend := s.monthCostUSD(ctx)
	predicted := PredictCostUSD(model, approxInputTokens, maxOut)

	hardStop, _ := s.Store.GetPreference(ctx, "llm_hard_stop_enabled")
	if hardStop != "false" {
		if todaySpend+predicted > dailyCap {
			s.logBlocked(ctx, req, fmt.Sprintf("daily cap exceeded (today $%.4f + predicted $%.4f > cap $%.2f)", todaySpend, predicted, dailyCap))
			out.Outcome = "budget_blocked"
			return out, ErrBudgetBlocked
		}
		effectiveMonthlyCap := monthlyCap + overrideExtraCap
		if monthSpend+predicted > effectiveMonthlyCap {
			s.logBlocked(ctx, req, fmt.Sprintf("monthly cap exceeded (month $%.4f + predicted $%.4f > cap $%.2f%s)",
				monthSpend, predicted, effectiveMonthlyCap, overrideTag(overrideExtraCap)))
			out.Outcome = "budget_blocked"
			return out, ErrBudgetBlocked
		}
	}

	// Threshold alert check — fire BEFORE the call so user sees alert
	// reflecting the call about to happen, not 30s of latency later.
	// Implemented in alerts.go; non-blocking on alert-send failure.
	go s.checkAndFireThresholds(context.Background(), monthSpend+predicted, monthlyCap+overrideExtraCap, todaySpend+predicted, dailyCap)

	// ----- 8. Make the actual API call --------------------------------
	if s.APIKey == "" {
		// Infrastructure built, no key yet. Logged as "error" with a clear
		// message so the Settings panel can surface it.
		s.logCall(ctx, req, model, 0, 0, 0, 0, 0, "error", "ANTHROPIC_API_KEY not set", started)
		return out, ErrAPIKeyMissing
	}
	resp, err := s.invokeAnthropic(ctx, model, req, maxOut)
	latency := int(s.clock().Sub(started).Milliseconds())
	if err != nil {
		s.logCall(ctx, req, model, 0, 0, 0, 0, 0, "error", err.Error(), started)
		out.LatencyMs = latency
		return out, err
	}

	// ----- 9-10. Compute real cost + log -----------------------------
	realCost := ComputeCostUSD(model, resp.InputTokens, resp.OutputTokens, resp.CacheRead, resp.CacheWrite)
	outcome := "success"
	if resp.StopReason == "max_tokens" {
		outcome = "truncated"
	}
	s.logCall(ctx, req, model, resp.InputTokens, resp.OutputTokens, resp.CacheRead, resp.CacheWrite, realCost, outcome, "", started)

	return CallResponse{
		Text:         resp.Text,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CacheRead:    resp.CacheRead,
		CacheWrite:   resp.CacheWrite,
		CostUSD:      realCost,
		LatencyMs:    latency,
		Outcome:      outcome,
	}, nil
}

// ----- internal helpers --------------------------------------------------

func (s *Service) featureEnabled(ctx context.Context, featureID string) bool {
	// Map well-known FeatureIDs to their kill-switch key. Unknown IDs default
	// to enabled (don't block what we don't know about — call will still be
	// logged + gated by overall budget).
	switch featureID {
	case "sunday_digest":
		return s.boolPref(ctx, "llm_feature_sunday_digest", true)
	case "rescoring":
		return s.boolPref(ctx, "llm_feature_rescoring", true)
	case "alert_text":
		return s.boolPref(ctx, "llm_feature_alert_text", true)
	case "jarvis_query":
		return s.boolPref(ctx, "llm_feature_jarvis_query", true)
	}
	return true
}

func (s *Service) intPref(ctx context.Context, key string, def int) int {
	v, err := s.Store.GetPreference(ctx, key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Service) floatPref(ctx context.Context, key string, def float64) float64 {
	v, err := s.Store.GetPreference(ctx, key)
	if err != nil || v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func (s *Service) boolPref(ctx context.Context, key string, def bool) bool {
	v, err := s.Store.GetPreference(ctx, key)
	if err != nil || v == "" {
		return def
	}
	return v == "true"
}

// approxTokens estimates token count from a string. 4 chars ≈ 1 token is
// the well-known rule-of-thumb. Good enough for the gate; real count is
// returned by the API in the response.
func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func overrideTag(extra float64) string {
	if extra <= 0 {
		return ""
	}
	return fmt.Sprintf(" [override +$%.2f]", extra)
}

// logCall is shared between success + error paths.
func (s *Service) logCall(ctx context.Context, req CallRequest, model string, in, out, cR, cW int, cost float64, outcome, errMsg string, started time.Time) {
	latency := int(s.clock().Sub(started).Milliseconds())
	if err := s.Store.InsertLLMUsage(ctx, store.LLMUsageRow{
		CalledAt:         started.UTC(),
		FeatureID:        req.FeatureID,
		FeatureContext:   req.FeatureContext,
		Provider:         "anthropic",
		Model:            model,
		InputTokens:      in,
		OutputTokens:     out,
		CacheReadTokens:  cR,
		CacheWriteTokens: cW,
		CostUSD:          cost,
		Outcome:          outcome,
		ErrorMessage:     errMsg,
		LatencyMs:        latency,
		RequestSummary:   summarisePrompt(req.UserPrompt),
	}); err != nil {
		slog.Warn("llm: usage log insert failed", "err", err)
	}
	// Update the daily aggregate. Failures here don't break the call.
	if err := s.Store.UpdateLLMUsageDaily(ctx, started.UTC().Format("2006-01-02"), req.FeatureID, cost, outcome); err != nil {
		slog.Warn("llm: daily aggregate update failed", "err", err)
	}
}

func (s *Service) logBlocked(ctx context.Context, req CallRequest, reason string) {
	s.logCall(ctx, req, "", 0, 0, 0, 0, 0, "budget_blocked", reason, s.clock())
}

func summarisePrompt(p string) string {
	if len(p) <= 200 {
		return p
	}
	return p[:200] + "…"
}
