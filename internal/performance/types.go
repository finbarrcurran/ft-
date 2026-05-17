// Package performance owns Spec 9d's retrospective analytics. The flow:
//
//	holdings_audit (Spec 9c trade_snapshot_json on open + close events)
//	    ↓ derive.go scans for unprocessed close events
//	closed_trades  (one append-only row per closed position)
//	    ↓ metrics.go + cohorts.go compute per-cohort aggregates
//	performance_snapshots  (cached for fast UI loads)
//	    ↓ Performance tab handler reads cached rows
//	UI
//
// Pure-function design: every helper takes/returns plain Go types so the
// math is unit-testable without a DB.
package performance

import "time"

// ClosedTrade mirrors the closed_trades schema. Append-only by design;
// corrections supersede instead of UPDATE.
type ClosedTrade struct {
	ID         int64  `json:"id"`
	Ticker     string `json:"ticker"`
	Kind       string `json:"kind"` // "stock" | "crypto"
	HoldingID  int64  `json:"holdingId"`

	// Entry snapshot
	OpenedAt              time.Time `json:"openedAt"`
	SetupType             string    `json:"setupType,omitempty"`
	RegimeEffective       string    `json:"regimeEffective,omitempty"`
	JordiScore            *int      `json:"jordiScore,omitempty"`
	CowenScore            *int      `json:"cowenScore,omitempty"`
	PercocoScore          *int      `json:"percocoScore,omitempty"`
	ATRWeeklyAtEntry      float64   `json:"atrWeeklyAtEntry"`
	VolTierAtEntry        string    `json:"volTierAtEntry,omitempty"`
	Support1AtEntry       float64   `json:"support1AtEntry"`
	Resistance1AtEntry    float64   `json:"resistance1AtEntry"`
	Resistance2AtEntry    float64   `json:"resistance2AtEntry"`
	EntryPrice            float64   `json:"entryPrice"`
	SLAtEntry             float64   `json:"slAtEntry"`
	TP1AtEntry            float64   `json:"tp1AtEntry"`
	TP2AtEntry            float64   `json:"tp2AtEntry"`
	RMultipleTP1Planned   float64   `json:"rMultipleTp1Planned"`
	RMultipleTP2Planned   float64   `json:"rMultipleTp2Planned"`
	PositionSizeUnits     float64   `json:"positionSizeUnits"`
	PositionSizeUSD       float64   `json:"positionSizeUsd"`
	PerTradeRiskPct       float64   `json:"perTradeRiskPct"`
	PerTradeRiskUSD       float64   `json:"perTradeRiskUsd"`
	PortfolioValueAtEntry float64   `json:"portfolioValueAtEntry"`

	// Exit
	ClosedAt          time.Time `json:"closedAt"`
	ExitReason        string    `json:"exitReason"`
	ExitPriceAvg      float64   `json:"exitPriceAvg"`
	HoldingPeriodDays int       `json:"holdingPeriodDays"`
	RealizedPnLUSD    float64   `json:"realizedPnlUsd"`
	RealizedPnLPct    float64   `json:"realizedPnlPct"`
	RealizedRMultiple float64   `json:"realizedRMultiple"`

	PostMortem        string  `json:"postMortem,omitempty"`
	SourceAuditOpenID  int64   `json:"sourceAuditOpenId"`
	SourceAuditCloseID int64   `json:"sourceAuditCloseId"`
	DerivedAt          time.Time `json:"derivedAt"`
	SupersededAt       *time.Time `json:"supersededAt,omitempty"`
}

// TradeMetrics is the headline rollup over a cohort. Spec 9d D3.
type TradeMetrics struct {
	Count          int     `json:"count"`
	WinCount       int     `json:"winCount"`
	LossCount      int     `json:"lossCount"`
	WinRate        float64 `json:"winRate"`        // 0..1
	AvgWinnerR     float64 `json:"avgWinnerR"`     // average R-multiple of winning trades
	AvgLoserR      float64 `json:"avgLoserR"`      // average R-multiple of losing trades (negative)
	Expectancy     float64 `json:"expectancy"`     // win_rate × avg_winner_r + (1 − win_rate) × avg_loser_r
	TotalPnLUSD    float64 `json:"totalPnlUsd"`
	AvgHoldDays    float64 `json:"avgHoldDays"`
	MaxDrawdownPct float64 `json:"maxDrawdownPct"` // peak-to-trough within cohort
}

// EquityPoint is one date in the equity curve (Spec 9d D5).
type EquityPoint struct {
	Date              string  `json:"date"`             // 'YYYY-MM-DD'
	PortfolioValue    float64 `json:"portfolioValue"`
	DrawdownFromPeak  float64 `json:"drawdownFromPeak"` // negative or 0
	TradesOpenedToday int     `json:"tradesOpenedToday"`
	TradesClosedToday int     `json:"tradesClosedToday"`
	RealizedR         float64 `json:"realizedR"`        // sum of R captured on trades closed this day
}

// PerformanceSnapshot is one cohort × window aggregate row.
type PerformanceSnapshot struct {
	Date         string  `json:"snapshotDate"`
	CohortKey    string  `json:"cohortKey"`
	Window       string  `json:"window"` // "all" | "365d" | "90d" | "30d"
	Metrics      TradeMetrics `json:"metrics"`
}

// ExitReason values — matches CHECK constraint in migration 0011.
const (
	ExitTP1Hit              = "tp1_hit"
	ExitTP2Hit              = "tp2_hit"
	ExitSLHit               = "sl_hit"
	ExitBounce              = "bounce_close"
	ExitTimeStop            = "time_stop"
	ExitManualClose         = "manual_close"
	ExitThesisChange        = "thesis_change"
	ExitPartialThenRemainder = "partial_then_remainder"
)
