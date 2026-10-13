package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

// SC-51 Part A — per-trade statistics from etoro_closed_positions (USD only: the figure that
// reconciles; per-position EUR does not sum to eToro's EUR summary). Interest, refunds, other
// income and dividends live in other tables and are never read here.

// Trade is one closed position as the statistics see it.
type Trade struct {
	AssetType string
	Year      int
	IsCopy    bool
	ProfitUSD float64
	OpenDate  string  // "" = unknown
	CloseDate string  // "" = unknown
	AmountUSD float64 // 0 = unknown
}

// TradeStats are the headline metrics for one set of trades. Pointer fields are nil when the
// metric is undefined for the set (no winners, no losses, no hold data ...), never NaN.
type TradeStats struct {
	N          int      `json:"n"`
	Wins       int      `json:"wins"`
	Losses     int      `json:"losses"`
	BreakEven  int      `json:"breakEven"`
	TotalUSD   float64  `json:"totalUsd"`
	GrossWin   float64  `json:"grossWin"`
	GrossLoss  float64  `json:"grossLoss"` // negative or zero
	WinRate    *float64 `json:"winRate"`   // wins / n (break-even counts in n)
	AvgWinner  *float64 `json:"avgWinner"`
	AvgLoser   *float64 `json:"avgLoser"` // negative
	Payoff     *float64 `json:"payoffRatio"`
	ProfitFact *float64 `json:"profitFactor"`
	Expectancy *float64 `json:"expectancy"`
	MedianHold *float64 `json:"medianHoldDays"`
	HoldKnown  int      `json:"holdKnown"` // trades that contributed to MedianHold
	BestTrade  *float64 `json:"largestWinner"`
	WorstTrade *float64 `json:"largestLoser"`
	Top5Share  *float64 `json:"top5WinnersShare"` // sum(5 largest winners) / total P&L; nil when total <= 0
	Small      bool     `json:"smallSample"`      // n < 10
}

// TradeStatsAsset / TradeStatsYear are the two breakdowns.
type TradeStatsAsset struct {
	AssetType string     `json:"assetType"`
	Stats     TradeStats `json:"stats"`
}
type TradeStatsYear struct {
	Year  int        `json:"year"`
	Stats TradeStats `json:"stats"`
}

// TradeStatsResult is the endpoint payload.
type TradeStatsResult struct {
	Scope         string            `json:"scope"`    // discretionary | copy | all
	Year          string            `json:"year"`     // "2026" or "all"
	Currency      string            `json:"currency"` // always USD
	Stats         TradeStats        `json:"stats"`
	ByAsset       []TradeStatsAsset `json:"byAsset"`
	ByYear        []TradeStatsYear  `json:"byYear,omitempty"` // only for year = all
	Years         []int             `json:"years"`            // years that have positions (any scope)
	MinAmountUSD  float64           `json:"minAmountUsd"`
	ExcludedN     int               `json:"excludedN"`   // trades dropped by the small-position filter
	ExcludedUSD   float64           `json:"excludedUsd"` // their P&L (so the reconciliation stays explainable)
	AmountUnknown int               `json:"amountUnknown"`
}

// ComputeTradeStats is pure: it needs no database, so it is tested against a hand-checked set.
func ComputeTradeStats(ts []Trade) TradeStats {
	var s TradeStats
	s.N = len(ts)
	s.Small = s.N < 10
	if s.N == 0 {
		return s
	}
	var wins []float64
	var holds []float64
	for _, t := range ts {
		p := t.ProfitUSD
		s.TotalUSD += p
		switch {
		case p > 0:
			s.Wins++
			s.GrossWin += p
			wins = append(wins, p)
		case p < 0:
			s.Losses++
			s.GrossLoss += p
		default:
			s.BreakEven++
		}
		if s.BestTrade == nil || p > *s.BestTrade {
			v := p
			s.BestTrade = &v
		}
		if s.WorstTrade == nil || p < *s.WorstTrade {
			v := p
			s.WorstTrade = &v
		}
		if d, ok := holdDays(t.OpenDate, t.CloseDate); ok {
			holds = append(holds, d)
		}
	}
	n := float64(s.N)
	s.WinRate = fptr(float64(s.Wins) / n)
	s.Expectancy = fptr(s.TotalUSD / n)
	if s.Wins > 0 {
		s.AvgWinner = fptr(s.GrossWin / float64(s.Wins))
	}
	if s.Losses > 0 {
		s.AvgLoser = fptr(s.GrossLoss / float64(s.Losses))
	}
	if s.AvgWinner != nil && s.AvgLoser != nil {
		s.Payoff = fptr(*s.AvgWinner / math.Abs(*s.AvgLoser))
	}
	if s.GrossLoss < 0 {
		s.ProfitFact = fptr(s.GrossWin / math.Abs(s.GrossLoss))
	}
	if len(wins) > 0 && s.TotalUSD > 0 {
		sort.Sort(sort.Reverse(sort.Float64Slice(wins)))
		top := 0.0
		for i := 0; i < len(wins) && i < 5; i++ {
			top += wins[i]
		}
		s.Top5Share = fptr(top / s.TotalUSD)
	}
	if len(holds) > 0 {
		sort.Float64s(holds)
		m := len(holds) / 2
		med := holds[m]
		if len(holds)%2 == 0 {
			med = (holds[m-1] + holds[m]) / 2
		}
		s.MedianHold = fptr(med)
		s.HoldKnown = len(holds)
	}
	return s
}

