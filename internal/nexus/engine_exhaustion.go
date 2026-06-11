package nexus

import (
	"encoding/json"
	"ft/internal/domain"
	"ft/internal/store"
	"math"
)

// SC-36 W3 — Exhaustion engine. Absolute 0–100 weighted composite of 11
// overextension signals (AI Macro Nexus Exhaustion Model). Each raw signal maps
// linearly between a floor (→0) and ceiling (→100), clamped, ×weight; final =
// Σ(weight×component) / available-weight, clipped 1–100. Verified against the
// snapshots: score within ±8 = 90.3%, band agreement 92.1% (fully bar-computed,
// incl. the TD approximation). The TD-Score rule is exact (500/500):
// max(max(0,setup−5)×25, max(0,countdown−8)×20); the raw countdown-from-bars is
// a documented approximation (~37% raw match, bounded score impact).
const exhMinBars = 70

type exhWeight struct {
	key string
	wt  float64
}

var exhWeights = []exhWeight{
	{"rsi14", 10}, {"rsi5", 7}, {"wr", 8}, {"pos20", 7}, {"ext20", 11},
	{"ext50", 9}, {"retvol", 11}, {"imp5", 7}, {"upvol", 8}, {"atrexp", 8}, {"td", 14},
}

// ComputeExhaustion builds a computed Exhaustion snapshot for asOf.
func ComputeExhaustion(bars []store.DailyBarRow, ticker, asOf string) (*domain.NexusExhaustion, string) {
	b := barsUpTo(bars, asOf)
	if len(b) < exhMinBars {
		return nil, "insufficient history (<70 bars)"
	}
	c := closesOf(b)
	price := c[len(c)-1]
	a14, _ := atr(b, 14)

	rsi14, ok14 := wilderRSI(c, 14)
	rsi5, ok5 := wilderRSI(c, 5)
	wr, okwr := williamsR(b, 14)
	pos20, okpos := rangePos(b, 20)
	s20, _ := sma(c, 20)
	s50, hasS50 := sma(c, 50)

	var ext20, ext50 float64
	okext20, okext50 := false, false
	if a14 > 0 {
		ext20 = (price - s20) / a14
		okext20 = true
		if hasS50 {
			ext50 = (price - s50) / a14
			okext50 = true
		}
	}

	rets := dailyReturns(c)
	var retvol float64
	okrv := false
	if len(rets) >= 63 {
		rv := pstdev(rets[len(rets)-63:]) * math.Sqrt(21)
		if r21, ok := retPct(c, 21); ok && rv > 0 {
			retvol = (r21 / 100) / rv
			okrv = true
		}
	}

	var imp5 float64
	okimp := false
	if a14 > 0 && len(c) > 6 {
		imp5 = (price - c[len(c)-6]) / a14
		okimp = true
	}

	var volratio float64
	okvol := false
	upday := len(c) >= 2 && c[len(c)-1] > c[len(c)-2]
	if len(b) >= 21 {
		var avg float64
		for _, x := range b[len(b)-21 : len(b)-1] {
			avg += x.Volume
		}
		avg /= 20
		if avg > 0 {
			volratio = b[len(b)-1].Volume / avg
			okvol = true
		}
	}

	// ATR expansion: current ATR% vs the median ATR% over the last ~60 bars.
	var atrexp float64
	okae := false
	{
		var pcts []float64
		start := len(b) - 60
		if start < 15 {
			start = 15
		}
		for k := start; k < len(b); k++ {
			if aa, ok := atr(b[:k+1], 14); ok && b[k].Close != 0 {
				pcts = append(pcts, aa/b[k].Close*100)
			}
		}
		if len(pcts) >= 5 {
			med := medianOf(pcts)
			if med > 0 {
				atrexp = pcts[len(pcts)-1] / med
				okae = true
			}
		}
	}

	// TD approximation.
	tdSetup := tdSetupBars(c)
	tdCnt := tdCountdownBars(b)
	tdScore := tdPressure(tdSetup, tdCnt)

	comp := map[string]float64{}
	valid := map[string]bool{}
	set := func(k string, v float64, ok bool) {
		if ok {
			comp[k] = clamp100(v)
			valid[k] = true
		}
	}
	set("rsi14", (rsi14-55)/20*100, ok14)
	set("rsi5", (rsi5-60)/25*100, ok5)
	set("wr", (wr+40)/35*100, okwr)
	set("pos20", (pos20-70)/30*100, okpos)
	set("ext20", (ext20-0.5)/2.5*100, okext20)
	set("ext50", (ext50-1.0)/4.0*100, okext50)
	set("retvol", (retvol-0.5)/2.0*100, okrv)
	set("imp5", (imp5-0.75)/1.75*100, okimp)
	if okvol {
		uv := 0.0
		if upday {
			uv = (volratio - 1.0) / 1.5 * 100
		}
		set("upvol", uv, true)
	}
	set("atrexp", (atrexp-0.8)/0.7*100, okae)
	comp["td"] = tdScore
	valid["td"] = true

	var num, den float64
	for _, w := range exhWeights {
		if valid[w.key] {
			num += w.wt * comp[w.key]
			den += w.wt
		}
	}
	if den == 0 {
		return nil, "no valid exhaustion components"
	}
	score := num / den
	if score < 1 {
		score = 1
	}
	if score > 100 {
		score = 100
	}
	band := exhBand(score)

	fp := func(v float64) *float64 { return &v }
	ip := func(v int) *int { return &v }
	cj, _ := json.Marshal(map[string]any{"components": comp, "td_setup": tdSetup, "td_countdown": tdCnt})
	out := &domain.NexusExhaustion{
		Ticker: ticker, AsOf: asOf, Source: "computed",
		Price: fp(price), ExhScore: fp(score), Band: &band,
		RSI14: fpIfWil(rsi14, ok14), RSI5: fpIfWil(rsi5, ok5), WilliamsR: fpIfWil(wr, okwr),
		Pos20D: fpIfWil(pos20, okpos), Ext20DATR: fpIfWil(ext20, okext20), Ext50DATR: fpIfWil(ext50, okext50),
		RetVol1M: fpIfWil(retvol, okrv), Imp5DATR: fpIfWil(imp5, okimp), VolRatio: fpIfWil(volratio, okvol),
		ATRExpansion: fpIfWil(atrexp, okae),
		TDSetup:      ip(tdSetup), TDCountdown: ip(tdCnt), TDScore: fp(tdScore),
		ATRPct:     fpIfWil(safeDiv(a14, price)*100, a14 > 0 && price != 0),
		DataWtPct:  fp(den),
		Components: string(cj),
	}
	return out, ""
}

