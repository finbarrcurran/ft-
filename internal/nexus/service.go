package nexus

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"ft/internal/domain"
	"ft/internal/store"
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

// UploadDates returns the distinct upload as_of dates for a snapshot table.
func (s *Service) UploadDates(ctx context.Context, table string) ([]string, error) {
	return s.st.NexusSnapshotDates(ctx, table, "upload")
}

// Ingest parses one uploaded xlsx and replaces the matching (as_of, 'upload')
// snapshot slice. asOfHint supplies the date for Technical sheets (which carry
// no internal date); Exhaustion/Fundamentals self-date and ignore it.
func (s *Service) Ingest(ctx context.Context, data []byte, asOfHint string) (*domain.NexusIngestResult, error) {
	pf, err := Parse(data, asOfHint)
	if err != nil {
		return nil, err
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
