// Spec 9f D2 — daily ETF snapshot ingestion + one-off 14-month backfill.
//
// Reuses the existing Yahoo provider in internal/market (FetchYahooDailyCloses)
// — no new external deps. SPY + VWRL pulled once per run and re-used across
// all 34 sectors so we don't hammer Yahoo with redundant requests.
//
// Idempotent: UNIQUE(sector_universe_id, snapshot_date) on the table; the
// store helper returns nil on conflict.

package sector_rotation

import (
	"context"
	"fmt"
	"ft/internal/market"
	"ft/internal/store"
	"log/slog"
	"time"
)

// IngestResult summarises a daily-ingest run for logging.
type IngestResult struct {
	StartedAt    time.Time
	FinishedAt   time.Time
	SectorsTried int
	SectorsOK    int
	Errors       []string
}

// IngestDaily fetches today's close for every active sector ETF + benchmarks
// and upserts a row per sector. `today` is overridable for tests; pass
// time.Now() in production.
func IngestDaily(ctx context.Context, st *store.Store, today time.Time) *IngestResult {
	r := &IngestResult{StartedAt: time.Now().UTC()}
	defer func() { r.FinishedAt = time.Now().UTC() }()

	sectors, err := st.ListSectorUniverse(ctx)
	if err != nil {
		r.Errors = append(r.Errors, "list sectors: "+err.Error())
		return r
	}

	// Pull SPY (always) + VWRL (best-effort) once for the day.
	spyClose, err := latestClose(ctx, "SPY")
	if err != nil || spyClose == 0 {
		r.Errors = append(r.Errors, "SPY benchmark: "+errStr(err))
		return r
	}
	vwrlClose, _ := latestClose(ctx, "VWRL.L") // London-listed; non-fatal

	dateISO := today.UTC().Format("2006-01-02")

	for _, s := range sectors {
		r.SectorsTried++
		primary, err := latestClose(ctx, s.ETFTickerPrimary)
		if err != nil || primary == 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("%s primary %s: %s", s.Code, s.ETFTickerPrimary, errStr(err)))
			continue
		}
		var secondary *float64
		if s.ETFTickerSecondary != nil && *s.ETFTickerSecondary != "" {
			if v, err := latestClose(ctx, *s.ETFTickerSecondary); err == nil && v > 0 {
				secondary = &v
			}
		}
		var vwrl *float64
		if vwrlClose > 0 {
			v := vwrlClose
			vwrl = &v
		}
		if err := st.InsertSectorSnapshot(ctx, s.ID, dateISO, primary, secondary, spyClose, vwrl, "yahoo"); err != nil {
			r.Errors = append(r.Errors, fmt.Sprintf("%s insert: %s", s.Code, err))
			continue
		}
		r.SectorsOK++
	}
	slog.Info("sector_rotation daily ingest",
		"date", dateISO,
		"ok", r.SectorsOK,
		"tried", r.SectorsTried,
		"errs", len(r.Errors),
		"took", r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond),
	)
	return r
}

// BackfillHistory runs the one-off 14-month seed. Pulls daily closes for
// each ETF + benchmarks, then upserts a snapshot row per (sector, date).
// Idempotent — UNIQUE constraint on the table means re-runs are no-ops.
//
// Sequential per ticker with a 250ms gap to stay polite to Yahoo. 36
// ETFs × ~300 trading days ≈ 11K rows; runs in ~3 min.
func BackfillHistory(ctx context.Context, st *store.Store, months int) *IngestResult {
	r := &IngestResult{StartedAt: time.Now().UTC()}
	defer func() { r.FinishedAt = time.Now().UTC() }()
	if months <= 0 {
		months = 14
	}

	sectors, err := st.ListSectorUniverse(ctx)
	if err != nil {
		r.Errors = append(r.Errors, "list sectors: "+err.Error())
		return r
	}

	// Yahoo daily range strings: "3mo" / "6mo" / "1y" / "2y" / "5y". For
	// 14 months we'll use "2y" and let the storage layer accept anything
	// past 14m (it'll get pruned by the existing price_history cron).
	rng := "2y"
	if months <= 3 {
		rng = "3mo"
	} else if months <= 6 {
		rng = "6mo"
	} else if months <= 12 {
		rng = "1y"
	}

	// Cache closes keyed by (ticker, date).
	spy := fetchDailyMap(ctx, "SPY", rng)
	vwrl := fetchDailyMap(ctx, "VWRL.L", rng)
	if len(spy) == 0 {
		r.Errors = append(r.Errors, "SPY history empty — aborting backfill")
		return r
	}

	for _, s := range sectors {
		r.SectorsTried++
		primary := fetchDailyMap(ctx, s.ETFTickerPrimary, rng)
		if len(primary) == 0 {
			r.Errors = append(r.Errors, fmt.Sprintf("%s primary %s: empty", s.Code, s.ETFTickerPrimary))
			continue
		}
		var secondary map[string]float64
		if s.ETFTickerSecondary != nil && *s.ETFTickerSecondary != "" {
			secondary = fetchDailyMap(ctx, *s.ETFTickerSecondary, rng)
		}
		// Iterate by primary's dates; we anchor on the ETF's calendar.
		for date, p := range primary {
			spyClose, ok := spy[date]
			if !ok {
				continue // benchmark missing → skip the row
			}
			var sec *float64
			if v, ok := secondary[date]; ok {
				sec = &v
			}
			var vw *float64
			if v, ok := vwrl[date]; ok {
				vw = &v
			}
			_ = st.InsertSectorSnapshot(ctx, s.ID, date, p, sec, spyClose, vw, "yahoo")
		}
		r.SectorsOK++
		// 250ms gap between sectors so we don't burst Yahoo.
		select {
		case <-ctx.Done():
			return r
		case <-time.After(250 * time.Millisecond):
		}
	}
	slog.Info("sector_rotation backfill done",
		"sectors_ok", r.SectorsOK,
		"sectors_tried", r.SectorsTried,
		"errs", len(r.Errors),
		"took", r.FinishedAt.Sub(r.StartedAt).Round(time.Second),
	)
	return r
}

// latestClose returns the most recent close for a ticker. Uses Yahoo's
// chart endpoint via the existing market provider.
func latestClose(ctx context.Context, ticker string) (float64, error) {
	closes, err := market.FetchYahooChartCloses(ctx, ticker)
	if err != nil || len(closes) == 0 {
		return 0, err
	}
	return closes[len(closes)-1], nil
}

// fetchDailyMap returns date → close for a ticker over the given range.
// Returns an empty map on error so callers can short-circuit.
func fetchDailyMap(ctx context.Context, ticker, rng string) map[string]float64 {
	out := map[string]float64{}
	closes, err := market.FetchYahooDailyCloses(ctx, ticker, rng)
	if err != nil || len(closes) == 0 {
		return out
	}
	for _, c := range closes {
		out[c.Date] = c.Close
	}
	return out
}

func errStr(err error) string {
	if err == nil {
		return "nil err / zero result"
	}
	return err.Error()
}
