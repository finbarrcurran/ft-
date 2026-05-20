// CSV import — accepts the same column shape that FT's per-tab CSV export
// produces (v1.5 GET /api/export.csv?tab=stocks|crypto). Round-trip-friendly:
// you can download a tab, edit in Excel/Sheets/LibreOffice, save as CSV,
// re-upload via /api/import/preview.
//
// Detection: the header row of the CSV determines which kind is being
// imported. A single CSV upload only populates ONE of {stocks, crypto}.
// The other side stays untouched in the apply step (existing
// applyStocks / applyCrypto checkbox behaviour in the handler).
//
// Lenient parsing: empty cells = NULL, unknown columns = ignored, numeric
// parsing strips $, €, comma, whitespace before ParseFloat. Same forgiving
// behaviour as the xlsx parser.

package persistence

import (
	"encoding/csv"
	"fmt"
	"ft/internal/domain"
	"io"
	"strings"
)

// ParseCSV reads a CSV (FT export format) and returns an ImportResult.
// Detects stocks vs crypto by the header row.
func ParseCSV(r io.Reader) (*ImportResult, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // allow ragged rows
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true

	out := &ImportResult{
		Stocks:   []*domain.StockHolding{},
		Crypto:   []*domain.CryptoHolding{},
		Skipped:  []SkippedRow{},
		Warnings: []string{},
	}

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 2 {
		out.Warnings = append(out.Warnings, "CSV has no data rows (just header or empty)")
		return out, nil
	}

	// Normalise header row: lowercase, trim, replace dashes with underscores
	// so cosmetic header variations don't break detection.
	header := make([]string, len(records[0]))
	for i, h := range records[0] {
		header[i] = normaliseHeader(h)
	}

	// Build []map[string]string mirroring xlsx rowsAsMaps shape, but keyed
	// by the normalised CSV header (snake_case).
	rows := make([]map[string]string, 0, len(records)-1)
	for _, rec := range records[1:] {
		// Skip blank rows.
		blank := true
		for _, cell := range rec {
			if strings.TrimSpace(cell) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}
		row := make(map[string]string, len(header))
		for i, key := range header {
			if i >= len(rec) {
				continue
			}
			row[key] = rec[i]
		}
		rows = append(rows, row)
	}

	switch detectCSVKind(header) {
	case "stocks":
		out.Stocks = parseStocksFromCSVRows(rows, out)
		out.IsMasterFormatStocks = true // export CSV always includes the rich columns
	case "crypto":
		out.Crypto = parseCryptoFromCSVRows(rows, out)
		out.HasCrypto = len(out.Crypto) > 0
	default:
		return nil, fmt.Errorf("could not identify CSV kind from header (expected 'ticker'+'invested_usd' for stocks, or 'symbol'+'quantity_held' for crypto); see /api/export.csv for the canonical column shape")
	}

	return out, nil
}

func normaliseHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, "-", "_")
	h = strings.ReplaceAll(h, " ", "_")
	return h
}

// detectCSVKind returns "stocks", "crypto", or "" based on header signature.
// Order of precedence: crypto first (its "symbol"+"quantity_held" pair is
// more specific), then stocks ("ticker"+"invested_usd").
func detectCSVKind(header []string) string {
	set := make(map[string]bool, len(header))
	for _, h := range header {
		set[h] = true
	}
	if set["symbol"] && set["quantity_held"] {
		return "crypto"
	}
	if set["ticker"] && set["invested_usd"] {
		return "stocks"
	}
	// Fallback for very minimal CSVs — ticker alone signals stocks.
	if set["ticker"] {
		return "stocks"
	}
	if set["symbol"] {
		return "crypto"
	}
	return ""
}

