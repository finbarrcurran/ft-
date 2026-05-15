// Package domain holds shared value types that cross the server/store boundary.
//
// Nullable fields use pointer types so they JSON-marshal to `null` when unset,
// matching the TypeScript prototype's `T | null` semantics. Non-pointer string
// fields are always strings, never null.
package domain

import "time"

type User struct {
	ID          int64
	Email       string
	DisplayName string
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

type Session struct {
	Token     string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

type ServiceToken struct {
	ID         int64
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// AlertKind matches the priority order RED > AMBER > GREEN > NEUTRAL from
// the prototype's lib/data-store/alert.ts.
type AlertKind string

const (
	AlertRed     AlertKind = "red"
	AlertAmber   AlertKind = "amber"
	AlertGreen   AlertKind = "green"
	AlertNeutral AlertKind = "neutral"
)

// TechnicalSetup mirrors the prototype's enum in types/index.ts.
// Stored in the DB as the literal string (or empty string for "unset").
type TechnicalSetup string

const (
	TSStrongBuyZone TechnicalSetup = "Strong Buy Zone"
	TSWatchPullback TechnicalSetup = "Watch Pullback"
	TSExtended      TechnicalSetup = "Extended"
	TSWeakSetup     TechnicalSetup = "Weak Setup"
	TSNeutral       TechnicalSetup = "Neutral"
)

// AnalystRRView mirrors the prototype's enum.
type AnalystRRView string

const (
	ARRAttractive   AnalystRRView = "Attractive"
	ARRModerate     AnalystRRView = "Moderate"
	ARRUnfavourable AnalystRRView = "Unfavourable"
	ARRNeutral      AnalystRRView = "Neutral"
)

// CryptoClassification mirrors the prototype's "core" | "alt" union.
type CryptoClassification string

const (
	CryptoCore CryptoClassification = "core"
	CryptoAlt  CryptoClassification = "alt"
)

// StockHolding is the canonical Go representation of a row in stock_holdings.
// JSON shape mirrors the prototype's StockHolding interface in types/index.ts.
type StockHolding struct {
	ID     int64 `json:"id"`
	UserID int64 `json:"-"`

	// User-entered identity / cost basis
	Name         string   `json:"name"`
	Ticker       *string  `json:"ticker"`
	Category     *string  `json:"category"`
	Sector       *string  `json:"sector"`
	InvestedUSD  float64  `json:"investedUsd"`
	AvgOpenPrice *float64 `json:"avgOpenPrice"`
	CurrentPrice *float64 `json:"currentPrice"`

	// Market data
	RSI14         *float64 `json:"rsi14"`
	MA50          *float64 `json:"ma50"`
	MA200         *float64 `json:"ma200"`
	GoldenCross   *bool    `json:"goldenCross"`
	Support       *float64 `json:"support"`
	Resistance    *float64 `json:"resistance"`
	AnalystTarget *float64 `json:"analystTarget"`

	// User opinion / strategy
	ProposedEntry  *float64 `json:"proposedEntry"`
	TechnicalSetup *string  `json:"technicalSetup"`
	AnalystRRView  *string  `json:"analystRrView"`
	StopLoss       *float64 `json:"stopLoss"`
	TakeProfit     *float64 `json:"takeProfit"`
	StrategyNote   string   `json:"strategyNote"`

	// Daily change percent — populated by the market refresh, used by movers.
	DailyChangePct *float64 `json:"dailyChangePct"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// CryptoHolding is the canonical Go representation of a row in crypto_holdings.
type CryptoHolding struct {
	ID     int64 `json:"id"`
	UserID int64 `json:"-"`

	Name           string  `json:"name"`
	Symbol         string  `json:"symbol"`
	Classification string  `json:"classification"` // "core" | "alt"
	IsCore         bool    `json:"isCore"`
	Category       *string `json:"category"`
	Wallet         *string `json:"wallet"`

	QuantityHeld   float64 `json:"quantityHeld"`
	QuantityStaked float64 `json:"quantityStaked"`

	// EUR — source of truth from the user's spreadsheet
	AvgBuyEUR       *float64 `json:"avgBuyEur"`
	CostBasisEUR    *float64 `json:"costBasisEur"`
	CurrentPriceEUR *float64 `json:"currentPriceEur"`
	CurrentValueEUR *float64 `json:"currentValueEur"`

	// USD — derived via snapshot FX
	AvgBuyUSD       *float64 `json:"avgBuyUsd"`
	CostBasisUSD    *float64 `json:"costBasisUsd"`
	CurrentPriceUSD *float64 `json:"currentPriceUsd"`
	CurrentValueUSD *float64 `json:"currentValueUsd"`

	// Optional momentum fields
	RSI14          *float64 `json:"rsi14"`
	DailyChangePct *float64 `json:"dailyChangePct"`
	Change7dPct    *float64 `json:"change7dPct"`
	Change30dPct   *float64 `json:"change30dPct"`

	StrategyNote string    `json:"strategyNote"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// AlertResult is the structured outcome of running alert rules on a holding.
// Triggers carry human-readable reason fragments for tooltips.
type AlertResult struct {
	Status   AlertKind `json:"status"`
	Triggers []string  `json:"triggers"`
}
