package llm

import (
	"math"
	"testing"
)

func TestCurrentGenerationRates(t *testing.T) {
	for _, m := range []string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1", "claude-haiku-4-5"} {
		if _, ok := ModelRates[m]; !ok {
			t.Fatalf("no rate for %s", m)
		}
	}
	// Cache rates from the pricing page: Sonnet 5.5 reads at 0.05x input ($0.10), Haiku 5.5 at $0.01.
	if r := ModelRates["claude-sonnet-5-5"]; r.CacheRead != 0.10 || r.CacheWrite != 2.50 {
		t.Fatalf("sonnet 5.5 cache rates = %+v", r)
	}
	if r := ModelRates["claude-haiku-5-5"]; r.CacheRead != 0.01 || r.CacheWrite != 0.125 {
		t.Fatalf("haiku 5.5 cache rates = %+v", r)
	}
	// 1M input + 1M output tokens on Haiku 5.5 = $0.10 + $0.50.
	if got := ComputeCostUSD("claude-haiku-5-5", 1_000_000, 1_000_000, 0, 0); math.Abs(got-0.60) > 1e-9 {
		t.Fatalf("haiku 5.5 cost = %v, want 0.60", got)
	}
}
