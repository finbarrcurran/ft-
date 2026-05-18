// Spec 13 — Portfolio risk engine tests (Spec 9c D12).

package technicals

import (
	"strings"
	"testing"
)

// defaultCaps mirrors the user_preferences seed in Spec 9c.
func defaultCaps() RiskCaps {
	return RiskCaps{
		ConcentrationPct:      15,
		ThemeConcentrationPct: 30,
		TotalActivePct:        8,
		DrawdownCircuitPct:    10,
		PerTradeDefaultPct:    1,
		PerTradeMaxPct:        2,
	}
}

func TestCompute_NoPositions(t *testing.T) {
	r := Compute(nil, 100_000, 0, defaultCaps(), false, "")
	if r.TotalActiveRiskPct != 0 {
		t.Errorf("no positions → 0 active risk, got %v", r.TotalActiveRiskPct)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("no positions → no warnings, got %v", r.Warnings)
	}
}

func TestCompute_PortfolioValueZero(t *testing.T) {
	// Defensive: PV=0 should not divide by zero; returns zero map.
	r := Compute([]OpenPosition{{Ticker: "NVDA", PositionUSD: 1000}}, 0, 0, defaultCaps(), false, "")
	if len(r.Concentration) != 0 {
		t.Errorf("PV=0 should yield empty concentration, got %v", r.Concentration)
	}
}

func TestCompute_ConcentrationCapBreach(t *testing.T) {
	// NVDA at $20k of $100k portfolio = 20% → over 15% cap.
	positions := []OpenPosition{
		{Ticker: "NVDA", Sector: "Information Technology", PositionUSD: 20_000, RiskUSD: 500},
	}
	r := Compute(positions, 100_000, 0, defaultCaps(), false, "")
	if !almostEqual(r.Concentration["NVDA"], 20.0, 0.01) {
		t.Errorf("NVDA concentration got %v want 20.0", r.Concentration["NVDA"])
	}
	if !warningsContain(r.Warnings, "NVDA exceeds concentration cap") {
		t.Errorf("expected concentration warning for NVDA, got %v", r.Warnings)
	}
}

func TestCompute_ThemeConcentrationBreach(t *testing.T) {
	// Three AI-infra positions, each 12% → 36% of theme → over 30% cap.
	positions := []OpenPosition{
		{Ticker: "NVDA", BottleneckTag: "AI-Infra", PositionUSD: 12_000},
		{Ticker: "TSM",  BottleneckTag: "AI-Infra", PositionUSD: 12_000},
		{Ticker: "ASML", BottleneckTag: "AI-Infra", PositionUSD: 12_000},
	}
	r := Compute(positions, 100_000, 0, defaultCaps(), false, "")
	if !almostEqual(r.ThemeConcentration["AI-Infra"], 36.0, 0.01) {
		t.Errorf("AI-Infra theme got %v want 36.0", r.ThemeConcentration["AI-Infra"])
	}
	if !warningsContain(r.Warnings, "theme AI-Infra exceeds cap") {
		t.Errorf("expected theme warning, got %v", r.Warnings)
	}
}

func TestCompute_TotalActiveRiskBreach(t *testing.T) {
	// Total risk $10k of $100k = 10% → over 8% cap.
	positions := []OpenPosition{
		{Ticker: "NVDA", PositionUSD: 10_000, RiskUSD: 5_000},
		{Ticker: "TSM",  PositionUSD: 10_000, RiskUSD: 5_000},
	}
	r := Compute(positions, 100_000, 0, defaultCaps(), false, "")
	if !almostEqual(r.TotalActiveRiskPct, 10.0, 0.01) {
		t.Errorf("total active risk got %v want 10.0", r.TotalActiveRiskPct)
	}
	if !warningsContain(r.Warnings, "total active risk exceeds cap") {
		t.Errorf("expected total-risk warning, got %v", r.Warnings)
	}
}

func TestCompute_DrawdownAtCircuitThreshold(t *testing.T) {
	// 10% drawdown with circuit-breaker not yet active → warn.
	r := Compute(nil, 100_000, -10.0, defaultCaps(), false, "")
	if !warningsContain(r.Warnings, "drawdown") {
		t.Errorf("expected drawdown warning at -10%%, got %v", r.Warnings)
	}
}

func TestCompute_DrawdownPastThreshold_BreakerAlreadyActive(t *testing.T) {
	// If breaker is already active no need to re-warn — that's just noise.
	r := Compute(nil, 100_000, -12.0, defaultCaps(), true, "2026-06-01")
	if warningsContain(r.Warnings, "drawdown") {
		t.Errorf("breaker active → no fresh drawdown warning, got %v", r.Warnings)
	}
}

