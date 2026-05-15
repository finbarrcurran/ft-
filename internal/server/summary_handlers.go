package server

import (
	"fmt"
	"ft/internal/domain"
	"ft/internal/donut"
	"ft/internal/metrics"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// GET /api/summary
//
// One-shot aggregate for the Summary tab. Computes 4 KPI cards + 3 donut
// SVGs server-side. Cookie `display_currency` (set in Chunk 2C) will flip
// totals into EUR; for now everything is USD.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())

	stocks, err := s.store.ListStockHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}
	cryptos, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if err != nil {
		mapStoreError(w, err)
		return
	}

	// FX snapshot from meta (used for EUR display + currency-converted totals)
	fx := s.store.GetMetaFloat(r.Context(), "fx_snapshot_eur_usd", 1.08)
	currency := "USD" // Chunk 2C will read the display_currency cookie here.

	// ---- KPI numbers --------------------------------------------------
	var (
		stocksInvested  float64
		stocksValue     float64
		stocksTodayDeltaUSD float64
		stocksValued    bool

		cryptoCostUSD   float64
		cryptoValueUSD  float64
		cryptoTodayDeltaUSD float64
		cryptoValued    bool
	)
	for _, h := range stocks {
		stocksInvested += h.InvestedUSD
		m := metrics.ComputeStock(h)
		if m.CurrentValueUSD != nil {
			stocksValue += *m.CurrentValueUSD
			stocksValued = true
			if h.DailyChangePct != nil {
				// Today's $ change ≈ today's % × current value (the value
				// already includes today's move; multiplying is a small
				// approximation, but close enough for the headline figure).
				stocksTodayDeltaUSD += *m.CurrentValueUSD * (*h.DailyChangePct) / 100
			}
		}
	}
	for _, c := range cryptos {
		if c.CostBasisUSD != nil {
			cryptoCostUSD += *c.CostBasisUSD
		}
		m := metrics.ComputeCrypto(c)
		if m.CurrentValueUSD != nil {
			cryptoValueUSD += *m.CurrentValueUSD
			cryptoValued = true
			if c.DailyChangePct != nil {
				cryptoTodayDeltaUSD += *m.CurrentValueUSD * (*c.DailyChangePct) / 100
			}
		}
	}

	totalInvestedCost := stocksInvested + cryptoCostUSD
	totalValue := stocksValue + cryptoValueUSD
	totalPnl := totalValue - totalInvestedCost
	var totalPnlPct *float64
	if totalInvestedCost > 0 {
		v := totalPnl / totalInvestedCost * 100
		totalPnlPct = &v
	}
	todayChange := stocksTodayDeltaUSD + cryptoTodayDeltaUSD
	var todayPct *float64
	if totalValue > 0 {
		v := todayChange / (totalValue - todayChange) * 100 // % of yesterday's close
		todayPct = &v
	}

	// ---- Donut 1: Asset class -----------------------------------------
	classSlices := []donut.Slice{}
	if stocksValued {
		classSlices = append(classSlices, donut.Slice{
			Label: "Stocks", Value: stocksValue, Color: "rgb(255,184,0)",
		})
	}
	if cryptoValued {
		classSlices = append(classSlices, donut.Slice{
			Label: "Crypto", Value: cryptoValueUSD, Color: "rgb(16,200,124)",
		})
	}
	assetClassSVG := donut.Render(classSlices, donut.Options{
		Width: 200, Height: 200,
		CenterText: fmtMoney(totalValue, currency, fx),
		CenterSub:  "value",
	})

	// ---- Donut 2: Crypto Core / Alt -----------------------------------
	var coreValue, altValue float64
	for _, c := range cryptos {
		m := metrics.ComputeCrypto(c)
		if m.CurrentValueUSD == nil {
			continue
		}
		if c.IsCore {
			coreValue += *m.CurrentValueUSD
		} else {
			altValue += *m.CurrentValueUSD
		}
	}
	coreAltSlices := []donut.Slice{
		{Label: "Core", Value: coreValue, Color: "rgb(255,184,0)"},
		{Label: "Alt", Value: altValue, Color: "rgb(140,152,170)"},
	}
	coreAltSVG := donut.Render(coreAltSlices, donut.Options{
		Width: 200, Height: 200,
		CenterText: fmtMoney(coreValue+altValue, currency, fx),
		CenterSub:  "crypto",
	})

	// ---- Donut 3: Stocks by sector ------------------------------------
	bySector := map[string]float64{}
	for _, h := range stocks {
		m := metrics.ComputeStock(h)
		if m.CurrentValueUSD == nil {
			continue
		}
		sector := "Other"
		if h.Category != nil && *h.Category != "" {
			sector = *h.Category
		} else if h.Sector != nil && *h.Sector != "" {
			sector = *h.Sector
		}
		bySector[sector] += *m.CurrentValueUSD
	}
	// Sort sectors descending by value so colours rotate from biggest to smallest.
	type sectorRow struct {
		Name  string
		Value float64
	}
	var sortedSectors []sectorRow
	for name, v := range bySector {
		sortedSectors = append(sortedSectors, sectorRow{name, v})
	}
	sort.Slice(sortedSectors, func(i, j int) bool {
		return sortedSectors[i].Value > sortedSectors[j].Value
	})
	pal := donut.Palette(len(sortedSectors))
	sectorSlices := make([]donut.Slice, 0, len(sortedSectors))
	for i, r := range sortedSectors {
		sectorSlices = append(sectorSlices, donut.Slice{
			Label: r.Name, Value: r.Value, Color: pal[i],
		})
	}
	stocksSectorSVG := donut.Render(sectorSlices, donut.Options{
		Width: 200, Height: 200,
		CenterText: fmtMoney(stocksValue, currency, fx),
		CenterSub:  "stocks",
	})

	// Build legend payloads so the client can render "[colour] Label — value (%)"
	// without re-computing the math.
	classLegend := buildLegend(classSlices, currency, fx)
	coreAltLegend := buildLegend(coreAltSlices, currency, fx)
	sectorLegend := buildLegend(sectorSlices, currency, fx)

	// ---- Response -----------------------------------------------------
	resp := map[string]any{
		"asOf":           time.Now().UTC().Format(time.RFC3339),
		"currency":       currency,
		"fxEURUSD":       fx,
		"kpis": map[string]any{
			"totalValue":     totalValue,
			"totalInvested":  totalInvestedCost,
			"totalPnl":       totalPnl,
			"totalPnlPct":    totalPnlPct,
			"todayChange":    todayChange,
			"todayChangePct": todayPct,
			"cash":           nil, // Placeholder — Spec 2 D2 keeps this dim
			"valued":         stocksValued || cryptoValued,
		},
		"donuts": map[string]any{
			"assetClass":     assetClassSVG,
			"cryptoCoreAlt":  coreAltSVG,
			"stocksBySector": stocksSectorSVG,
		},
		"legends": map[string]any{
			"assetClass":     classLegend,
			"cryptoCoreAlt":  coreAltLegend,
			"stocksBySector": sectorLegend,
		},
		"counts": map[string]any{
			"stocks": len(stocks),
			"crypto": len(cryptos),
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

func buildLegend(slices []donut.Slice, currency string, fx float64) []map[string]any {
	total := 0.0
	for _, s := range slices {
		if s.Value > 0 {
			total += s.Value
		}
	}
	out := make([]map[string]any, 0, len(slices))
	for _, s := range slices {
		row := map[string]any{
			"label": s.Label,
			"color": s.Color,
			"value": s.Value,
			"valueStr": fmtMoney(s.Value, currency, fx),
		}
		if total > 0 {
			row["pct"] = s.Value / total * 100
		}
		out = append(out, row)
	}
	return out
}

// fmtMoney formats a USD-source number into either USD or EUR display.
// Chunk 2C uses this when display_currency cookie is set; for now it's
// always USD.
func fmtMoney(usd float64, currency string, fx float64) string {
	v := usd
	prefix := "$"
	if currency == "EUR" && fx > 0 {
		v = usd / fx
		prefix = "€"
	}
	abs := v
	if abs < 0 {
		abs = -abs
	}
	switch {
	case abs >= 1_000_000:
		return fmt.Sprintf("%s%s%.2fM", neg(v), prefix, abs/1_000_000)
	case abs >= 10_000:
		// thousands separator with no decimals
		whole := int64(v)
		return fmt.Sprintf("%s%s", prefix, withCommas(whole))
	default:
		return fmt.Sprintf("%s%s", prefix, withCommas2dp(v))
	}
}

func neg(v float64) string {
	if v < 0 {
		return "-"
	}
	return ""
}
func withCommas(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		if neg {
			return "-" + s
		}
		return s
	}
	first := len(s) % 3
	var out []byte
	if first > 0 {
		out = append(out, s[:first]...)
		out = append(out, ',')
	}
	for i := first; i < len(s); i += 3 {
		out = append(out, s[i:i+3]...)
		if i+3 < len(s) {
			out = append(out, ',')
		}
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
func withCommas2dp(v float64) string {
	negStr := ""
	if v < 0 {
		negStr = "-"
		v = -v
	}
	whole := int64(v)
	cents := int64((v - float64(whole)) * 100 + 0.5)
	if cents == 100 {
		whole++
		cents = 0
	}
	return fmt.Sprintf("%s%s.%02d", negStr, withCommas(whole), cents)
}

// Suppress unused-import warnings if we trim during iteration.
var _ = domain.AlertNeutral