// parseStocksFromCSVRows builds StockHolding pointers from the normalised
// rows. Mirrors parseStocks (xlsx) but with snake_case keys matching the
// FT export CSV. Anything that fails to parse falls back to nil/zero.
func parseStocksFromCSVRows(rows []map[string]string, out *ImportResult) []*domain.StockHolding {
	stocks := make([]*domain.StockHolding, 0, len(rows))
	for i, row := range rows {
		// Identity: name is required; if absent, fall back to ticker.
		name := pickStr(row, "name", "company_name", "stock_name")
		ticker := pickStr(row, "ticker", "symbol")
		if name == "" && ticker == "" {
			out.Skipped = append(out.Skipped, SkippedRow{
				Reason: "missing both name and ticker",
				Row:    i + 2,
			})
			continue
		}
		if name == "" {
			name = ticker // use ticker as display name fallback
		}
		var tickerPtr *string
		if ticker != "" {
			t := ticker
			tickerPtr = &t
		}

		invested := pickNum(row, "invested_usd", "invested", "invested_$")
		avgOpen := pickNum(row, "avg_open_price", "avg_open", "avgopenprice")
		current := pickNum(row, "current_price", "currentprice")
		if avgOpen == nil && current == nil {
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("%s: imported without price data — fill avg_open_price and current_price to compute P&L.", name))
		}
		invUSD := 0.0
		if invested != nil {
			invUSD = *invested
		}

		h := &domain.StockHolding{
			Name:             name,
			Ticker:           tickerPtr,
			Category:         ptrStrIfSet(pickStr(row, "category")),
			Sector:           ptrStrIfSet(pickStr(row, "sector")),
			Currency:         ptrStrIfSet(pickStr(row, "currency")),
			InvestedUSD:      invUSD,
			AvgOpenPrice:     avgOpen,
			CurrentPrice:     current,
			RSI14:            pickNum(row, "rsi14", "rsi"),
			MA50:             pickNum(row, "ma50"),
			MA200:            pickNum(row, "ma200"),
			Support:          pickNum(row, "support"),
			Resistance:       pickNum(row, "resistance"),
			StopLoss:         pickNum(row, "stop_loss", "stoploss"),
			TakeProfit:       pickNum(row, "take_profit", "takeprofit"),
			AnalystTarget:    pickNum(row, "analyst_target"),
			Beta:             pickNum(row, "beta"),
			Volatility12mPct: pickNum(row, "volatility_12m_pct", "volatility12mpct"),
			EarningsDate:     ptrStrIfSet(pickStr(row, "earnings_date")),
			ExDividendDate:   ptrStrIfSet(pickStr(row, "ex_dividend_date")),
			ThesisLink:       ptrStrIfSet(pickStr(row, "thesis_link")),
			Note:             ptrStrIfSet(pickStr(row, "note")),
		}
		stocks = append(stocks, h)
	}
	return stocks
}

// parseCryptoFromCSVRows mirrors the stocks variant. Schema matches FT's
// per-tab crypto CSV export.
func parseCryptoFromCSVRows(rows []map[string]string, out *ImportResult) []*domain.CryptoHolding {
	crypto := make([]*domain.CryptoHolding, 0, len(rows))
	for i, row := range rows {
		symbol := strings.ToUpper(pickStr(row, "symbol", "ticker"))
		if symbol == "" {
			out.Skipped = append(out.Skipped, SkippedRow{Reason: "missing symbol", Row: i + 2})
			continue
		}
		name := pickStr(row, "name", "company_name", "asset")
		if name == "" {
			name = symbol
		}
		classification := strings.ToLower(pickStr(row, "classification", "class"))
		if classification == "" {
			if symbol == "BTC" || symbol == "ETH" {
				classification = "core"
			} else {
				classification = "alt"
			}
		}
		isCore := classification == "core" ||
			strings.EqualFold(pickStr(row, "is_core"), "true") ||
			pickStr(row, "is_core") == "1"

		volTier := strings.ToLower(pickStr(row, "vol_tier"))
		if volTier == "" {
			volTier = "medium"
		}

		h := &domain.CryptoHolding{
			Name:             name,
			Symbol:           symbol,
			Classification:   classification,
			IsCore:           isCore,
			Wallet:           ptrStrIfSet(pickStr(row, "wallet")),
			CurrentLocation:  ptrStrIfSet(pickStr(row, "current_location")),
			QuantityHeld:     pickNumDefault(row, 0, "quantity_held", "qty_held"),
			QuantityStaked:   pickNumDefault(row, 0, "quantity_staked", "qty_staked"),
			AvgBuyEUR:        pickNum(row, "avg_buy_eur"),
			CostBasisEUR:     pickNum(row, "cost_basis_eur"),
			CurrentPriceEUR:  pickNum(row, "current_price_eur"),
			CurrentValueEUR:  pickNum(row, "current_value_eur"),
			AvgBuyUSD:        pickNum(row, "avg_buy_usd"),
			CostBasisUSD:     pickNum(row, "cost_basis_usd"),
			CurrentPriceUSD:  pickNum(row, "current_price_usd"),
			CurrentValueUSD:  pickNum(row, "current_value_usd"),
			RSI14:            pickNum(row, "rsi14"),
			DailyChangePct:   pickNum(row, "daily_change_pct"),
			Change7dPct:      pickNum(row, "change_7d_pct"),
			Change30dPct:     pickNum(row, "change_30d_pct"),
			VolTier:          volTier,
			Volatility12mPct: pickNum(row, "volatility_12m_pct"),
			Note:             ptrStrIfSet(pickStr(row, "note")),
			ThesisLink:       ptrStrIfSet(pickStr(row, "thesis_link")),
		}
		crypto = append(crypto, h)
	}
	return crypto
}
