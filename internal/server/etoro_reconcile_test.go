package server

import (
	"strings"
	"testing"

	"ft/internal/domain"
	"ft/internal/etoro"
)

func sp(s string) *string   { return &s }
func fp(f float64) *float64 { return &f }

func find(rows []reconRow, kind, action, ticker string) *reconRow {
	for i := range rows {
		if rows[i].Kind == kind && rows[i].Action == action && rows[i].Ticker == ticker {
			return &rows[i]
		}
	}
	return nil
}

// The SC-17 statement fallback (tightened 2026-10-11) on the real-world cases
// found when Fin ran the 2026-10-09 statement: 13 non-eToro-wallet crypto
// closures, false drift on GBp/JPY/EUR listings (units derived from a USD cost
// divided by a local-currency price), SU vs SU.PA as an add plus a closure, and
// a statement that cannot see partial closes.
func TestReconcileFallback(t *testing.T) {
	stocks := []*domain.StockHolding{
		{ID: 1, Name: "Rolls-Royce", Ticker: sp("RR.L"), InvestedUSD: 567.31, AvgOpenPrice: fp(1115.4653), Currency: sp("GBp")},
		{ID: 2, Name: "Rheinmetall", Ticker: sp("RHM.DE"), InvestedUSD: 262.97, AvgOpenPrice: fp(1380.6466), Currency: sp("EUR")},
		{ID: 3, Name: "Schneider", Ticker: sp("SU.PA"), InvestedUSD: 100, AvgOpenPrice: fp(275.05), Currency: sp("EUR")},
		{ID: 4, Name: "Gold", Ticker: sp("GLD"), InvestedUSD: 1917.68, AvgOpenPrice: fp(466.906)},
		{ID: 5, Name: "Micron", Ticker: sp("MU"), InvestedUSD: 200, AvgOpenPrice: fp(826)}, // FT is behind the live data
		{ID: 6, Name: "Old", Ticker: sp("ORCL"), InvestedUSD: 100},                         // no longer at eToro
	}
	cryptos := []*domain.CryptoHolding{
		{ID: 10, Symbol: "BTC", Wallet: sp("Ledger"), QuantityHeld: 0.5},
		{ID: 11, Symbol: "ETH", Wallet: sp("Binance"), QuantityHeld: 2},
		{ID: 12, Symbol: "SOL", Wallet: sp("eToro"), QuantityHeld: 3},
		{ID: 13, Symbol: "ADA", Wallet: sp("eToro"), QuantityHeld: 100}, // gone from eToro
		{ID: 14, Symbol: "XRP", QuantityHeld: 50},                       // no wallet recorded
		{ID: 15, Symbol: "SUI", Wallet: sp("Phantom"), QuantityHeld: 9},
	}
	holdings := []etoro.Holding{
		{Name: "RR", Ticker: "RR.L", Currency: "GBX", Units: 38.0796, InvestedUSD: 567.31, AvgPriceUSD: 14.9, Lots: 4, Underlying: "stock"},
		{Name: "RHM", Ticker: "RHM.DE", Currency: "EUR", Units: 0.216638, InvestedUSD: 380.79, Lots: 2, Underlying: "stock"},
		{Name: "SU", Ticker: "SU", Currency: "EUR", Units: 0.309418, InvestedUSD: 100, Lots: 1, Underlying: "stock"},
		{Name: "GLD", Ticker: "GLD", Currency: "USD", Units: 4.1073, InvestedUSD: 1917.68, AvgPriceUSD: 466.906, Underlying: "stock"},
		{Name: "MU", Ticker: "MU", Currency: "USD", Units: 0.2424, InvestedUSD: 200, Underlying: "stock"},
		{Name: "SOL", Ticker: "SOL", Units: 3, InvestedUSD: 500, Underlying: "crypto"},
		{Name: "BTC", Ticker: "BTC", Units: 0.01, InvestedUSD: 1000, Underlying: "crypto"},
	}
	live := map[string]liveHolding{
		"RR.L":   {Units: 38.0796, InvestedUSD: 567.31, AvgOpen: 1115.4653},
		"RHM.DE": {Units: 0.1632, InvestedUSD: 262.97, AvgOpen: 1380.6466},
		"SU.PA":  {Units: 0.309418, InvestedUSD: 100, AvgOpen: 275.05},
		"GLD":    {Units: 4.1073, InvestedUSD: 1917.68, AvgOpen: 466.906},
		"MU":     {Units: 0.2424, InvestedUSD: 450, AvgOpen: 826},
	}

	rows := reconcile(holdings, stocks, cryptos, live)

	// Crypto: only eToro-wallet rows are eToro's. Ledger/Binance/Phantom/no-wallet are untouched.
	for _, tk := range []string{"BTC", "ETH", "XRP", "SUI"} {
		if r := find(rows, "crypto", "close", tk); r != nil {
			t.Errorf("non-eToro-wallet crypto %s must never be proposed for closure: %+v", tk, r)
		}
	}
	if find(rows, "crypto", "close", "ADA") == nil {
		t.Error("an eToro-wallet row that eToro no longer holds should be proposed for closure")
	}
	if find(rows, "crypto", "add", "BTC") == nil {
		t.Error("BTC held at eToro but only at Ledger in FT: it's a new eToro holding (add), not a match")
	}
	if r := find(rows, "crypto", "insync", "SOL"); r == nil {
		t.Errorf("SOL (eToro wallet, same quantity) should be in sync; rows: %+v", rows)
	}

	// No false drift from currency-mixed unit derivation (RR.L was +7,387%).
	if r := find(rows, "stock", "insync", "RR.L"); r == nil {
		t.Errorf("RR.L must be in sync (units match the live data): %+v", find(rows, "stock", "drift", "RR.L"))
	} else if r.FTUnits != 38.0796 {
		t.Errorf("FT units should be the broker's 38.0796, got %v", r.FTUnits)
	}
	// Partial close: the statement overstates RHM.DE; FT already matches live → never "update from the statement".
	if r := find(rows, "stock", "insync", "RHM.DE"); r == nil || !strings.Contains(r.Note, "partial closes") {
		t.Errorf("RHM.DE should be in sync with a partial-close note: %+v", r)
	}
	// SU / SU.PA: one match, not an add plus a closure.
	if find(rows, "stock", "add", "SU") != nil || find(rows, "stock", "close", "SU.PA") != nil {
		t.Error("SU and SU.PA are the same holding — no add and no closure")
	}
	if r := find(rows, "stock", "insync", "SU.PA"); r == nil || r.Confidence != "needs_confirm" || !strings.Contains(r.Note, "omits the exchange suffix") {
		t.Errorf("SU should match SU.PA (needs_confirm, with a note): %+v", r)
	}
	// FT behind the live data → drift carrying the broker's figures, not the statement's.
	if r := find(rows, "stock", "drift", "MU"); r == nil || r.EtoroInvested != 450 || r.EtoroUnits != 0.2424 || r.EtoroAvgPrice != 826 {
		t.Errorf("MU should drift to the live figures: %+v", r)
	}
	if find(rows, "stock", "insync", "GLD") == nil {
		t.Error("GLD should be in sync")
	}
	if find(rows, "stock", "close", "ORCL") == nil {
		t.Error("ORCL (FT only) should still be proposed for closure")
	}

	// Without live data (SC-42 not configured): non-USD listings never show invented drift.
	rows = reconcile(holdings, stocks, cryptos, nil)
	if find(rows, "stock", "drift", "RR.L") != nil {
		t.Error("without live data a GBp listing must not drift on derived units")
	}
	if find(rows, "stock", "insync", "RR.L") == nil {
		t.Error("RR.L: invested amounts match, so in sync")
	}
	// …and a non-USD avg price from the statement (USD) is never handed on for writing.
	for _, r := range rows {
		if r.Kind == "stock" && r.Ticker == "RR.L" && r.EtoroAvgPrice != 0 {
			t.Errorf("statement USD average must not be applied to a GBp listing: %v", r.EtoroAvgPrice)
		}
	}
}
