package nexus

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"ft/internal/domain"
	"ft/internal/market"
	"ft/internal/store"
	"time"
)

//go:embed seed/nexus_universe.json
var universeSeedJSON []byte

// Service owns SC-36 universe seeding + sheet ingestion.
type Service struct {
	st *store.Store
}

// New returns a service tied to the store.
func New(st *store.Store) *Service { return &Service{st: st} }

// SeedIfEmpty seeds the Visser 100 into nexus_universe on first run (is_nexus=1).
// Idempotent — no-op once any nexus member exists.
func (s *Service) SeedIfEmpty(ctx context.Context) error {
	n, err := s.st.CountNexusUniverse(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var seed []struct {
		Ticker  string `json:"ticker"`
		Company string `json:"company"`
		Theme   string `json:"theme"`
	}
	if err := json.Unmarshal(universeSeedJSON, &seed); err != nil {
		return fmt.Errorf("nexus universe seed: %w", err)
	}
	rows := make([]domain.NexusUniverseRow, 0, len(seed))
	for _, r := range seed {
		theme := r.Theme
		rows = append(rows, domain.NexusUniverseRow{
			Ticker: r.Ticker, Company: r.Company, Theme: &theme, IsNexus: true, Active: true,
		})
	}
	return s.st.BulkUpsertNexusUniverse(ctx, rows)
}

// spyCloses returns SPY's date→close map from the benchmark bars.
func (s *Service) spyCloses(ctx context.Context) (map[string]float64, error) {
	bars, err := s.st.GetDailyBars(ctx, "SPY", "benchmark")
	if err != nil {
		return nil, err
	}
	m := make(map[string]float64, len(bars))
	for _, b := range bars {
		m[b.Date] = b.Close
	}
	return m, nil
}

// ComputeResult reports a compute pass.
type ComputeResult struct {
	AsOf     string
	Computed int
	Degraded []string // "TICKER: reason"
}

// ComputeForDate runs both bar-based engines (Trend + Exhaustion) for every
// universe member as of asOf and writes source='computed' snapshot rows. The
// Fundamentals engine is separate (it needs a live Yahoo earningsTrend fetch).
func (s *Service) ComputeForDate(ctx context.Context, asOf string) (*ComputeResult, *ComputeResult, error) {
	uni, err := s.st.ListNexusUniverse(ctx)
	if err != nil {
		return nil, nil, err
	}
	spy, err := s.spyCloses(ctx)
	if err != nil {
		return nil, nil, err
	}
	tech := &ComputeResult{AsOf: asOf}
	exh := &ComputeResult{AsOf: asOf}
	var techRows []domain.NexusTechnical
	var exhRows []domain.NexusExhaustion
	for _, u := range uni {
		bars, berr := s.st.GetDailyBars(ctx, u.Ticker, "stock")
		if berr != nil || len(bars) == 0 {
			tech.Degraded = append(tech.Degraded, u.Ticker+": no bars")
			exh.Degraded = append(exh.Degraded, u.Ticker+": no bars")
			continue
		}
		if t, reason := ComputeTrend(bars, spy, u.Ticker, asOf); t != nil {
			techRows = append(techRows, *t)
		} else {
			tech.Degraded = append(tech.Degraded, u.Ticker+": "+reason)
		}
		if e, reason := ComputeExhaustion(bars, u.Ticker, asOf); e != nil {
			exhRows = append(exhRows, *e)
		} else {
			exh.Degraded = append(exh.Degraded, u.Ticker+": "+reason)
		}
	}
	if err := s.st.ReplaceNexusTechnical(ctx, asOf, "computed", techRows); err != nil {
		return nil, nil, err
	}
	if err := s.st.ReplaceNexusExhaustion(ctx, asOf, "computed", exhRows); err != nil {
		return nil, nil, err
	}
	tech.Computed = len(techRows)
	exh.Computed = len(exhRows)
	return tech, exh, nil
}

// ComputeFundamentals fetches Yahoo earningsTrend per universe member, computes
// Forward PEG, and writes source='computed' rows for asOf. Rows are always
// written (degraded ones carry a non-OK data_status — never dropped). gap paces
// the Yahoo calls.
func (s *Service) ComputeFundamentals(ctx context.Context, asOf string, gap time.Duration) (*ComputeResult, error) {
	uni, err := s.st.ListNexusUniverse(ctx)
	if err != nil {
		return nil, err
	}
	res := &ComputeResult{AsOf: asOf}
	rows := make([]domain.NexusFundamentals, 0, len(uni))
	for _, u := range uni {
		f, ferr := market.FetchYahooFundamentals(ctx, u.Ticker)
		var row *domain.NexusFundamentals
		if ferr != nil {
			row = ComputeFundamentals(u.Ticker, asOf, nil)
			res.Degraded = append(res.Degraded, u.Ticker+": fetch "+ferr.Error())
		} else {
			row = ComputeFundamentals(u.Ticker, asOf, f)
			if row.DataStatus != nil && *row.DataStatus != "OK" {
				res.Degraded = append(res.Degraded, u.Ticker+": "+*row.DataStatus)
			}
		}
		rows = append(rows, *row)
		if gap > 0 {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(gap):
			}
		}
	}
	if err := s.st.ReplaceNexusFundamentals(ctx, asOf, "computed", rows); err != nil {
		return nil, err
	}
	res.Computed = len(rows)
	return res, nil
}

