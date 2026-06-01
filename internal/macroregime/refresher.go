package macroregime

import (
	"context"
	"ft/internal/cryptoindicators/providers"
	"log/slog"
	"strings"
	"time"
)

// fredThrottle is the minimum gap between FRED requests. FRED rate-limits at
// ~120 req/min; spacing each series (some do 2 historical calls) keeps the full
// 13-series sweep — which can collide with the 9e Pal warm-up — under the cap.
const fredThrottle = 600 * time.Millisecond

// Refresher fetches every 9p series from FRED, frames each as a rate-of-change
// reading, upserts the latest row, then re-classifies and persists the regime.
// Reuses cryptoindicators/providers.FREDClient (do not rebuild — §B).
//
// Called from:
//   - POST /api/macro/refresh (manual button)
//   - daily 01:00 UTC cron (RefreshAllAndSnapshot)
type Refresher struct {
	Service    *Service
	FREDApiKey string
}

// NewRefresher builds a Refresher. fredKey may be empty (FRED indicators then
// mark themselves stale via fetch_error and drop out of the classifier).
func NewRefresher(svc *Service, fredKey string) *Refresher {
	return &Refresher{Service: svc, FREDApiKey: fredKey}
}

// RefreshAll fetches all series, upserts readings, classifies + persists regime.
func (r *Refresher) RefreshAll(ctx context.Context) error {
	fred := providers.NewFREDClient(r.FREDApiKey)
	today := time.Now().UTC().Format("2006-01-02")

	for i, d := range Series {
		if i > 0 && !sleepCtx(ctx, fredThrottle) {
			return ctx.Err()
		}
		ind := r.fetchOne(ctx, fred, d, today)
		// One backoff retry on a transient rate-limit (HTTP 429).
		if strings.Contains(ind.FetchError, "429") {
			if !sleepCtx(ctx, 3*time.Second) {
				return ctx.Err()
			}
			ind = r.fetchOne(ctx, fred, d, today)
		}
		if err := r.Service.UpsertIndicator(ctx, ind); err != nil {
			slog.Warn("macroregime: upsert failed", "series", d.ID, "err", err)
		}
	}

	// Classify from the freshly-upserted readings + manual ISM override.
	byID, err := r.Service.ListIndicators(ctx)
	if err != nil {
		return err
	}
	ism, _ := r.Service.LatestISM(ctx)
	st := Classify(byID, ism)
	if _, err := r.Service.WriteRegime(ctx, st); err != nil {
		slog.Warn("macroregime: write regime failed", "err", err)
	}
	return nil
}

// RefreshAllAndSnapshot runs RefreshAll then appends the daily snapshot row.
func (r *Refresher) RefreshAllAndSnapshot(ctx context.Context) error {
	if err := r.RefreshAll(ctx); err != nil {
		return err
	}
	return r.Service.WriteSnapshot(ctx, time.Now().UTC().Format("2006-01-02"))
}

// fetchOne resolves one series into an Indicator, handling the YoY (CPI) and
// 13-week RoC (M2) special cases, falling back to the standard latest+trend.
func (r *Refresher) fetchOne(ctx context.Context, fred *providers.FREDClient, d SeriesDef, asOf string) Indicator {
	ind := Indicator{
		SeriesID: d.ID, FREDID: d.FREDID, Name: d.Name, Source: "FRED",
		Axis: d.Axis, Group: d.Group, AsOf: asOf,
	}

	switch {
	case d.YoY:
		// CPI: compute YoY inflation rate + RoC-of-YoY (pct-points) from history.
		pts, err := fred.FetchHistoricalSeries(ctx, d.FREDID, time.Now().AddDate(0, 0, -500))
		if err != nil || len(pts) < 13 {
			ind.FetchError = errStr(err, "insufficient CPI history")
			return ind
		}
		yoyNow, ok1 := yoyAt(pts, len(pts)-1)
		yoyPrior, ok2 := yoyAt(pts, len(pts)-4) // ~3 monthly prints ago
		if !ok1 {
			ind.FetchError = "could not compute YoY"
			return ind
		}
		ind.Value = &yoyNow
		if ok2 {
			ind.Prior = &yoyPrior
			roc := yoyNow - yoyPrior
			ind.RoC = &roc
		}
		ind.Direction = dirFrom(ind.RoC)
		return ind

	case d.Group == "liquidity":
		// M2: latest level + 13-week (91d) RoC %.
		pts, err := fred.FetchHistoricalSeries(ctx, d.FREDID, time.Now().AddDate(0, 0, -200))
		if err != nil || len(pts) == 0 {
			ind.FetchError = errStr(err, "no M2 history")
			return ind
		}
		latest := pts[len(pts)-1].Value
		ind.Value = &latest
		if prior, ok := valueNDaysBefore(pts, 91); ok && prior != 0 {
			roc := (latest - prior) / prior * 100
			ind.Prior = &prior
			ind.RoC = &roc
		}
		ind.Direction = dirFrom(ind.RoC)
		return ind

	default:
		var rd providers.Reading
		if d.AroundZero {
			rd = fred.FetchSeriesAbsoluteTrend(ctx, d.FREDID)
		} else {
			rd = fred.FetchSeries(ctx, d.FREDID)
		}
		if rd.Err != "" {
			ind.FetchError = rd.Err
		}
		ind.Value = rd.Value
		ind.RoC = rd.Trend4w
		if rd.Value != nil && rd.Trend4w != nil {
			p := *rd.Value - *rd.Trend4w
			if !d.AroundZero && *rd.Trend4w != 0 {
				// for pct trend, recover prior level: v = prior*(1+t/100)
				p = *rd.Value / (1 + *rd.Trend4w/100)
			}
			ind.Prior = &p
		}
		ind.Direction = dirFrom(ind.RoC)
		return ind
	}
}

// yoyAt computes year-over-year % at index i using the observation nearest to
// 365 days before pts[i] (pts sorted ascending by date).
func yoyAt(pts []providers.FREDHistoricalPoint, i int) (float64, bool) {
	if i < 0 || i >= len(pts) {
		return 0, false
	}
	target := pts[i].Date.AddDate(-1, 0, 0)
	// walk back to the observation closest to one year prior
	best := -1
	bestDelta := time.Duration(1<<62 - 1)
	for j := 0; j <= i; j++ {
		dlt := absDur(pts[j].Date.Sub(target))
		if dlt < bestDelta {
			bestDelta = dlt
			best = j
		}
	}
	if best < 0 || pts[best].Value == 0 || bestDelta > 45*24*time.Hour {
		return 0, false
	}
	return (pts[i].Value - pts[best].Value) / pts[best].Value * 100, true
}

// valueNDaysBefore returns the value nearest to n days before the latest point.
func valueNDaysBefore(pts []providers.FREDHistoricalPoint, n int) (float64, bool) {
	if len(pts) == 0 {
		return 0, false
	}
	target := pts[len(pts)-1].Date.AddDate(0, 0, -n)
	best := -1
	bestDelta := time.Duration(1<<62 - 1)
	for j := range pts {
		dlt := absDur(pts[j].Date.Sub(target))
		if dlt < bestDelta {
			bestDelta = dlt
			best = j
		}
	}
	if best < 0 {
		return 0, false
	}
	return pts[best].Value, true
}

// sleepCtx waits d or until ctx is cancelled; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func dirFrom(roc *float64) string {
	if roc == nil {
		return "flat"
	}
	if *roc > 0 {
		return "up"
	}
	if *roc < 0 {
		return "down"
	}
	return "flat"
}

func errStr(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
