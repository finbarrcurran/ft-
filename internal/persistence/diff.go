package persistence

import (
	"ft/internal/domain"
	"math"
	"strings"
)

// RowDiff captures one paired row's verdict (one of new / updated / unchanged
// / removed). For updated rows, ChangedFields lists which fields differ.
type RowDiff struct {
	Kind          string        `json:"kind"`          // "new" | "updated" | "unchanged" | "removed"
	Imported      any           `json:"imported,omitempty"`
	Current       any           `json:"current,omitempty"`
	ChangedFields []string      `json:"changedFields,omitempty"`
	Label         string        `json:"label"`
	Sub           string        `json:"sub,omitempty"`
}

// DiffCounts summarises a diff in numbers.
type DiffCounts struct {
	New       int `json:"new"`
	Updated   int `json:"updated"`
	Unchanged int `json:"unchanged"`
	Removed   int `json:"removed"`
}

// ImportDiff is the outcome of comparing imported vs current.
type ImportDiff struct {
	Rows   []RowDiff  `json:"rows"`
	Counts DiffCounts `json:"counts"`
}

// =============================================================================
// Stocks
// =============================================================================

func stockKey(h *domain.StockHolding) string {
	if h.Ticker != nil && strings.TrimSpace(*h.Ticker) != "" {
		return strings.ToLower(strings.TrimSpace(*h.Ticker))
	}
	return strings.ToLower(strings.TrimSpace(h.Name))
}

type stockField struct {
	name   string
	differ func(a, b *domain.StockHolding) bool
}

var stockFields = []stockField{
	{"name", func(a, b *domain.StockHolding) bool { return a.Name != b.Name }},
	{"ticker", func(a, b *domain.StockHolding) bool { return !eqStrP(a.Ticker, b.Ticker) }},
	{"category", func(a, b *domain.StockHolding) bool { return !eqStrP(a.Category, b.Category) }},
	{"investedUsd", func(a, b *domain.StockHolding) bool { return !eqFloat(a.InvestedUSD, b.InvestedUSD) }},
	{"avgOpenPrice", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.AvgOpenPrice, b.AvgOpenPrice) }},
	{"currentPrice", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.CurrentPrice, b.CurrentPrice) }},
	{"stopLoss", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.StopLoss, b.StopLoss) }},
	{"takeProfit", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.TakeProfit, b.TakeProfit) }},
	{"rsi14", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.RSI14, b.RSI14) }},
	{"ma50", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.MA50, b.MA50) }},
	{"ma200", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.MA200, b.MA200) }},
	{"support", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.Support, b.Support) }},
	{"resistance", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.Resistance, b.Resistance) }},
	{"analystTarget", func(a, b *domain.StockHolding) bool { return !eqFloatP(a.AnalystTarget, b.AnalystTarget) }},
	{"strategyNote", func(a, b *domain.StockHolding) bool { return a.StrategyNote != b.StrategyNote }},
}

// DiffStocks compares imported vs current stocks.
// Match key: lowercase trimmed ticker, falling back to lowercase trimmed name.
func DiffStocks(current, imported []*domain.StockHolding) ImportDiff {
	curByKey := make(map[string]*domain.StockHolding, len(current))
	impByKey := make(map[string]*domain.StockHolding, len(imported))
	for _, h := range current {
		curByKey[stockKey(h)] = h
	}
	for _, h := range imported {
		impByKey[stockKey(h)] = h
	}

	d := ImportDiff{Rows: []RowDiff{}}

	for _, imp := range imported {
		key := stockKey(imp)
		cur := curByKey[key]
		label := imp.Name
		sub := ""
		if imp.Ticker != nil {
			sub = *imp.Ticker
		}
		if cur == nil {
			d.Rows = append(d.Rows, RowDiff{
				Kind: "new", Label: label, Sub: sub,
			})
			d.Counts.New++
			continue
		}
		var changed []string
		for _, f := range stockFields {
			if f.differ(cur, imp) {
				changed = append(changed, f.name)
			}
		}
		if len(changed) == 0 {
			d.Rows = append(d.Rows, RowDiff{
				Kind: "unchanged", Label: label, Sub: sub,
			})
			d.Counts.Unchanged++
		} else {
			d.Rows = append(d.Rows, RowDiff{
				Kind: "updated", Label: label, Sub: sub, ChangedFields: changed,
			})
			d.Counts.Updated++
		}
	}
	for _, cur := range current {
		if _, ok := impByKey[stockKey(cur)]; !ok {
			label := cur.Name
			sub := ""
			if cur.Ticker != nil {
				sub = *cur.Ticker
			}
			d.Rows = append(d.Rows, RowDiff{
				Kind: "removed", Label: label, Sub: sub,
			})
			d.Counts.Removed++
		}
	}
	return d
}

