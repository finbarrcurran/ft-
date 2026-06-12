package nexus

import (
	"ft/internal/domain"
	"ft/internal/market"
	"math"
)

// SC-36 W3 — Fundamentals engine. Forward P/E, next-FY EPS growth, Forward PEG
// from Yahoo's earningsTrend consensus (vs Visser's FMP). Per the handover §6:
//   Forward P/E      = price / next-FY EPS
//   Next-FY growth   = (nextFY − currentFY) / |currentFY|
//   Forward PEG      = Forward P/E / (growth × 100)   (only when growth > 0)
// data_status discipline (rows are kept, never dropped — UI renders "—"):
//   OK          — both estimates present and current-FY EPS > 0
//   NEG_BASE    — current-FY EPS ≤ 0 (growth base unusable)
//   MISSING_EST — a consensus estimate is absent
//   MISSING_PX  — no current price

// ComputeFundamentals builds a computed Fundamentals row from a Yahoo pull.
func ComputeFundamentals(ticker, asOf string, f *market.YahooFundamentals) *domain.NexusFundamentals {
	out := &domain.NexusFundamentals{Ticker: ticker, AsOf: asOf, Source: "computed"}
	ds := func(s string) *string { return &s }
	if f != nil {
		out.Price = f.Price
		out.MarketCap = f.MarketCap
		out.CurrentFYEPS = f.CurrentFYEPS
		out.NextFYEPS = f.NextFYEPS
	}
	switch {
	case f == nil || f.CurrentFYEPS == nil || f.NextFYEPS == nil:
		out.DataStatus = ds("MISSING_EST")
		return out
	case f.Price == nil:
		out.DataStatus = ds("MISSING_PX")
		return out
	}
	cur, next, price := *f.CurrentFYEPS, *f.NextFYEPS, *f.Price
	if cur <= 0 {
		out.DataStatus = ds("NEG_BASE")
		return out
	}
	status := "OK"
	if next != 0 {
		fpe := price / next
		out.FwdPE = &fpe
		growth := (next - cur) / math.Abs(cur)
		out.NextFYEPSGrowth = &growth
		if growth > 0 {
			peg := fpe / (growth * 100)
			out.FwdPEG = &peg
		}
		// UNSTABLE_BASE: a tiny current-FY EPS base makes growth (and so the
		// PEG ratio) hypersensitive to small cross-vendor estimate differences.
		// The PEG is still computed + displayed, but flagged for a W5 caution
		// marker. |growth| > 100% is the empirical threshold (7/90 OK rows).
		if math.Abs(growth) > 1.0 {
			status = "UNSTABLE_BASE"
		}
	}
	out.DataStatus = ds(status)
	return out
}
