package nexus

import (
	"encoding/json"
	"ft/internal/domain"
	"ft/internal/store"
)

// SC-36 W3 — Trend Score engine. Ten binary checks summing to 0–100 + an
// 8-label setup classification (VisserLabs Trend Score Methodology v2.1).
// Calibration fitted against the snapshots (see SC36_verification_report.md):
// RSI = Wilder; slope lookbacks L50=10, L200=15 trading days. Verified: score
// within ±10 = 95.9% of snapshot rows; setup-label agreement 90.7%.
const (
	trendSlopeL50  = 10
	trendSlopeL200 = 15
	trendMinBars   = 215 // 200-day MA + 15-bar slope lookback
)

// ComputeTrend builds a computed Trend snapshot for asOf from the ticker's bars
// and the SPY close history (for the relative-strength check). Returns nil with
// a reason when there is insufficient history (caller records it degraded).
func ComputeTrend(bars []store.DailyBarRow, spyByDate map[string]float64, ticker, asOf string) (*domain.NexusTechnical, string) {
	b := barsUpTo(bars, asOf)
	if len(b) < trendMinBars {
		return nil, "insufficient history (<215 bars)"
	}
	c := closesOf(b)
	price := c[len(c)-1]
	s20, _ := sma(c, 20)
	s50, _ := sma(c, 50)
	s200, _ := sma(c, 200)
	rsi, _ := wilderRSI(c, 14)
	sl50, _ := slopePct(c, 50, trendSlopeL50)
	sl200, _ := slopePct(c, 200, trendSlopeL200)
	r21, _ := retPct(c, 21)
	r63, _ := retPct(c, 63)
	r5, _ := retPct(c, 5)

	// Relative strength vs SPY: 21-day return of ticker minus SPY's.
	rsRel := 0.0
	haveRS := false
	if spyR, ok := spyRet21(spyByDate, asOf); ok {
		rsRel = r21 - spyR
		haveRS = true
	}

	// Volume vs 20-day average (prior 20 bars).
	volOK := false
	if len(b) >= 21 {
		var avg float64
		for _, x := range b[len(b)-21 : len(b)-1] {
			avg += x.Volume
		}
		avg /= 20
		volOK = b[len(b)-1].Volume > avg
	}

	bi := func(cond bool, pts int) int {
		if cond {
			return pts
		}
		return 0
	}
	score := bi(price > s200, 20) + bi(price > s50, 15) + bi(price > s20, 5) +
		bi(sl50 > 0, 10) + bi(sl200 > 0, 10) +
		bi(r21 > 0, 10) + bi(r63 > 0, 10) +
		bi(rsi >= 50 && rsi <= 70, 10) +
		bi(haveRS && rsRel > 0, 5) + bi(volOK, 5)

	label := trendSetupLabel(score, rsi, sl50, price > s200)

	atrPct := func() *float64 {
		if a, ok := atr(b, 14); ok && price != 0 {
			v := a / price * 100
			return &v
		}
		return nil
	}()
	dist52 := func() *float64 {
		n := 252
		if len(b) < n {
			n = len(b)
		}
		hi := b[len(b)-n].High
		for _, x := range b[len(b)-n:] {
			if x.High > hi {
				hi = x.High
			}
		}
		if hi == 0 {
			return nil
		}
		v := (price - hi) / hi * 100
		return &v
	}()

	fp := func(v float64) *float64 { return &v }
	comp, _ := json.Marshal(map[string]any{
		"p_gt_200": price > s200, "p_gt_50": price > s50, "p_gt_20": price > s20,
		"slope50_pos": sl50 > 0, "slope200_pos": sl200 > 0,
		"ret1m_pos": r21 > 0, "ret3m_pos": r63 > 0,
		"rsi_50_70": rsi >= 50 && rsi <= 70, "rs_pos": haveRS && rsRel > 0, "vol_gt_avg": volOK,
		"L50": trendSlopeL50, "L200": trendSlopeL200,
	})
	out := &domain.NexusTechnical{
		Ticker: ticker, AsOf: asOf, Source: "computed",
		Price: fp(price), TrendScore: &score, SetupLabel: &label,
		RSI14: fp(rsi), Ret1W: fp(r5), Ret1M: fp(r21), Ret3M: fp(r63),
		Vs20D: fp((price - s20) / s20 * 100), Vs50D: fp((price - s50) / s50 * 100), Vs200D: fp((price - s200) / s200 * 100),
		Slope50D: fp(sl50), Slope200D: fp(sl200), Dist52WHi: dist52, ATRPct: atrPct,
		Components: string(comp),
	}
	if haveRS {
		out.RSSpy = fp(rsRel)
	}
	return out, ""
}

// trendSetupLabel applies the 8-label precedence (Extended > Buyable;
// Pullback > Neutral; Early > Weakening).
func trendSetupLabel(score int, rsi, slope50 float64, aboveSMA200 bool) string {
	switch {
	case score >= 75 && rsi > 72:
		return "Strong but Extended"
	case score >= 80:
		return "Strong Uptrend / Buyable"
	case score >= 65:
		return "Constructive"
	case score >= 50 && score <= 64 && rsi < 45 && aboveSMA200:
		return "Pullback Opportunity"
	case score >= 50 && score <= 64:
		return "Neutral / Watch"
	case score >= 35 && score <= 49 && slope50 > 0:
		return "Early Trend Improvement"
	case score >= 35 && score <= 49:
		return "Weakening"
	default:
		return "Breakdown Risk"
	}
}

// spyRet21 is SPY's 21-trading-day return as of asOf, from a date→close map.
func spyRet21(spyByDate map[string]float64, asOf string) (float64, bool) {
	dates := make([]string, 0, len(spyByDate))
	for d := range spyByDate {
		if d <= asOf {
			dates = append(dates, d)
		}
	}
	if len(dates) < 22 {
		return 0, false
	}
	// partial selection of the 22 most recent is enough; sort ascending.
	sortStrings(dates)
	last := spyByDate[dates[len(dates)-1]]
	prev := spyByDate[dates[len(dates)-22]]
	if prev == 0 {
		return 0, false
	}
	return (last/prev - 1) * 100, true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
