// Per-tab CSV export — v1.5.
//
// GET /api/export.csv?tab=stocks|crypto|watchlist
//
// Streams a flat CSV of the user's current state for the named tab. Designed
// for one-click "Download" from the Stocks / Crypto / Watchlist tabs so the
// user can drop the rows into Excel/Sheets without going through the master
// xlsx round-trip endpoint (which is meant for full import/export, not
// per-tab snapshots).
//
// Column choice mirrors what the user actually sees on the tab — name/ticker
// first, then the columns that matter for review.

package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GET /api/export.csv?tab=stocks|crypto|watchlist
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromContext(r.Context())
	tab := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tab")))
	if tab == "" {
		tab = "stocks"
	}

	switch tab {
	case "stocks":
		s.writeStocksCSV(w, r, userID)
	case "crypto":
		s.writeCryptoCSV(w, r, userID)
	case "watchlist":
		s.writeWatchlistCSV(w, r, userID)
	default:
		writeError(w, http.StatusBadRequest, "tab must be stocks|crypto|watchlist")
	}
}

func setCSVHeaders(w http.ResponseWriter, tab string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ft-%s-%s.csv"`,
		tab, time.Now().UTC().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) writeStocksCSV(w http.ResponseWriter, r *http.Request, userID int64) {
	holdings, err := s.store.ListStockHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	setCSVHeaders(w, "stocks")
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"ticker", "name", "category", "sector", "currency",
		"invested_usd", "avg_open_price", "current_price",
		"unrealized_pnl_usd", "unrealized_pnl_pct",
		"rsi14", "ma50", "ma200", "support", "resistance",
		"stop_loss", "take_profit", "analyst_target",
		"beta", "volatility_12m_pct",
		"earnings_date", "ex_dividend_date",
		"thesis_link", "note",
	})
	for _, h := range holdings {
		var pnlUSD, pnlPct string
		if h.CurrentPrice != nil && h.AvgOpenPrice != nil && *h.AvgOpenPrice > 0 && h.InvestedUSD > 0 {
			pct := (*h.CurrentPrice/(*h.AvgOpenPrice) - 1.0) * 100.0
			pnlPct = fmt.Sprintf("%.2f", pct)
			pnlUSD = fmt.Sprintf("%.2f", h.InvestedUSD*pct/100.0)
		}
		_ = cw.Write([]string{
			deref(h.Ticker), h.Name, deref(h.Category), deref(h.Sector), deref(h.Currency),
			fmt.Sprintf("%.2f", h.InvestedUSD),
			fmtFloatPtr(h.AvgOpenPrice, 4),
			fmtFloatPtr(h.CurrentPrice, 4),
			pnlUSD, pnlPct,
			fmtFloatPtr(h.RSI14, 2),
			fmtFloatPtr(h.MA50, 4),
			fmtFloatPtr(h.MA200, 4),
			fmtFloatPtr(h.Support, 4),
			fmtFloatPtr(h.Resistance, 4),
			fmtFloatPtr(h.StopLoss, 4),
			fmtFloatPtr(h.TakeProfit, 4),
			fmtFloatPtr(h.AnalystTarget, 4),
			fmtFloatPtr(h.Beta, 3),
			fmtFloatPtr(h.Volatility12mPct, 2),
			deref(h.EarningsDate), deref(h.ExDividendDate),
			deref(h.ThesisLink), deref(h.Note),
		})
	}
}

func (s *Server) writeCryptoCSV(w http.ResponseWriter, r *http.Request, userID int64) {
	holdings, err := s.store.ListCryptoHoldings(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	setCSVHeaders(w, "crypto")
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"symbol", "name", "classification", "is_core", "wallet", "current_location",
		"quantity_held", "quantity_staked",
		"avg_buy_eur", "cost_basis_eur", "current_price_eur", "current_value_eur",
		"avg_buy_usd", "cost_basis_usd", "current_price_usd", "current_value_usd",
		"rsi14", "daily_change_pct", "change_7d_pct", "change_30d_pct",
		"vol_tier", "volatility_12m_pct",
		"thesis_link", "note",
	})
	for _, h := range holdings {
		_ = cw.Write([]string{
			h.Symbol, h.Name, h.Classification, fmt.Sprintf("%t", h.IsCore),
			deref(h.Wallet), deref(h.CurrentLocation),
			fmt.Sprintf("%.8f", h.QuantityHeld), fmt.Sprintf("%.8f", h.QuantityStaked),
			fmtFloatPtr(h.AvgBuyEUR, 4), fmtFloatPtr(h.CostBasisEUR, 2),
			fmtFloatPtr(h.CurrentPriceEUR, 4), fmtFloatPtr(h.CurrentValueEUR, 2),
			fmtFloatPtr(h.AvgBuyUSD, 4), fmtFloatPtr(h.CostBasisUSD, 2),
			fmtFloatPtr(h.CurrentPriceUSD, 4), fmtFloatPtr(h.CurrentValueUSD, 2),
			fmtFloatPtr(h.RSI14, 2), fmtFloatPtr(h.DailyChangePct, 2),
			fmtFloatPtr(h.Change7dPct, 2), fmtFloatPtr(h.Change30dPct, 2),
			h.VolTier, fmtFloatPtr(h.Volatility12mPct, 2),
			deref(h.ThesisLink), deref(h.Note),
		})
	}
}

func (s *Server) writeWatchlistCSV(w http.ResponseWriter, r *http.Request, userID int64) {
	entries, err := s.store.ListWatchlist(r.Context(), userID)
	if mapStoreError(w, err) {
		return
	}
	setCSVHeaders(w, "watchlist")
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"ticker", "kind", "company_name", "sector",
		"current_price", "target_entry_low", "target_entry_high",
		"forecast_low", "forecast_mean", "forecast_high",
		"added_at", "thesis_link", "note",
	})
	for _, e := range entries {
		_ = cw.Write([]string{
			e.Ticker, e.Kind, deref(e.CompanyName), deref(e.Sector),
			fmtFloatPtr(e.CurrentPrice, 4),
			fmtFloatPtr(e.TargetEntryLow, 4),
			fmtFloatPtr(e.TargetEntryHigh, 4),
			fmtFloatPtr(e.ForecastLow, 4),
			fmtFloatPtr(e.ForecastMean, 4),
			fmtFloatPtr(e.ForecastHigh, 4),
			e.AddedAt.UTC().Format(time.RFC3339),
			deref(e.ThesisLink), deref(e.Note),
		})
	}
}

// ----- helpers -----------------------------------------------------------

func fmtFloatPtr(p *float64, decimals int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%.*f", decimals, *p)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