func TestCompute_ThemeFallbackToSectorThenUnknown(t *testing.T) {
	// Position with no bottleneck → falls back to Sector. No sector either → "Unknown".
	positions := []OpenPosition{
		{Ticker: "AAA", Sector: "Energy", PositionUSD: 5_000},
		{Ticker: "BBB", PositionUSD: 5_000},
	}
	r := Compute(positions, 100_000, 0, defaultCaps(), false, "")
	if _, ok := r.ThemeConcentration["Energy"]; !ok {
		t.Errorf("expected Energy theme bucket, got %v", r.ThemeConcentration)
	}
	if _, ok := r.ThemeConcentration["Unknown"]; !ok {
		t.Errorf("expected Unknown theme bucket, got %v", r.ThemeConcentration)
	}
}

// ----- AllowsNewTrade ---------------------------------------------------

func TestAllowsNewTrade_BlocksDuringCircuitBreaker(t *testing.T) {
	c := PortfolioRiskCheck{
		PortfolioValue:       100_000,
		Caps:                 defaultCaps(),
		CircuitBreakerActive: true,
	}
	ok, reasons := c.AllowsNewTrade(OpenPosition{Ticker: "NVDA", PositionUSD: 5_000})
	if ok {
		t.Fatalf("circuit breaker active → should block")
	}
	if !warningsContain(reasons, "circuit breaker active") {
		t.Errorf("expected breaker reason, got %v", reasons)
	}
}

func TestAllowsNewTrade_AllowsCleanTrade(t *testing.T) {
	c := PortfolioRiskCheck{
		PortfolioValue:     100_000,
		Concentration:      map[string]float64{},
		ThemeConcentration: map[string]float64{},
		Caps:               defaultCaps(),
	}
	ok, reasons := c.AllowsNewTrade(OpenPosition{
		Ticker: "PHANTOM", Sector: "Tech", PositionUSD: 5_000, RiskUSD: 200,
	})
	if !ok {
		t.Fatalf("clean trade should pass, got reasons %v", reasons)
	}
}

func TestAllowsNewTrade_BlocksWhenPredictedConcentrationExceedsCap(t *testing.T) {
	// NVDA already at 12% of $100k = $12k. Adding $5k more → 17% > 15% cap.
	c := PortfolioRiskCheck{
		PortfolioValue:     100_000,
		Concentration:      map[string]float64{"NVDA": 12.0},
		ThemeConcentration: map[string]float64{},
		Caps:               defaultCaps(),
	}
	ok, reasons := c.AllowsNewTrade(OpenPosition{
		Ticker: "NVDA", PositionUSD: 5_000,
	})
	if ok {
		t.Fatalf("17%% predicted concentration should block")
	}
	if !warningsContain(reasons, "concentration cap on NVDA") {
		t.Errorf("expected concentration reason, got %v", reasons)
	}
}

func TestAllowsNewTrade_BlocksWhenPredictedThemeExceedsCap(t *testing.T) {
	// AI theme at 28% + $5k new = 33% > 30% cap.
	c := PortfolioRiskCheck{
		PortfolioValue:     100_000,
		Concentration:      map[string]float64{},
		ThemeConcentration: map[string]float64{"AI": 28.0},
		Caps:               defaultCaps(),
	}
	ok, reasons := c.AllowsNewTrade(OpenPosition{
		Ticker: "NEW", BottleneckTag: "AI", PositionUSD: 5_000,
	})
	if ok {
		t.Fatalf("predicted theme 33%% should block")
	}
	if !warningsContain(reasons, "theme cap on AI") {
		t.Errorf("expected theme reason, got %v", reasons)
	}
}

func TestAllowsNewTrade_BlocksWhenPredictedTotalRiskExceedsCap(t *testing.T) {
	// Existing 7% total risk + $1500 new risk on $100k → 1.5% extra = 8.5% > 8%.
	c := PortfolioRiskCheck{
		PortfolioValue:     100_000,
		Concentration:      map[string]float64{},
		ThemeConcentration: map[string]float64{},
		TotalActiveRiskPct: 7.0,
		Caps:               defaultCaps(),
	}
	ok, reasons := c.AllowsNewTrade(OpenPosition{
		Ticker: "NEW", PositionUSD: 5_000, RiskUSD: 1_500,
	})
	if ok {
		t.Fatalf("predicted total risk 8.5%% should block")
	}
	if !warningsContain(reasons, "total active risk cap") {
		t.Errorf("expected total-risk reason, got %v", reasons)
	}
}

// ----- helpers ----------------------------------------------------------

func warningsContain(warnings []string, fragment string) bool {
	for _, w := range warnings {
		if strings.Contains(w, fragment) {
			return true
		}
	}
	return false
}
