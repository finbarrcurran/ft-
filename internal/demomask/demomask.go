// Package demomask implements SC-22 Demo / Privacy Mode.
//
// When demo mode is ON the API must return a believable but entirely synthetic
// book so the user can show FT to other people without exposing real position
// sizes, values, P&L or net worth (handover D22.1). The masking happens here,
// server-side, by rewriting the sensitive financial fields of the holding
// structs in place. Because every downstream computation (metrics.ComputeStock
// /ComputeCrypto, the Summary KPIs, all five donuts, the risk dashboard) is
// derived from these raw fields, masking the holdings once makes the entire
// payload internally consistent — there is no second place that can leak a real
// number.
//
// Design rules baked in from the handover:
//
//   - D22.1 Server-side: real values are overwritten before they leave the
//     process; the browser never receives them.
//   - D22.2 Synthetic, not scaled: values are generated from a seeded RNG with
//     weights that are independent of real position sizes. There is no constant
//     factor between any real and demo number, so the masking is not reversible
//     from public data. Each line stays internally consistent because the real
//     (public) current price is kept and units are derived from it
//     (units × price = demo value).
//   - D22.3 Target = a fixed ~£30k notional book (TargetBookUSD).
//   - D22.4 Realistic spread: most lines modestly green, 1–2 small losses, book
//     net positive overall.
//   - D22.5 Deterministic: the RNG is seeded from a hash of the ticker list, so
//     the demo book is identical on every refresh (no jitter mid-demo).
package demomask

import (
	"hash/fnv"
	"math"
	"math/rand"
	"sort"

	"ft/internal/domain"
)

// TargetBookUSD is the synthetic notional book size. The handover fixes a £30k
// book (D22.3); the app is USD-denominated internally, so we target the USD
// equivalent at a fixed reference GBP→USD rate. Not user-editable in v1.
const (
	targetBookGBP = 30000.0
	gbpUSDRef     = 1.27
	// TargetBookUSD ≈ $38,100 — a believable small (~£30k) book.
	TargetBookUSD = targetBookGBP * gbpUSDRef

	seedSalt = "ft-demo-mode-v1"
)

// MaskBook deterministically overwrites the sensitive financial fields of the
// given stock + crypto holdings with a single synthetic book worth ~£30k. The
// stocks and cryptos must be passed together so the £30k is allocated across
// the whole book and the per-line demo values are stable regardless of which
// endpoint composed the call (Summary, the holdings tabs, or the risk
// dashboard all see the same numbers). Mutates the holdings in place.
func MaskBook(stocks []*domain.StockHolding, cryptos []*domain.CryptoHolding, fxEURUSD float64) {
	if fxEURUSD <= 0 {
		fxEURUSD = 1.08
	}

	// One line per holding, carrying the real (public) current price and a
	// pointer back to the source struct.
	type line struct {
		key      string
		price    float64
		isCrypto bool
		idx      int
	}
	lines := make([]line, 0, len(stocks)+len(cryptos))
	for i, h := range stocks {
		key := h.Name
		if h.Ticker != nil && *h.Ticker != "" {
			key = *h.Ticker
		}
		price := 0.0
		if h.CurrentPrice != nil && *h.CurrentPrice > 0 {
			price = *h.CurrentPrice
		}
		lines = append(lines, line{key: "S:" + key, price: price, isCrypto: false, idx: i})
	}
	for i, h := range cryptos {
		price := 0.0
		if h.CurrentPriceUSD != nil && *h.CurrentPriceUSD > 0 {
			price = *h.CurrentPriceUSD
		}
		lines = append(lines, line{key: "C:" + h.Symbol, price: price, isCrypto: true, idx: i})
	}
	if len(lines) == 0 {
		return
	}

	// Deterministic order independent of the input slice order, so the seed and
	// the resulting allocation never depend on DB row ordering.
	sort.Slice(lines, func(i, j int) bool { return lines[i].key < lines[j].key })

	// Seed the RNG from the concatenated key list + salt (D22.5).
	seedStr := seedSalt
	for _, l := range lines {
		seedStr += "|" + l.key
	}
	rng := rand.New(rand.NewSource(int64(fnv64(seedStr)))) //nolint:gosec // determinism, not crypto

	// 1) Allocation weights — independent of real position sizes (D22.2). A
	//    0.5..1.5 spread gives believable variation without anything real.
	weights := make([]float64, len(lines))
	var wsum float64
	for i := range lines {
		weights[i] = 0.5 + rng.Float64()
		wsum += weights[i]
	}

	// 2) Per-line synthetic gain: most lines +3%..+25% (D22.4).
	gains := make([]float64, len(lines))
	for i := range lines {
		gains[i] = 0.03 + rng.Float64()*0.22
	}
	// 3) Turn 1–2 lines into small losses (−2%..−8%) for believability. Never
	//    all-green (looks staged); the green lines dominate so the book stays
	//    net positive overall.
	nLoss := 1
	if len(lines) >= 5 {
		nLoss = 2
	}
	for n := 0; n < nLoss; n++ {
		li := rng.Intn(len(lines))
		gains[li] = -(0.02 + rng.Float64()*0.06)
	}

	for i, l := range lines {
		demoValue := weights[i] / wsum * TargetBookUSD
		demoCost := demoValue / (1 + gains[i])
		price := l.price
		if price <= 0 {
			// No public price available — synthesize a plausible one so the line
			// is still valued. Deterministic via the same RNG stream.
			price = 10 + rng.Float64()*490
		}
		units := demoValue / price
		if units <= 0 {
			continue
		}
		if l.isCrypto {
			maskCrypto(cryptos[l.idx], demoValue, demoCost, price, units, fxEURUSD)
		} else {
			maskStock(stocks[l.idx], demoCost, price, units)
		}
	}
}