// UploadDates returns the distinct upload as_of dates for a snapshot table.
func (s *Service) UploadDates(ctx context.Context, table string) ([]string, error) {
	return s.st.NexusSnapshotDates(ctx, table, "upload")
}

// RefreshUniverseBars fetches fresh daily OHLC for the universe + SPY/QQQ/SOXX
// benchmarks (freshness-aware skip). Shared by the W4 daily cron.
func (s *Service) RefreshUniverseBars(ctx context.Context, rng string, gap time.Duration, freshCutoff string) (refreshed, skipped, failed int) {
	type job struct{ ticker, kind string }
	jobs := []job{{"SPY", "benchmark"}, {"QQQ", "benchmark"}, {"SOXX", "benchmark"}}
	uni, err := s.st.ListNexusUniverse(ctx)
	if err != nil {
		return 0, 0, 0
	}
	for _, u := range uni {
		jobs = append(jobs, job{u.Ticker, "stock"})
	}
	for _, j := range jobs {
		if n, _ := s.st.CountDailyBars(ctx, j.ticker, j.kind); n >= 400 {
			if last, _ := s.st.LastDailyBarDate(ctx, j.ticker, j.kind); last >= freshCutoff {
				skipped++
				continue
			}
		}
		bars, ferr := market.FetchYahooDailyBars(ctx, j.ticker, rng)
		if ferr != nil {
			failed++
		} else {
			rows := make([]store.DailyBarRow, 0, len(bars))
			for _, b := range bars {
				rows = append(rows, store.DailyBarRow{Date: b.Date, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume})
			}
			if serr := s.st.BulkInsertDailyBars(ctx, j.ticker, j.kind, rows); serr != nil {
				failed++
			} else {
				refreshed++
			}
		}
		if gap > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(gap):
			}
		}
	}
	return
}

// NexusDailyResult summarises a daily cron run + any anomalies for monitoring.
type NexusDailyResult struct {
	AsOf       string
	Trend      int
	Exhaustion int
	BarsOK     int
	BarsFailed int
	Anomalies  []string
}

// NexusDaily is the W4 22:30-UTC job: refresh universe bars, compute Trend +
// Exhaustion for today, and surface anomalies for the bot to relay.
func (s *Service) NexusDaily(ctx context.Context) (*NexusDailyResult, error) {
	today := time.Now().UTC().Format("2006-01-02")
	freshCutoff := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	ok, _, failed := s.RefreshUniverseBars(ctx, "1y", 700*time.Millisecond, freshCutoff)
	tech, exh, err := s.ComputeForDate(ctx, today)
	res := &NexusDailyResult{AsOf: today, BarsOK: ok, BarsFailed: failed}
	if err != nil {
		res.Anomalies = append(res.Anomalies, "compute failed: "+err.Error())
		return res, err
	}
	res.Trend, res.Exhaustion = tech.Computed, exh.Computed
	res.Anomalies = s.dailyAnomalies(ctx, today, tech, exh, failed)
	return res, nil
}

// NexusWeeklyFundamentals is the W4 Sunday job: recompute Forward PEG.
func (s *Service) NexusWeeklyFundamentals(ctx context.Context) (*ComputeResult, error) {
	today := time.Now().UTC().Format("2006-01-02")
	return s.ComputeFundamentals(ctx, today, 700*time.Millisecond)
}