// =============================================================================
// Crypto
// =============================================================================

func cryptoKey(h *domain.CryptoHolding) string {
	return strings.ToUpper(strings.TrimSpace(h.Symbol))
}

type cryptoField struct {
	name   string
	differ func(a, b *domain.CryptoHolding) bool
}

var cryptoFields = []cryptoField{
	{"name", func(a, b *domain.CryptoHolding) bool { return a.Name != b.Name }},
	{"symbol", func(a, b *domain.CryptoHolding) bool { return a.Symbol != b.Symbol }},
	{"classification", func(a, b *domain.CryptoHolding) bool { return a.Classification != b.Classification }},
	{"category", func(a, b *domain.CryptoHolding) bool { return !eqStrP(a.Category, b.Category) }},
	{"wallet", func(a, b *domain.CryptoHolding) bool { return !eqStrP(a.Wallet, b.Wallet) }},
	{"quantityHeld", func(a, b *domain.CryptoHolding) bool { return !eqFloat(a.QuantityHeld, b.QuantityHeld) }},
	{"quantityStaked", func(a, b *domain.CryptoHolding) bool { return !eqFloat(a.QuantityStaked, b.QuantityStaked) }},
	{"avgBuyEur", func(a, b *domain.CryptoHolding) bool { return !eqFloatP(a.AvgBuyEUR, b.AvgBuyEUR) }},
	{"costBasisEur", func(a, b *domain.CryptoHolding) bool { return !eqFloatP(a.CostBasisEUR, b.CostBasisEUR) }},
	{"currentPriceEur", func(a, b *domain.CryptoHolding) bool { return !eqFloatP(a.CurrentPriceEUR, b.CurrentPriceEUR) }},
	{"currentValueEur", func(a, b *domain.CryptoHolding) bool { return !eqFloatP(a.CurrentValueEUR, b.CurrentValueEUR) }},
	{"strategyNote", func(a, b *domain.CryptoHolding) bool { return a.StrategyNote != b.StrategyNote }},
}

// DiffCrypto compares imported vs current crypto holdings.
// Match key: uppercase trimmed symbol.
func DiffCrypto(current, imported []*domain.CryptoHolding) ImportDiff {
	curByKey := make(map[string]*domain.CryptoHolding, len(current))
	impByKey := make(map[string]*domain.CryptoHolding, len(imported))
	for _, h := range current {
		curByKey[cryptoKey(h)] = h
	}
	for _, h := range imported {
		impByKey[cryptoKey(h)] = h
	}

	d := ImportDiff{Rows: []RowDiff{}}

	for _, imp := range imported {
		key := cryptoKey(imp)
		cur := curByKey[key]
		label, sub := imp.Name, imp.Symbol
		if cur == nil {
			d.Rows = append(d.Rows, RowDiff{Kind: "new", Label: label, Sub: sub})
			d.Counts.New++
			continue
		}
		var changed []string
		for _, f := range cryptoFields {
			if f.differ(cur, imp) {
				changed = append(changed, f.name)
			}
		}
		if len(changed) == 0 {
			d.Rows = append(d.Rows, RowDiff{Kind: "unchanged", Label: label, Sub: sub})
			d.Counts.Unchanged++
		} else {
			d.Rows = append(d.Rows, RowDiff{
				Kind: "updated", Label: label, Sub: sub, ChangedFields: changed,
			})
			d.Counts.Updated++
		}
	}
	for _, cur := range current {
		if _, ok := impByKey[cryptoKey(cur)]; !ok {
			d.Rows = append(d.Rows, RowDiff{
				Kind: "removed", Label: cur.Name, Sub: cur.Symbol,
			})
			d.Counts.Removed++
		}
	}
	return d
}

// =============================================================================
// Comparison helpers
// =============================================================================

const floatTolerance = 1e-4

func eqFloat(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}
func eqFloatP(a, b *float64) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return math.Abs(*a-*b) < floatTolerance
}
func eqStrP(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