// maskStock rewrites a stock holding so that metrics.ComputeStock yields:
//
//	quantity     = invested / avgOpen        = demoCost / avgOpen = units
//	currentValue = quantity × currentPrice   = units × price      = demoValue
//	pnl          = currentValue − invested   = demoValue − demoCost
//
// The real (public) current price is kept; avgOpen is back-solved from the
// synthetic cost and units. Strategic price levels (SL/TP, S/R, proposed entry)
// are cleared so the demo never reveals real risk placement (handover §2).
func maskStock(h *domain.StockHolding, demoCost, price, units float64) {
	h.InvestedUSD = round2(demoCost)
	avg := round4(demoCost / units)
	h.AvgOpenPrice = &avg
	cp := round4(price)
	h.CurrentPrice = &cp
	h.RealizedPnLUSD = 0

	// Hide real risk placement / strategy levels.
	h.StopLoss = nil
	h.TakeProfit = nil
	h.ProposedEntry = nil
	h.Support = nil
	h.Resistance = nil
	h.Support1 = nil
	h.Support2 = nil
	h.Resistance1 = nil
	h.Resistance2 = nil
}

// maskCrypto rewrites a crypto holding's value/cost/quantity to the synthetic
// line and mirrors the EUR figures via the FX snapshot so both currencies stay
// consistent. ComputeCrypto prefers CurrentValueUSD, so setting it directly is
// authoritative.
func maskCrypto(h *domain.CryptoHolding, demoValue, demoCost, price, units, fxEURUSD float64) {
	h.QuantityHeld = round4(units)
	h.QuantityStaked = 0

	cb := round2(demoCost)
	h.CostBasisUSD = &cb
	cv := round2(demoValue)
	h.CurrentValueUSD = &cv
	cp := round4(price)
	h.CurrentPriceUSD = &cp
	avgUSD := round4(demoCost / units)
	h.AvgBuyUSD = &avgUSD

	// EUR mirror (snapshot is eur_usd: usd = eur × fx → eur = usd / fx).
	cbE := round2(demoCost / fxEURUSD)
	h.CostBasisEUR = &cbE
	cvE := round2(demoValue / fxEURUSD)
	h.CurrentValueEUR = &cvE
	cpE := round4(price / fxEURUSD)
	h.CurrentPriceEUR = &cpE
	avgE := round4(avgUSD / fxEURUSD)
	h.AvgBuyEUR = &avgE

	h.RealizedPnLUSD = 0

	// Hide real risk placement / strategy levels.
	h.Support1 = nil
	h.Support2 = nil
	h.Resistance1 = nil
	h.Resistance2 = nil
}

func fnv64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