// NexusHealthCheck re-derives the persistent anomaly conditions from the latest
// computed snapshot, so the Telegram bot can relay them at poll time even though
// the daily cron ran earlier. Empty slice = all-clear (the bot stays silent).
func (s *Service) NexusHealthCheck(ctx context.Context) ([]string, error) {
	var a []string
	td, _ := s.st.LatestNexusTechnicalAsOf(ctx, "computed")
	if td == "" {
		a = append(a, "no computed Trend snapshot yet")
	} else {
		t, _ := s.st.ListNexusTechnical(ctx, td, "computed")
		if len(t) < 90 {
			a = append(a, fmt.Sprintf("Trend snapshot incomplete: %d rows (%s)", len(t), td))
		}
		var vals []float64
		for _, r := range t {
			if r.TrendScore != nil {
				vals = append(vals, float64(*r.TrendScore))
			}
		}
		if len(vals) >= 10 && pstdev(vals) < 5 {
			a = append(a, "Trend-score distribution collapsed (std-dev < 5) — degenerate input signal")
		}
	}
	ed, _ := s.st.LatestNexusExhaustionAsOf(ctx, "computed")
	if ed == "" {
		a = append(a, "no computed Exhaustion snapshot yet")
	} else {
		e, _ := s.st.ListNexusExhaustion(ctx, ed, "computed")
		low := 0
		for _, r := range e {
			if r.DataWtPct != nil && *r.DataWtPct < 100 {
				low++
			}
		}
		if len(e) > 0 && float64(low)/float64(len(e)) > 0.10 {
			a = append(a, fmt.Sprintf("%d/%d exhaustion rows have incomplete model weight", low, len(e)))
		}
	}
	if last, _ := s.st.LastDailyBarDate(ctx, "SPY", "benchmark"); last != "" {
		if last < time.Now().AddDate(0, 0, -5).Format("2006-01-02") {
			a = append(a, "benchmark bars stale (SPY last "+last+")")
		}
	}
	return a, nil
}

// dailyAnomalies returns anomaly-only health flags (empty when all-clear):
// bar fetch failures, >10% of exhaustion rows with missing model weight, a
// degenerate (collapsed) Trend-score distribution, or a degraded compute.
func (s *Service) dailyAnomalies(ctx context.Context, asOf string, tech, exh *ComputeResult, barsFailed int) []string {
	var a []string
	if barsFailed > 5 {
		a = append(a, fmt.Sprintf("%d universe bar fetches failed", barsFailed))
	}
	if tech.Computed < 90 {
		a = append(a, fmt.Sprintf("only %d Trend rows computed (degraded %d)", tech.Computed, len(tech.Degraded)))
	}
	rows, err := s.st.ListNexusExhaustion(ctx, asOf, "computed")
	if err == nil && len(rows) > 0 {
		lowWt := 0
		for _, r := range rows {
			if r.DataWtPct != nil && *r.DataWtPct < 100 {
				lowWt++
			}
		}
		if float64(lowWt)/float64(len(rows)) > 0.10 {
			a = append(a, fmt.Sprintf("%d/%d exhaustion rows have incomplete model weight", lowWt, len(rows)))
		}
	}
	if t, err := s.st.ListNexusTechnical(ctx, asOf, "computed"); err == nil && len(t) >= 10 {
		var vals []float64
		for _, r := range t {
			if r.TrendScore != nil {
				vals = append(vals, float64(*r.TrendScore))
			}
		}
		if pstdev(vals) < 5 {
			a = append(a, "Trend-score distribution collapsed (std-dev < 5) — degenerate input signal")
		}
	}
	return a
}

// Ingest parses one uploaded xlsx and replaces the matching (as_of, 'upload')
// snapshot slice. asOfHint supplies the date for Technical sheets (which carry
// no internal date); Exhaustion/Fundamentals self-date and ignore it.
func (s *Service) Ingest(ctx context.Context, data []byte, asOfHint string) (*domain.NexusIngestResult, error) {
	pf, err := Parse(data, asOfHint)
	if err != nil {
		return nil, err
	}
	// Apply the source→FT symbol bridge so legacy Visser symbols (e.g. SOTL.NS)
	// land under the canonical universe ticker (STLTECH.NS) and join cleanly
	// with computed rows. Sparse map; no-op when empty.
	if tm, merr := s.st.GetNexusTickerMap(ctx); merr == nil && len(tm) > 0 {
		for i := range pf.Technical {
			if ft, ok := tm[pf.Technical[i].Ticker]; ok {
				pf.Technical[i].Ticker = ft
			}
		}
		for i := range pf.Exhaustion {
			if ft, ok := tm[pf.Exhaustion[i].Ticker]; ok {
				pf.Exhaustion[i].Ticker = ft
			}
		}
		for i := range pf.Fundamentals {
			if ft, ok := tm[pf.Fundamentals[i].Ticker]; ok {
				pf.Fundamentals[i].Ticker = ft
			}
		}
	}
	switch pf.Kind {
	case KindTechnical:
		if err := s.st.ReplaceNexusTechnical(ctx, pf.AsOf, "upload", pf.Technical); err != nil {
			return nil, err
		}
	case KindExhaustion:
		if err := s.st.ReplaceNexusExhaustion(ctx, pf.AsOf, "upload", pf.Exhaustion); err != nil {
			return nil, err
		}
	case KindFundamentals:
		if err := s.st.ReplaceNexusFundamentals(ctx, pf.AsOf, "upload", pf.Fundamentals); err != nil {
			return nil, err
		}
	}
	return &domain.NexusIngestResult{Kind: pf.Kind, AsOf: pf.AsOf, Rows: pf.Rows(), Source: "upload"}, nil
}
