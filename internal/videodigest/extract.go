package videodigest

import (
	"context"
	"sort"
	"strings"

	"ft/internal/llm"
)

// SC-52 P3 — extraction on request, with a spend ceiling. Used by `ft video-digest-extract` for the
// backfill: it digests packages that have no digest yet (including transcript-only ones), in the
// order asked, and stops at the ceiling or when the governor refuses.

// ExtractOptions select and bound a run.
type ExtractOptions struct {
	Source       string  // jordi | cowen
	FlagshipOnly bool    // Cowen: only the weekly flagship video
	NewestFirst  bool    // else oldest first
	From, To     string  // published range, inclusive (YYYY-MM-DD, "" = open)
	MaxVideos    int     // 0 = no limit
	MaxCostUSD   float64 // stop once this much has been spent in the run (0 = no limit)
}

// ExtractRun is the outcome.
type ExtractRun struct {
	Results   []Result
	SpentUSD  float64
	Remaining int    // eligible packages left undone
	StoppedBy string // "", "ceiling", "budget", "limit"
}

// Eligible lists the packages a run would digest, in order.
func (s *Service) Eligible(ctx context.Context, o ExtractOptions) ([]pkg, error) {
	all, err := s.packages()
	if err != nil {
		return nil, err
	}
	var out []pkg
	for _, p := range all {
		m := p.Meta
		if m.Source != o.Source {
			continue
		}
		if o.FlagshipOnly && !m.IsFlagship && m.Source != "jordi" {
			continue
		}
		if o.From != "" && m.PublishedAt < o.From || o.To != "" && m.PublishedAt > o.To {
			continue
		}
		id, err := s.existingID(ctx, m.VideoID)
		if err != nil {
			return nil, err
		}
		if id != 0 {
			continue // already digested: never duplicated
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if o.NewestFirst {
			return out[i].Meta.PublishedAt > out[j].Meta.PublishedAt
		}
		return out[i].Meta.PublishedAt < out[j].Meta.PublishedAt
	})
	return out, nil
}

// Extract runs the extraction. report is called after every video.
func (s *Service) Extract(ctx context.Context, o ExtractOptions, report func(Result, float64)) (ExtractRun, error) {
	var run ExtractRun
	todo, err := s.Eligible(ctx, o)
	if err != nil {
		return run, err
	}
	for i, p := range todo {
		if o.MaxVideos > 0 && len(run.Results) >= o.MaxVideos {
			run.StoppedBy, run.Remaining = "limit", len(todo)-i
			return run, nil
		}
		// Keep a margin of one more video under the ceiling.
		if o.MaxCostUSD > 0 && run.SpentUSD+0.06 > o.MaxCostUSD {
			run.StoppedBy, run.Remaining = "ceiling", len(todo)-i
			return run, nil
		}
		r := s.ingest(ctx, p, false)
		run.SpentUSD += r.CostUSD
		run.Results = append(run.Results, r)
		if report != nil {
			report(r, run.SpentUSD)
		}
		if r.Status == "failed" && (strings.Contains(r.Error, llm.ErrBudgetBlocked.Error()) ||
			strings.Contains(r.Error, llm.ErrGloballyPaused.Error()) ||
			strings.Contains(r.Error, llm.ErrFeatureDisabled.Error()) ||
			strings.Contains(r.Error, llm.ErrAPIKeyMissing.Error())) {
			run.StoppedBy, run.Remaining = "budget", len(todo)-i-1
			return run, nil
		}
		if err := ctx.Err(); err != nil {
			run.StoppedBy, run.Remaining = "interrupted", len(todo)-i-1
			return run, nil
		}
	}
	return run, nil
}
