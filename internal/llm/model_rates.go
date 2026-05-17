// Package llm is the single choke point for all outbound LLM API calls
// from FT. Spec 9c.1 — LLM Cost Discipline.
//
// Nothing else in the codebase imports the Anthropic SDK directly. Every
// LLM-flavored operation routes through llm.Call(). Grep for `llm.Call(`
// to find every place FT spends tokens.
package llm

// Rates is the per-million-token pricing for one model. CacheRead is
// typically 10% of input; CacheWrite (5-minute ephemeral cache) is
// typically 125% of input. Anthropic's official rates as of May 2026.
type Rates struct {
	Input      float64 // USD per million input tokens
	Output     float64 // USD per million output tokens
	CacheRead  float64 // USD per million cache-read tokens (cheap)
	CacheWrite float64 // USD per million cache-write tokens (premium)
}

// ModelRates is the canonical pricing table. Update when Anthropic changes
// pricing. The "source date" comment is for the quarterly review reminder.
//
// Source: anthropic.com/pricing, snapshot 2026-05-17.
var ModelRates = map[string]Rates{
	"claude-haiku-4-5":   {Input: 1.0, Output: 5.0, CacheRead: 0.10, CacheWrite: 1.25},
	"claude-sonnet-4-6":  {Input: 3.0, Output: 15.0, CacheRead: 0.30, CacheWrite: 3.75},
	"claude-opus-4-7":    {Input: 5.0, Output: 25.0, CacheRead: 0.50, CacheWrite: 6.25},
	// Aliases — Anthropic uses dated and non-dated model ids interchangeably.
	"claude-haiku-latest":  {Input: 1.0, Output: 5.0, CacheRead: 0.10, CacheWrite: 1.25},
	"claude-sonnet-latest": {Input: 3.0, Output: 15.0, CacheRead: 0.30, CacheWrite: 3.75},
}

// ComputeCostUSD returns the USD cost for one call given token counts.
// Returns 0 for unknown models (logged as warning; outcome still recorded).
func ComputeCostUSD(model string, inputTok, outputTok, cacheReadTok, cacheWriteTok int) float64 {
	r, ok := ModelRates[model]
	if !ok {
		return 0
	}
	return (float64(inputTok)/1e6)*r.Input +
		(float64(outputTok)/1e6)*r.Output +
		(float64(cacheReadTok)/1e6)*r.CacheRead +
		(float64(cacheWriteTok)/1e6)*r.CacheWrite
}

// PredictCostUSD estimates the cost of a call given INPUT tokens only.
// Used by the pre-call budget gate. Output tokens are unknown until the
// response arrives, so the predictor assumes worst-case (max_output_tokens
// at full output rate). Real post-call cost is logged for accounting.
func PredictCostUSD(model string, inputTok, maxOutputTok int) float64 {
	r, ok := ModelRates[model]
	if !ok {
		return 0
	}
	return (float64(inputTok)/1e6)*r.Input + (float64(maxOutputTok)/1e6)*r.Output
}

// IsKnownModel reports whether the table has rates for `model`. Unknown
// models pass the gate (we don't block what we don't know) but cost
// computation returns 0 — UI flags this as a calibration warning.
func IsKnownModel(model string) bool {
	_, ok := ModelRates[model]
	return ok
}
