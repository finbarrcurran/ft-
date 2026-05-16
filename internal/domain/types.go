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

	// Spec 3 extensions
	Note           *string    `json:"note"`
	Beta           *float64   `json:"beta"`
	EarningsDate   *string    `json:"earningsDate"`   // ISO YYYY-MM-DD, fed by future Yahoo poll
	ExDividendDate *string    `json:"exDividendDate"` // ISO YYYY-MM-DD, fed by future Yahoo poll
	DeletedAt      *time.Time `json:"deletedAt"`      // nil when active; non-nil when soft-deleted

	// Spec 5: optional override for the ticker→exchange suffix rule.
	ExchangeOverride *string `json:"exchangeOverride"`

	// Spec 9c — Percoco execution layer.
	Support1         *float64   `json:"support1,omitempty"`
	Support2         *float64   `json:"support2,omitempty"`
	Resistance1      *float64   `json:"resistance1,omitempty"`
	Resistance2     *float64   `json:"resistance2,omitempty"`
	ATRWeekly        *float64   `json:"atrWeekly,omitempty"`
	VolTierAuto      *string    `json:"volTierAuto,omitempty"`
	SetupType        *string    `json:"setupType,omitempty"`         // 'A_breakout_retest' | 'B_support_bounce' | 'C_continuation'
	Stage            string     `json:"stage"`                       // 'pre_tp1' | 'post_tp1' | 'runner' | 'stopped'
	TP1HitAt         *time.Time `json:"tp1HitAt,omitempty"`
	TP2HitAt         *time.Time `json:"tp2HitAt,omitempty"`
	TimeStopReviewAt *string    `json:"timeStopReviewAt,omitempty"` // ISO YYYY-MM-DD

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

	// Spec 3 extensions
	Note      *string    `json:"note"`
	VolTier   string     `json:"volTier"`         // "low" | "medium" | "high" | "extreme"
	DeletedAt *time.Time `json:"deletedAt"`       // nil when active; non-nil when soft-deleted

	// Spec 9c — Percoco execution layer (same shape as StockHolding).
	Support1         *float64   `json:"support1,omitempty"`
	Support2         *float64   `json:"support2,omitempty"`
	Resistance1      *float64   `json:"resistance1,omitempty"`
	Resistance2      *float64   `json:"resistance2,omitempty"`
	ATRWeekly        *float64   `json:"atrWeekly,omitempty"`
	VolTierAuto      *string    `json:"volTierAuto,omitempty"`
	SetupType        *string    `json:"setupType,omitempty"`
	Stage            string     `json:"stage"`
	TP1HitAt         *time.Time `json:"tp1HitAt,omitempty"`
	TP2HitAt         *time.Time `json:"tp2HitAt,omitempty"`
	TimeStopReviewAt *string    `json:"timeStopReviewAt,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// RegimeRecord is one row of the regime_history table (Spec 9b).
type RegimeRecord struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"ts"`
	FrameworkID string    `json:"frameworkId"` // "jordi" | "cowen"
	Regime      string    `json:"regime"`      // "stable" | "shifting" | "defensive" | "unclassified"
	Source      string    `json:"source"`      // "manual" | "auto_cowen_form"
	InputsJSON  *string   `json:"inputsJson,omitempty"`
	Note        *string   `json:"note,omitempty"`
}

// HoldingsAudit is one row of the audit log table.
type HoldingsAudit struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"ts"`
	UserID      int64     `json:"-"`
	HoldingKind string    `json:"holdingKind"` // "stock" | "crypto"
	HoldingID   int64     `json:"holdingId"`
	Ticker      *string   `json:"ticker,omitempty"`
	Symbol      *string   `json:"symbol,omitempty"`
	Action      string    `json:"action"` // "create" | "update" | "soft_delete" | "restore" | "import_replace"
	Changes     string    `json:"changes"` // raw JSON string of the diff
	Reason      *string   `json:"reason,omitempty"`
	Actor       string    `json:"actor"`
}

// AlertResult is the structured outcome of running alert rules on a holding.
// Triggers carry human-readable reason fragments for tooltips.
type AlertResult struct {
	Status   AlertKind `json:"status"`
	Triggers []string  `json:"triggers"`
}

// WatchlistEntry is a row in the `watchlist` table (Spec 4 D1).
type WatchlistEntry struct {
	ID                 int64      `json:"id"`
	UserID             int64      `json:"-"`
	Ticker             string     `json:"ticker"`
	Kind               string     `json:"kind"` // "stock" | "crypto"
	CompanyName        *string    `json:"companyName"`
	Sector             *string    `json:"sector"`
	CurrentPrice       *float64   `json:"currentPrice"`
	TargetEntryLow     *float64   `json:"targetEntryLow"`
	TargetEntryHigh    *float64   `json:"targetEntryHigh"`
	ThesisLink         *string    `json:"thesisLink"`
	Note               *string    `json:"note"`
	AddedAt            time.Time  `json:"addedAt"`
	PromotedHoldingID  *int64     `json:"promotedHoldingId"`
	DeletedAt          *time.Time `json:"deletedAt"`

	// Spec 9c — levels carry through from watchlist to holding on promote.
	Support1     *float64 `json:"support1,omitempty"`
	Support2     *float64 `json:"support2,omitempty"`
	Resistance1  *float64 `json:"resistance1,omitempty"`
	Resistance2  *float64 `json:"resistance2,omitempty"`
	ATRWeekly    *float64 `json:"atrWeekly,omitempty"`
	VolTierAuto  *string  `json:"volTierAuto,omitempty"`
	SetupType    *string  `json:"setupType,omitempty"`

	UpdatedAt          time.Time  `json:"updatedAt"`
}

// FrameworkScore is a row in the `framework_scores` table. Append-only — every
// re-score creates a new row. Latest = MAX(scored_at) per (target_kind, target_id).
type FrameworkScore struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"-"`
	TargetKind    string    `json:"targetKind"` // "holding" | "watchlist"
	TargetID      int64     `json:"targetId"`
	FrameworkID   string    `json:"frameworkId"`
	ScoredAt      time.Time `json:"scoredAt"`
	TotalScore    int       `json:"totalScore"`
	MaxScore      int       `json:"maxScore"`
	Passes        bool      `json:"passes"`
	ScoresJSON    string    `json:"scoresJson"`         // raw JSON: {qid: {score, note}}
	TagsJSON      *string   `json:"tagsJson,omitempty"` // raw JSON: {tagKey: value}
	ReviewerNote  *string   `json:"reviewerNote,omitempty"`
}
