package server

import (
	"net/http"

	"ft/internal/domain"
	"ft/internal/nexus"
)

// GET /api/nexus/entry-candidates?dip=0|1&as_of=YYYY-MM-DD
//
// SC-39 — a read/join/filter/rank over the nightly nexus_* snapshots producing
// one candidates list: intact-trend + not-stretched names, ranked by Fundamentals
// within-theme. A SCREEN, not a signal — no buy language, no sizing, no SL/TP.
// Returns only public universe data (no held/watchlist/thesis fields), so it is
// demo-safe by construction; the client overlays 📌/👁 + Thesis via the shared
// SC-36.3 filter (omitted in demo). The gate/rank logic is unit-tested in
// internal/nexus/entry_test.go.
func (s *Server) handleNexusEntryCandidates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dip := r.URL.Query().Get("dip") == "1"

	// Resolve each engine's snapshot independently (upload fallback, mirroring
	// the per-engine handlers). Exhaustion honours the ?as_of picker.
	techAsOf, techSrc := s.latestNexus(r, "technical", "")
	exhAsOf, exhSrc := s.latestNexus(r, "exhaustion", r.URL.Query().Get("as_of"))
	fundAsOf, fundSrc := s.latestNexus(r, "fundamentals", "")

	tech, err := s.store.ListNexusTechnical(ctx, techAsOf, techSrc)
	if mapStoreError(w, err) {
		return
	}
	exh, err := s.store.ListNexusExhaustion(ctx, exhAsOf, exhSrc)
	if mapStoreError(w, err) {
		return
	}
	fund, err := s.store.ListNexusFundamentals(ctx, fundAsOf, fundSrc)
	if mapStoreError(w, err) {
		return
	}

	exhByTicker := map[string]domain.NexusExhaustion{}
	for _, e := range exh {
		exhByTicker[e.Ticker] = e
	}
	fundByTicker := map[string]domain.NexusFundamentals{}
	for _, f := range fund {
		fundByTicker[f.Ticker] = f
	}
	uni := s.universeByTicker(r)

	inputs := make([]nexus.EntryInput, 0, len(tech))
	for _, t := range tech {
		in := nexus.EntryInput{
			Ticker:     t.Ticker,
			TrendScore: t.TrendScore,
			SetupLabel: t.SetupLabel,
			Vs50D:      t.Vs50D,
			Vs200D:     t.Vs200D,
			RSI14:      t.RSI14,
		}
		if u, ok := uni[t.Ticker]; ok {
			in.Company = u.Company
			if u.Theme != nil {
				in.Theme = *u.Theme
			}
		}
		if e, ok := exhByTicker[t.Ticker]; ok {
			in.ExhScore = e.ExhScore
			in.Band = e.Band
		}
		if f, ok := fundByTicker[t.Ticker]; ok {
			in.FwdPeg = f.FwdPEG
			in.FwdPe = f.FwdPE
		}
		// W1 context columns — read-time from daily_bars (§3). NULL when a
		// ticker has too few bars; never fabricated.
		if bars, err := s.store.GetDailyBars(ctx, t.Ticker, "stock"); err == nil && len(bars) > 0 {
			closes := make([]float64, len(bars))
			for i, b := range bars {
				closes[i] = b.Close
			}
			in.Change5D = nexus.ChangePct(closes, 5)
			in.Change20D = nexus.ChangePct(closes, 20)
		}
		inputs = append(inputs, in)
	}

	candidates, tally := nexus.ComposeEntryCandidates(inputs, dip)
	writeJSON(w, http.StatusOK, map[string]any{
		"asOf":         techAsOf,
		"exhAsOf":      exhAsOf,
		"dip":          dip,
		"candidates":   candidates,
		"tally":        tally,
		"passedCount":  len(candidates),
		"universeSize": len(tech),
	})
}

// latestNexus resolves the (as_of, source) for one engine. When as_of is given
// it is used verbatim with the resolved source; otherwise the newest snapshot
// for the requested source, falling back to "upload" when the default source is
// empty (same rule as the per-engine handlers).
func (s *Server) latestNexus(r *http.Request, engine, asOf string) (string, string) {
	ctx := r.Context()
	src := nexusSource(r)
	latest := func(source string) string {
		var d string
		switch engine {
		case "technical":
			d, _ = s.store.LatestNexusTechnicalAsOf(ctx, source)
		case "exhaustion":
			d, _ = s.store.LatestNexusExhaustionAsOf(ctx, source)
		case "fundamentals":
			d, _ = s.store.LatestNexusFundamentalsAsOf(ctx, source)
		}
		return d
	}
	if asOf != "" {
		return asOf, src
	}
	d := latest(src)
	if d == "" && !nexusSourceExplicit(r) {
		src = "upload"
		d = latest(src)
	}
	return d, src
}
