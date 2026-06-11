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