func fpIfWil(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &v
}
func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func exhBand(s float64) string {
	switch {
	case s >= 75:
		return "Extreme"
	case s >= 60:
		return "Elevated"
	case s >= 40:
		return "Moderate"
	default:
		return "Low"
	}
}

// tdSetupBars: consecutive closes > close 4 bars earlier, capped 9 (98% match).
func tdSetupBars(c []float64) int {
	run := 0
	for i := 4; i < len(c); i++ {
		if c[i] > c[i-4] {
			if run < 9 {
				run++
			}
		} else {
			run = 0
		}
	}
	return run
}

// tdCountdownBars: consecutive closes >= the high 2 bars earlier, capped 13
// (documented approximation — Visser's exact countdown is underspecified).
func tdCountdownBars(b []store.DailyBarRow) int {
	run := 0
	for i := 2; i < len(b); i++ {
		if b[i].Close >= b[i-2].High {
			if run < 13 {
				run++
			}
		} else {
			run = 0
		}
	}
	return run
}

// tdPressure is the exact (500/500-fit) TD-Score rule.
func tdPressure(setup, countdown int) float64 {
	sp := 0.0
	if setup > 5 {
		sp = float64(setup-5) * 25
	}
	cp := 0.0
	if countdown > 8 {
		cp = float64(countdown-8) * 20
	}
	if sp > cp {
		return sp
	}
	return cp
}
