package cryptoindicators

import (
	"context"
	"fmt"
	"ft/internal/cryptoindicators/providers"
	"log/slog"
	"time"
)

// Refresher orchestrates one full sweep across all wired providers, scores
// each indicator via the scoring engine, and upserts the latest reading
// into crypto_indicators. Called from:
//   - POST /api/crypto-indicators/refresh (manual button)
//   - Daily 00:30 UTC cron
//
// Refresher is stateless; safe to construct per call. Provider clients
// are cheap (Go *http.Client with a 20s timeout).
type Refresher struct {
	Service     *Service
	FREDApiKey  string
}

// NewRefresher builds one from a Service handle + FRED key (may be empty;
// FRED-sourced indicators then mark themselves stale).
func NewRefresher(svc *Service, fredKey string) *Refresher {
	return &Refresher{Service: svc, FREDApiKey: fredKey}
}

// RefreshAll fetches all v1.8.2-wired providers in sequence (cheap;
// avoids hammering CoinGecko free-tier rate limit), computes scores via
// the scoring engine, and upserts each crypto_indicators row.
//
// Indicators not yet wired in v1.8.2 (Cowen log-band, price_vs_200wma,
// risk_indicator, Farside ETF flow, DefiLlama stablecoin supply, ISM)
// are skipped silently; v1.8.3 will add them.
func (r *Refresher) RefreshAll(ctx context.Context) error {
	cg := providers.NewCoinGeckoClient()
	fred := providers.NewFREDClient(r.FREDApiKey)

	// Compute trend_4w from snapshot history where the provider can't
	// supply it (CoinGecko, F&G). This requires the snapshot table to
	// have at least 28 days of values — until then trend_4w stays nil
	// for those indicators.
	trendFor := func(id string, current *float64) *float64 {
		if current == nil {
			return nil
		}
		prior, err := r.Service.PriorSnapshotValue(ctx, id, 28)
		if err != nil || prior == nil || *prior == 0 {
			return nil
		}
		t := (*current - *prior) / *prior * 100
		return &t
	}

	type result struct {
		id      string
		reading providers.Reading
	}
	// Each provider call wrapped to also include the indicator id.
	calls := []func() result{
		// FRED (Pal bucket)
		func() result { return result{"pal_dxy", fred.FetchSeries(ctx, "DTWEXBGS")} },
		func() result { return result{"pal_us2y", fred.FetchSeries(ctx, "DGS2")} },
		// CoinGecko (Cowen bucket)
		func() result { return result{"cowen_btc_dominance", cg.FetchBTCDominance(ctx)} },
		func() result { return result{"cowen_eth_btc", cg.FetchETHBTCRatio(ctx)} },
		// Alternative.me (Sentiment bucket)
		func() result { return result{"sentiment_fear_greed", providers.FetchFearGreed(ctx)} },
	}

	var firstErr error
	for _, call := range calls {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		res := call()
		// Backfill trend for providers that don't include it natively.
		if res.reading.Trend4w == nil && res.reading.Value != nil {
			res.reading.Trend4w = trendFor(res.id, res.reading.Value)
		}
		// Score via the scoring engine.
		def, ok := DefsByID[res.id]
		var score *float64
		if ok && res.reading.Err == "" {
			s, valid := def.Score(buildInputsFor(def, res.reading))
			if valid {
				score = &s
			}
		}
		if err := r.Service.UpsertIndicatorReading(ctx, res.id,
			res.reading.Value, score, res.reading.Trend4w, res.reading.Err); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			slog.Error("crypto indicators: upsert", "id", res.id, "err", err)
		}
		// Tiny gap between CoinGecko calls so we don't trip free-tier RL.
		time.Sleep(800 * time.Millisecond)
	}
	return firstErr
}

// buildInputsFor adapts a Reading into the ScoringInputs the scoring
// engine wants. Per Spec 9e §D3 the scoring_fn determines which fields
// are read; we just populate everything we have.
func buildInputsFor(def IndicatorDef, r providers.Reading) ScoringInputs {
	in := ScoringInputs{
		Value:   r.Value,
		Trend4w: r.Trend4w,
	}
	// step-fn indicators (e.g. cowen_log_band) use a band string,
	// which we don't have from any provider in v1.8.2. The log-band
	// fit lands in v1.8.3.
	_ = def
	return in
}

// RefreshAllAndSnapshot runs RefreshAll then writes a daily snapshot
// row. Used by the 00:30 UTC cron. Manual refresh endpoint only calls
// RefreshAll (snapshot is once-daily).
func (r *Refresher) RefreshAllAndSnapshot(ctx context.Context) error {
	if err := r.RefreshAll(ctx); err != nil {
		// Log but proceed — partial data is still worth snapshotting.
		slog.Warn("crypto indicators refresh: partial failure", "err", err)
	}
	// Capture BTC price for the snapshot.
	var btcPrice *float64
	cg := providers.NewCoinGeckoClient()
	if v, err := cg.FetchBTCPriceUSD(ctx); err == nil {
		btcPrice = v
	}
	today := time.Now().UTC().Format("2006-01-02")
	if err := r.Service.WriteDailySnapshot(ctx, today, btcPrice); err != nil {
		return fmt.Errorf("write daily snapshot: %w", err)
	}
	return nil
}
