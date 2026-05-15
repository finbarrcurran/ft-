// Package heatmap renders the S&P-flavored treemap heatmap as inline SVG.
//
// The tile catalogue here is the same sample data set used by the Next.js
// prototype's lib/data-store/heatmap-data.ts. It's an illustrative snapshot
// of the S&P 500 covering all 11 GICS sectors, sized by approximate market
// cap and colored by mock daily change %. A future iteration will replace
// this with a live batch-quote refresh against Finnhub.
package heatmap

// MarketTile is one ticker's row in the heatmap dataset.
type MarketTile struct {
	Ticker     string
	Name       string
	Sector     string
	MarketCapB float64 // USD billions — drives tile size
	Price      float64 // last close USD
	ChangePct  float64 // daily % change — drives tile color
	VolumeM    float64 // trading volume in millions
	Held       bool    // set per-request from user's holdings
}

// Sectors is the canonical render order for sector boxes.
var Sectors = []string{
	"Technology",
	"Financials",
	"Healthcare",
	"Consumer Discretionary",
	"Consumer Staples",
	"Communication Services",
	"Industrials",
	"Energy",
	"Utilities",
	"Materials",
	"Real Estate",
}

// Tiles is the static heatmap dataset. 86 rows.
var Tiles = []MarketTile{
	// ---- Technology ----
	{"AAPL", "Apple", "Technology", 3450, 228.40, 0.82, 51.2, false},
	{"MSFT", "Microsoft", "Technology", 3120, 419.55, 1.14, 22.8, false},
	{"NVDA", "NVIDIA", "Technology", 3050, 217.48, 2.34, 240.3, false},
	{"AVGO", "Broadcom", "Technology", 960, 412.82, -1.21, 17.5, false},
	{"ORCL", "Oracle", "Technology", 510, 183.95, 1.84, 12.4, false},
	{"ADBE", "Adobe", "Technology", 240, 542.20, -0.43, 3.1, false},
	{"AMD", "AMD", "Technology", 250, 154.30, 1.65, 44.8, false},
	{"CRM", "Salesforce", "Technology", 295, 298.55, 0.34, 6.0, false},
	{"INTC", "Intel", "Technology", 130, 30.21, -2.18, 62.4, false},
	{"TSM", "Taiwan Semi", "Technology", 1010, 388.40, 1.78, 11.2, false},
	{"ASML", "ASML", "Technology", 330, 1481.33, -1.94, 2.5, false},
	{"ARM", "ARM Holdings", "Technology", 220, 203.92, 0.91, 5.8, false},
	{"QCOM", "Qualcomm", "Technology", 185, 168.40, 0.45, 8.7, false},
	{"TXN", "Texas Instruments", "Technology", 185, 202.10, -0.32, 5.0, false},
	{"PLTR", "Palantir", "Technology", 190, 82.50, 3.42, 64.1, false},
	{"LSCC", "Lattice Semi", "Technology", 17, 123.54, -1.05, 1.4, false},
	{"COHR", "Coherent", "Technology", 16, 357.70, -2.41, 1.0, false},

	// ---- Financials ----
	{"JPM", "JPMorgan Chase", "Financials", 720, 251.40, 0.62, 8.2, false},
	{"V", "Visa", "Financials", 640, 340.20, 0.18, 5.1, false},
	{"MA", "Mastercard", "Financials", 500, 558.30, 0.32, 2.4, false},
	{"BAC", "Bank of America", "Financials", 365, 46.75, 0.94, 39.6, false},
	{"WFC", "Wells Fargo", "Financials", 255, 76.20, 1.21, 17.5, false},
	{"GS", "Goldman Sachs", "Financials", 185, 588.40, -0.31, 1.8, false},
	{"MS", "Morgan Stanley", "Financials", 205, 127.55, 0.42, 7.3, false},
	{"AXP", "American Express", "Financials", 220, 305.10, -0.15, 2.6, false},
	{"C", "Citigroup", "Financials", 155, 82.55, 1.08, 12.4, false},
	{"BLK", "BlackRock", "Financials", 170, 1124.40, 0.51, 0.7, false},

	// ---- Healthcare ----
	{"LLY", "Eli Lilly", "Healthcare", 720, 798.55, -0.81, 3.6, false},
	{"JNJ", "Johnson & Johnson", "Healthcare", 370, 154.30, -0.41, 7.5, false},
	{"UNH", "UnitedHealth", "Healthcare", 525, 580.20, 0.22, 3.1, false},
	{"ABBV", "AbbVie", "Healthcare", 340, 192.10, -0.92, 6.2, false},
	{"MRK", "Merck", "Healthcare", 255, 102.40, -1.45, 10.8, false},
	{"PFE", "Pfizer", "Healthcare", 165, 29.10, -0.62, 36.3, false},
	{"TMO", "Thermo Fisher", "Healthcare", 215, 562.80, -0.18, 1.5, false},
	{"ABT", "Abbott", "Healthcare", 205, 117.40, 0.31, 5.4, false},
	{"ISRG", "Intuitive Surgical", "Healthcare", 180, 504.55, 1.42, 1.7, false},

	// ---- Consumer Discretionary ----
	{"AMZN", "Amazon", "Consumer Discretionary", 2150, 205.40, 1.85, 42.1, false},
	{"TSLA", "Tesla", "Consumer Discretionary", 950, 298.20, -2.61, 102.3, false},
	{"HD", "Home Depot", "Consumer Discretionary", 395, 397.50, -0.42, 2.9, false},
	{"MCD", "McDonald's", "Consumer Discretionary", 215, 298.40, 0.18, 2.1, false},
	{"NKE", "Nike", "Consumer Discretionary", 115, 76.55, -1.12, 8.3, false},
	{"SBUX", "Starbucks", "Consumer Discretionary", 115, 100.40, 0.81, 7.1, false},
	{"LOW", "Lowe's", "Consumer Discretionary", 150, 269.80, -0.34, 2.5, false},
	{"BKNG", "Booking Holdings", "Consumer Discretionary", 170, 5102.40, 0.65, 0.3, false},

	// ---- Consumer Staples ----
	{"WMT", "Walmart", "Consumer Staples", 775, 96.55, 0.28, 16.2, false},
	{"PG", "Procter & Gamble", "Consumer Staples", 395, 168.20, -0.15, 5.0, false},
	{"COST", "Costco", "Consumer Staples", 420, 945.30, 0.55, 1.6, false},
	{"KO", "Coca-Cola", "Consumer Staples", 295, 68.40, -0.24, 10.5, false},
	{"PEP", "PepsiCo", "Consumer Staples", 215, 156.40, -0.51, 4.8, false},
	{"PM", "Philip Morris", "Consumer Staples", 215, 138.20, 0.42, 3.5, false},

	// ---- Communication Services ----
	{"GOOGL", "Alphabet", "Communication Services", 2140, 175.20, 0.92, 22.8, false},
	{"META", "Meta Platforms", "Communication Services", 1480, 582.10, 1.32, 12.3, false},
	{"NFLX", "Netflix", "Communication Services", 385, 870.20, 2.15, 3.8, false},
	{"DIS", "Disney", "Communication Services", 170, 94.50, -0.73, 8.7, false},
	{"T", "AT&T", "Communication Services", 165, 23.10, -0.52, 39.4, false},
	{"TMUS", "T-Mobile", "Communication Services", 255, 222.50, 0.41, 3.6, false},
	{"VZ", "Verizon", "Communication Services", 185, 44.20, -0.31, 16.1, false},

	// ---- Industrials ----
	{"CAT", "Caterpillar", "Industrials", 175, 898.23, -0.84, 1.6, false},
	{"BA", "Boeing", "Industrials", 125, 168.40, -1.55, 5.6, false},
	{"GE", "GE Aerospace", "Industrials", 215, 200.40, 0.78, 4.4, false},
	{"HON", "Honeywell", "Industrials", 140, 220.30, -0.21, 3.2, false},
	{"RTX", "RTX", "Industrials", 175, 130.40, 0.12, 4.8, false},
	{"UPS", "UPS", "Industrials", 110, 128.20, -0.92, 3.5, false},
	{"DE", "Deere", "Industrials", 130, 472.40, -0.18, 1.5, false},
	{"LMT", "Lockheed Martin", "Industrials", 130, 542.20, 0.81, 1.1, false},
	{"MOD", "Modine", "Industrials", 9, 268.89, -1.34, 0.5, false},

	// ---- Energy ----
	{"XOM", "ExxonMobil", "Energy", 490, 151.44, -1.42, 15.8, false},
	{"CVX", "Chevron", "Energy", 290, 157.30, -1.18, 9.2, false},
	{"COP", "ConocoPhillips", "Energy", 140, 110.50, -0.85, 7.4, false},
	{"SLB", "Schlumberger", "Energy", 60, 42.10, -2.12, 15.3, false},
	{"EOG", "EOG Resources", "Energy", 70, 122.40, -1.45, 3.8, false},
	{"BE", "Bloom Energy", "Energy", 65, 272.23, -1.82, 1.8, false},

	// ---- Utilities ----
	{"NEE", "NextEra Energy", "Utilities", 165, 80.20, 0.42, 8.5, false},
	{"SO", "Southern Company", "Utilities", 95, 85.30, 0.15, 4.2, false},
	{"DUK", "Duke Energy", "Utilities", 90, 116.20, -0.25, 3.0, false},
	{"AEP", "American Electric", "Utilities", 55, 102.20, 0.18, 2.1, false},

	// ---- Materials ----
	{"LIN", "Linde", "Materials", 225, 470.30, -0.42, 1.7, false},
	{"SHW", "Sherwin-Williams", "Materials", 95, 380.30, -0.34, 1.2, false},
	{"FCX", "Freeport-McMoRan", "Materials", 60, 42.30, 1.12, 19.4, false},
	{"GLD", "SPDR Gold (ETF)", "Materials", 75, 428.60, -0.65, 7.8, false},
	{"AEM", "Agnico Eagle", "Materials", 95, 191.13, 1.24, 3.5, false},
	{"WPM", "Wheaton Precious", "Materials", 65, 138.31, 0.95, 2.1, false},

	// ---- Real Estate ----
	{"PLD", "Prologis", "Real Estate", 100, 108.40, -0.18, 4.8, false},
	{"AMT", "American Tower", "Real Estate", 95, 203.20, 0.32, 1.8, false},
	{"EQIX", "Equinix", "Real Estate", 80, 845.20, 0.74, 0.3, false},
	{"WELL", "Welltower", "Real Estate", 95, 152.30, -0.41, 2.5, false},
}
