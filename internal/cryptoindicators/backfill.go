package cryptoindicators

import (
	"context"
	"encoding/json"
	"fmt"
	"ft/internal/cryptoindicators/providers"
	"log/slog"
	"os"
	"sort"
	"time"
)

// BackfillResult summarises a run of Backfill. Returned to the admin
// endpoint and logged. v1.20.
type BackfillResult struct {
	DaysRequested     int            `json:"daysRequested"`
	DaysWritten       int            `json:"daysWritten"`
	SnapshotsByID     map[string]int `json:"snapshotsByIndicator"`
	CompositesWritten int            `json:"compositesWritten"`
	EarliestDate      string         `json:"earliestDate,omitempty"`
	LatestDate        string         `json:"latestDate,omitempty"`
	Warnings          []string       `json:"warnings,omitempty"`
}

// Backfill computes and writes `days` of historical crypto-indicator
// snapshots + composite snapshots. v1.20.
//
// Sources used:
//
//	FRED — DXY (DTWEXBGS) / US 2Y (DGS2) / CFNAI
//	alternative.me — Fear & Greed Index full history
//	Local btc_price_history — Cowen indicators (log-band, 200wma, risk)
//	Local Playwright Farside JSON — universal_etf_flow_7d
//
// Sources SKIPPED (not historically available on free tiers):
//
//	CoinGecko BTC dominance + ETH/BTC ratio — current value only
//	DefiLlama stablecoin supply — paginating historical aggregates is
//	  brittle on free tier
//
// Idempotent via INSERT OR REPLACE on (snapshot_date, indicator_id) and
// snapshot_date primary keys. Safe to re-run.
func (s *Service) Backfill(ctx context.Context, days int, fredKey string) (*BackfillResult, error) {
	if days < 7 {
		days = 7
	}
	if days > 1825 {
		days = 1825
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -days)
	res := &BackfillResult{
		DaysRequested: days,
		SnapshotsByID: map[string]int{},
	}

	// --- 1. Pull every external source ONCE up-front -------------------

	// FRED
	fred := providers.NewFREDClient(fredKey)
	dxy, err := fred.FetchHistoricalSeries(ctx, "DTWEXBGS", start)
	if err != nil {
		res.Warnings = append(res.Warnings, "DXY FRED: "+err.Error())
	}
	us2y, err := fred.FetchHistoricalSeries(ctx, "DGS2", start)
	if err != nil {
		res.Warnings = append(res.Warnings, "US 2Y FRED: "+err.Error())
	}
	cfnai, err := fred.FetchHistoricalSeries(ctx, "CFNAI", start)
	if err != nil {
		res.Warnings = append(res.Warnings, "CFNAI FRED: "+err.Error())
	}

	// F&G
	fgHist, err := providers.FetchFearGreedHistory(ctx, days+10)
	if err != nil {
		res.Warnings = append(res.Warnings, "F&G alt.me: "+err.Error())
	}

	// Local BTC history
	btc, err := s.BTCHistory(ctx)
	if err != nil {
		return nil, fmt.Errorf("load BTC history: %w", err)
	}

	// Local Farside ETF cache
	farsideDaily := loadFarsideDailyTotals()

	// --- 2. Build date-keyed lookup maps -------------------------------

	dxyByDate := byDateForwardFill(dxy)
	us2yByDate := byDateForwardFill(us2y)
	cfnaiByDate := byDateForwardFill(cfnai)
	fgByDate := fgMap(fgHist)
	btcByDate, btcIndexOnDate := btcMaps(btc)

	// --- 3. Iterate over each historical date and compute snapshots ----

	currentDate := start
	for !currentDate.After(now) {
		dateStr := currentDate.Format("2006-01-02")
		dayWritten := false

		// Pal: DXY
		if v, ok := dxyByDate[dateStr]; ok {
			s.upsertHistoricalSnapshot(ctx, dateStr, "pal_dxy", &v, scoreForLevelAndTrend("pal_dxy", v, nil), res)
			dayWritten = true
		}
		// Pal: US 2Y
		if v, ok := us2yByDate[dateStr]; ok {
			s.upsertHistoricalSnapshot(ctx, dateStr, "pal_us2y", &v, scoreForLevelAndTrend("pal_us2y", v, nil), res)
			dayWritten = true
		}
		// Pal: CFNAI
		if v, ok := cfnaiByDate[dateStr]; ok {
			s.upsertHistoricalSnapshot(ctx, dateStr, "pal_cfnai", &v, scoreLinearFor("pal_cfnai", v), res)
			dayWritten = true
		}
		// Sentiment: F&G
		if v, ok := fgByDate[dateStr]; ok {
			fv := float64(v)
			s.upsertHistoricalSnapshot(ctx, dateStr, "sentiment_fear_greed", &fv, scoreLinearFor("sentiment_fear_greed", fv), res)
			dayWritten = true
		}
		// Cowen indicators — need BTC history UP TO this date.
		if idx, ok := btcIndexOnDate[dateStr]; ok && idx >= 14*7 {
			subHist := btc[:idx+1]
			cw := ComputeCowen(subHist)
			if cw.LogBandValue != nil {
				v := *cw.LogBandValue
				score := scoreStepFor("cowen_log_band", cw.LogBand)
				s.upsertHistoricalSnapshot(ctx, dateStr, "cowen_log_band", &v, score, res)
				dayWritten = true
			}
			if cw.PriceVs200WMA != nil {
				v := *cw.PriceVs200WMA
				s.upsertHistoricalSnapshot(ctx, dateStr, "cowen_price_vs_200wma", &v, scoreLinearFor("cowen_price_vs_200wma", v), res)
				dayWritten = true
			}
			if cw.RiskProxy != nil {
				v := *cw.RiskProxy
				s.upsertHistoricalSnapshot(ctx, dateStr, "cowen_risk_indicator", &v, scoreLinearFor("cowen_risk_indicator", v), res)
				dayWritten = true
			}
		}
		// Universal: ETF flow 7d rolling sum
		if v, ok := computeETFFlow7d(farsideDaily, currentDate); ok {
			s.upsertHistoricalSnapshot(ctx, dateStr, "universal_etf_flow_7d", &v, scoreLinearFor("universal_etf_flow_7d", v), res)
			dayWritten = true
		}
		// --- Composite for this date -----------------------------------
		var btcPrice *float64
		if c, ok := btcByDate[dateStr]; ok {
			btcPrice = &c
		}
		if s.writeHistoricalCompositeForDate(ctx, dateStr, btcPrice) {
			res.CompositesWritten++
		}
		if dayWritten {
			res.DaysWritten++
			if res.EarliestDate == "" || dateStr < res.EarliestDate {
				res.EarliestDate = dateStr
			}
			if dateStr > res.LatestDate {
				res.LatestDate = dateStr
			}
		}
		currentDate = currentDate.AddDate(0, 0, 1)
	}
	slog.Info("crypto indicators: backfill complete",
		"days", res.DaysWritten,
		"composites", res.CompositesWritten,
		"earliest", res.EarliestDate,
		"latest", res.LatestDate,
		"warnings", len(res.Warnings))
	return res, nil
}

