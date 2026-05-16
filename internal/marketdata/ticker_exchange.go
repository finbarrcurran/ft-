package marketdata

import "strings"

// ExchangeForTicker maps a ticker symbol to the exchange code per the
// suffix rules in Spec 5 D2:
//
//	*.L     → LSE
//	*.PA    → EURONEXT
//	*.AS    → EURONEXT (Amsterdam)
//	*.BR    → EURONEXT (Brussels)
//	*.MI    → EURONEXT (Milan)
//	*.LS    → EURONEXT (Lisbon)
//	*.DE    → XETRA
//	*.T     → TSE
//	*.HK    → HKEX
//	*.SA    → B3
//	(no suffix)  → US
//
// Returns "" only for empty input. Unknown suffixes default to "US" so
// the table never has a "no idea" state — the user can override via
// stock_holdings.exchange_override.
func ExchangeForTicker(ticker string) string {
	t := strings.TrimSpace(strings.ToUpper(ticker))
	if t == "" {
		return ""
	}
	if i := strings.LastIndex(t, "."); i != -1 {
		switch t[i+1:] {
		case "L":
			return "LSE"
		case "PA", "AS", "BR", "MI", "LS":
			return "EURONEXT"
		case "DE":
			return "XETRA"
		case "T":
			return "TSE"
		case "HK":
			return "HKEX"
		case "SA":
			return "B3"
		}
	}
	return "US"
}