func fptr(f float64) *float64 { return &f }

// holdDays is the calendar-day difference between two YYYY-MM-DD dates (same day = 0).
func holdDays(open, closeD string) (float64, bool) {
	if open == "" || closeD == "" {
		return 0, false
	}
	o, err1 := time.Parse("2006-01-02", open)
	c, err2 := time.Parse("2006-01-02", closeD)
	if err1 != nil || err2 != nil || c.Before(o) {
		return 0, false
	}
	return c.Sub(o).Hours() / 24, true
}

// ScopeMatches mirrors the performance tables' classification exactly (rederiveYear):
// discretionary = is_copy 0 (any asset type, CFDs included); copy = is_copy 1.
func scopeMatches(scope string, t Trade) bool {
	switch scope {
	case "copy":
		return t.IsCopy
	case "all":
		return true
	default:
		return !t.IsCopy
	}
}

// BuildTradeStats filters and assembles the result from raw trades (pure, testable).
func BuildTradeStats(all []Trade, scope, year string, minAmount float64) TradeStatsResult {
	if scope != "copy" && scope != "all" {
		scope = "discretionary"
	}
	res := TradeStatsResult{Scope: scope, Year: year, Currency: "USD", MinAmountUSD: minAmount, ByAsset: []TradeStatsAsset{}, Years: []int{}}
	seenYear := map[int]bool{}
	for _, t := range all {
		if !seenYear[t.Year] {
			seenYear[t.Year] = true
			res.Years = append(res.Years, t.Year)
		}
	}
	sort.Ints(res.Years)

	var sel []Trade
	for _, t := range all {
		if !scopeMatches(scope, t) {
			continue
		}
		if year != "all" && strconv.Itoa(t.Year) != year {
			continue
		}
		if t.AmountUSD == 0 {
			res.AmountUnknown++
		} else if minAmount > 0 && t.AmountUSD < minAmount {
			res.ExcludedN++
			res.ExcludedUSD += t.ProfitUSD
			continue
		}
		sel = append(sel, t)
	}
	res.Stats = ComputeTradeStats(sel)

	byAsset := map[string][]Trade{}
	byYear := map[int][]Trade{}
	for _, t := range sel {
		byAsset[t.AssetType] = append(byAsset[t.AssetType], t)
		byYear[t.Year] = append(byYear[t.Year], t)
	}
	for _, a := range []string{"Stocks", "ETFs", "Crypto", "CFDs"} {
		if ts, ok := byAsset[a]; ok {
			res.ByAsset = append(res.ByAsset, TradeStatsAsset{AssetType: a, Stats: ComputeTradeStats(ts)})
			delete(byAsset, a)
		}
	}
	rest := make([]string, 0, len(byAsset))
	for a := range byAsset {
		rest = append(rest, a)
	}
	sort.Strings(rest)
	for _, a := range rest {
		res.ByAsset = append(res.ByAsset, TradeStatsAsset{AssetType: a, Stats: ComputeTradeStats(byAsset[a])})
	}
	if year == "all" {
		ys := make([]int, 0, len(byYear))
		for y := range byYear {
			ys = append(ys, y)
		}
		sort.Ints(ys)
		for _, y := range ys {
			res.ByYear = append(res.ByYear, TradeStatsYear{Year: y, Stats: ComputeTradeStats(byYear[y])})
		}
	}
	return res
}

// EtoroTradeStats reads etoro_closed_positions only.
func (s *Store) EtoroTradeStats(ctx context.Context, userID int64, scope, year string, minAmount float64) (TradeStatsResult, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT asset_type, year, is_copy, profit_usd, COALESCE(open_date,''), COALESCE(close_date,''), COALESCE(amount_usd,0)
		FROM etoro_closed_positions WHERE user_id = ?`, userID)
	if err != nil {
		return TradeStatsResult{}, fmt.Errorf("trade stats: %w", err)
	}
	defer rows.Close()
	var all []Trade
	for rows.Next() {
		var t Trade
		var c int
		if err := rows.Scan(&t.AssetType, &t.Year, &c, &t.ProfitUSD, &t.OpenDate, &t.CloseDate, &t.AmountUSD); err != nil {
			return TradeStatsResult{}, err
		}
		t.IsCopy = c == 1
		all = append(all, t)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return TradeStatsResult{}, err
	}
	return BuildTradeStats(all, scope, year, minAmount), nil
}