// --- helpers ---------------------------------------------------------

func (s *Service) upsertHistoricalSnapshot(ctx context.Context, date, id string, rawValue *float64, score *float64, res *BackfillResult) {
	var rawArg, scoreArg any
	if rawValue != nil {
		rawArg = *rawValue
	}
	if score != nil {
		scoreArg = *score
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT OR REPLACE INTO crypto_indicator_snapshots
		  (snapshot_date, indicator_id, raw_value, score)
		VALUES (?, ?, ?, ?)`, date, id, rawArg, scoreArg)
	if err == nil {
		res.SnapshotsByID[id]++
	}
}

// writeHistoricalCompositeForDate reads back the snapshots for `date`
// from crypto_indicator_snapshots, computes the composite via the
// existing ComputeComposite function, and writes the result. Returns
// true iff a composite row was written.
func (s *Service) writeHistoricalCompositeForDate(ctx context.Context, date string, btcPrice *float64) bool {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT indicator_id, score
		  FROM crypto_indicator_snapshots
		 WHERE snapshot_date = ? AND score IS NOT NULL`, date)
	if err != nil {
		return false
	}
	defer rows.Close()
	active := []ActiveScore{}
	for rows.Next() {
		var id string
		var sc float64
		if err := rows.Scan(&id, &sc); err != nil {
			continue
		}
		bucket := ""
		if d, ok := DefsByID[id]; ok {
			bucket = d.Bucket
		}
		if bucket == "" {
			continue
		}
		active = append(active, ActiveScore{IndicatorID: id, Bucket: bucket, Score: sc})
	}
	if len(active) == 0 {
		return false
	}
	weights, err := s.loadWeights(ctx)
	if err != nil {
		weights = DefaultWeights()
	}
	r := ComputeComposite(active, weights)
	weightsJSON, _ := json.Marshal(r.EffectiveWeights)
	var btcArg, cowArg, palArg, uniArg, senArg any
	if btcPrice != nil {
		btcArg = *btcPrice
	}
	if v, ok := r.SubScores["cowen"]; ok && v != nil {
		cowArg = roundTo(*v, 2)
	}
	if v, ok := r.SubScores["pal"]; ok && v != nil {
		palArg = roundTo(*v, 2)
	}
	if v, ok := r.SubScores["universal"]; ok && v != nil {
		uniArg = roundTo(*v, 2)
	}
	if v, ok := r.SubScores["sentiment"]; ok && v != nil {
		senArg = roundTo(*v, 2)
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT OR REPLACE INTO crypto_composite_snapshots
		  (snapshot_date, composite_score, cowen_subscore, pal_subscore,
		   universal_subscore, sentiment_subscore, action_band,
		   btc_price_usd, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		date, r.Composite, cowArg, palArg, uniArg, senArg, r.ActionBand, btcArg, string(weightsJSON))
	return err == nil
}

// byDateForwardFill takes irregular FRED observations and produces a
// dense map from every calendar day to the most recent observation on
// or before that day. Used so weekly/monthly series (CFNAI) populate
// every daily snapshot, not just the days the underlying series posted.
func byDateForwardFill(pts []providers.FREDHistoricalPoint) map[string]float64 {
	out := map[string]float64{}
	if len(pts) == 0 {
		return out
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Date.Before(pts[j].Date) })
	cur := pts[0].Date
	end := time.Now().UTC()
	idx := 0
	lastV := pts[0].Value
	for !cur.After(end) {
		for idx+1 < len(pts) && !pts[idx+1].Date.After(cur) {
			idx++
			lastV = pts[idx].Value
		}
		out[cur.Format("2006-01-02")] = lastV
		cur = cur.AddDate(0, 0, 1)
	}
	return out
}

func fgMap(pts []providers.FGHistoricalPoint) map[string]int {
	out := map[string]int{}
	for _, p := range pts {
		out[p.Date.Format("2006-01-02")] = p.Value
	}
	return out
}

// btcMaps returns (closesByDate, indexByDate) so we can both look up a
// price by date and slice the history up to that day for Cowen math.
func btcMaps(btc []providers.BTCMarketChartDay) (map[string]float64, map[string]int) {
	byDate := map[string]float64{}
	idxByDate := map[string]int{}
	for i, p := range btc {
		d := p.Date.Format("2006-01-02")
		byDate[d] = p.Close
		idxByDate[d] = i
	}
	return byDate, idxByDate
}

// loadFarsideDailyTotals reads the Playwright-scraped JSON cache and
// returns a date -> daily ETF Total map. Empty map on any failure.
func loadFarsideDailyTotals() map[string]float64 {
	out := map[string]float64{}
	raw, err := os.ReadFile("/var/lib/ft/data/farside/etf-flow.json")
	if err != nil {
		return out
	}
	var cached struct {
		Rows []struct {
			Date   string  `json:"date"`
			TotalM float64 `json:"totalM"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &cached); err != nil {
		return out
	}
	for _, r := range cached.Rows {
		out[r.Date] = r.TotalM
	}
	return out
}

// computeETFFlow7d returns the trailing-7-day rolling sum of ETF Total
// values ending on `date`. Returns (0, false) when fewer than 7 prior
// days exist in the dataset.
func computeETFFlow7d(daily map[string]float64, date time.Time) (float64, bool) {
	if len(daily) < 7 {
		return 0, false
	}
	sum := 0.0
	found := 0
	for d := 0; d < 7; d++ {
		ds := date.AddDate(0, 0, -d).Format("2006-01-02")
		if v, ok := daily[ds]; ok {
			sum += v
			found++
		}
	}
	if found < 4 { // require at least 4 of the 7 days to count
		return 0, false
	}
	return sum, true
}

// scoreLinearFor + friends — apply the scoring engine for a given
// indicator id. Returns nil when the input is insufficient or the
// indicator isn't in DefsByID.
func scoreLinearFor(id string, v float64) *float64 {
	def, ok := DefsByID[id]
	if !ok {
		return nil
	}
	score, ok := def.Score(ScoringInputs{Value: &v})
	if !ok {
		return nil
	}
	return &score
}

func scoreStepFor(id, band string) *float64 {
	def, ok := DefsByID[id]
	if !ok || band == "" {
		return nil
	}
	score, ok := def.Score(ScoringInputs{Band: band})
	if !ok {
		return nil
	}
	return &score
}

// scoreForLevelAndTrend handles indicators that take both level + trend.
// We don't have historical trend at every date during backfill, so we
// pass trend=nil and let the scoring engine pick the "any" or
// trend-less branch. Returns nil if the engine can't score.
func scoreForLevelAndTrend(id string, v float64, trend *float64) *float64 {
	def, ok := DefsByID[id]
	if !ok {
		return nil
	}
	score, ok := def.Score(ScoringInputs{Value: &v, Trend4w: trend})
	if !ok {
		return nil
	}
	return &score
}
