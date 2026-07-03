package nexus

import "sort"

// SC-39 Entry Candidates — a screen, not a signal. Pure gate + ranking logic
// over the three nightly engine snapshots, so every gate boundary is unit-
// tested (AC1). No weights, no composite score, no buy language: it surfaces
// names for the three-gate process (thesis → regime → Percoco).
//
// Rulings (Fin/Fable 2026-07-03):
//   Gate 1 (trend intact) qualifying setup labels = {Strong Uptrend / Buyable,
//     Constructive, Pullback Opportunity}. "Strong but Extended" EXCLUDED.
//   Gate 2 (not stretched) = exhaustion score < 60 (bands Low, Moderate) — the
//     same "stretched" boundary the Summary stretched-holdings card uses.
//   Gate 3 (Dip, optional) = price>MA200 AND MA50>MA200 AND Change_5D<0 AND
//     Wilder RSI in [35,50].

var entryGate1Labels = map[string]bool{
	"Strong Uptrend / Buyable": true,
	"Constructive":             true,
	"Pullback Opportunity":     true,
}

// PassesGate1 — trend intact (setup-label based). Nil/unknown label fails.
func PassesGate1(setupLabel *string) bool {
	return setupLabel != nil && entryGate1Labels[*setupLabel]
}

// PassesGate2 — not stretched: exhaustion strictly below the stretched
// boundary (Elevated, exhScore ≥ 60). Nil score fails (cannot confirm
// not-stretched → exclude, conservative).
func PassesGate2(exhScore *float64) bool {
	return exhScore != nil && *exhScore < 60
}

// PassesDip — optional Gate 3. ALL of: price>MA200, MA50>MA200, Change_5D<0,
// RSI in [35,50]. Derived from the Technical engine's own vs-MA outputs — no
// MA recompute (single source of truth): price>MA200 ⟺ vs200d>0;
// MA50>MA200 ⟺ vs200d>vs50d (price sits further above the lower MA). Any nil
// input fails.
func PassesDip(vs50d, vs200d, change5D, rsi14 *float64) bool {
	if vs50d == nil || vs200d == nil || change5D == nil || rsi14 == nil {
		return false
	}
	priceAboveMA200 := *vs200d > 0
	ma50AboveMA200 := *vs200d > *vs50d
	return priceAboveMA200 && ma50AboveMA200 && *change5D < 0 && *rsi14 >= 35 && *rsi14 <= 50
}

// ChangePct returns the percent change of the last close vs the close `lookback`
// trading days earlier, from an ascending-by-date close series. nil when there
// are too few bars or the base close is non-positive (rendered "—", never
// fabricated).
func ChangePct(closes []float64, lookback int) *float64 {
	if lookback <= 0 || len(closes) < lookback+1 {
		return nil
	}
	last := closes[len(closes)-1]
	base := closes[len(closes)-1-lookback]
	if base <= 0 {
		return nil
	}
	v := (last - base) / base * 100
	return &v
}

// EntryInput is one ticker's already-joined signals (built by the handler from
// the three snapshots + daily_bars). Kept flat so ComposeEntryCandidates is a
// pure function over it.
type EntryInput struct {
	Ticker     string
	Company    string
	Theme      string
	TrendScore *int
	SetupLabel *string
	ExhScore   *float64
	Band       *string
	Vs50D      *float64
	Vs200D     *float64
	RSI14      *float64
	FwdPeg     *float64
	FwdPe      *float64
	Change5D   *float64
	Change20D  *float64
	ThesisStr  string // "" in demo or when unscored
	Membership string // "" in demo
}

// EntryCandidate is one surviving row, ranked.
type EntryCandidate struct {
	Ticker      string   `json:"ticker"`
	Company     string   `json:"company,omitempty"`
	Theme       string   `json:"theme,omitempty"`
	TrendScore  *int     `json:"trendScore"`
	SetupLabel  *string  `json:"setupLabel"`
	ExhScore    *float64 `json:"exhScore"`
	Band        *string  `json:"band"`
	FwdPeg      *float64 `json:"fwdPeg"`
	FwdPe       *float64 `json:"fwdPe"`
	Change5D    *float64 `json:"change5d"`
	Change20D   *float64 `json:"change20d"`
	PriceVsMA50 *float64 `json:"priceVsMa50"`
	ThemeRank   int      `json:"themeRank"`
	ThesisStr   string   `json:"thesisStr,omitempty"`
	Membership  string   `json:"membership,omitempty"`
}

// GateTallyRow is the per-ticker pass/fail used by the empty-state view (AC6).
type GateTallyRow struct {
	Ticker string `json:"ticker"`
	G1     bool   `json:"g1"` // trend intact
	G2     bool   `json:"g2"` // not stretched
	G3     bool   `json:"g3"` // dip (meaningful only when dip requested)
}

// ComposeEntryCandidates applies the gates to every input, ranks the survivors,
// and returns both the ranked candidates and the full per-ticker gate tally
// (so the client can render "nothing qualifies today" as a valid output).
//
// Ranking (ruling D): Fundamentals within-theme rank ascending (cheaper Fwd PEG
// first, nulls last), tie-broken by Technical score descending, then ticker.
func ComposeEntryCandidates(in []EntryInput, dip bool) ([]EntryCandidate, []GateTallyRow) {
	tally := make([]GateTallyRow, 0, len(in))
	survivors := make([]EntryCandidate, 0, len(in))
	for _, x := range in {
		g1 := PassesGate1(x.SetupLabel)
		g2 := PassesGate2(x.ExhScore)
		g3 := PassesDip(x.Vs50D, x.Vs200D, x.Change5D, x.RSI14)
		tally = append(tally, GateTallyRow{Ticker: x.Ticker, G1: g1, G2: g2, G3: g3})
		if !(g1 && g2 && (!dip || g3)) {
			continue
		}
		survivors = append(survivors, EntryCandidate{
			Ticker: x.Ticker, Company: x.Company, Theme: x.Theme,
			TrendScore: x.TrendScore, SetupLabel: x.SetupLabel,
			ExhScore: x.ExhScore, Band: x.Band, FwdPeg: x.FwdPeg, FwdPe: x.FwdPe,
			Change5D: x.Change5D, Change20D: x.Change20D, PriceVsMA50: x.Vs50D,
			ThesisStr: x.ThesisStr, Membership: x.Membership,
		})
	}
	assignThemeRanks(survivors)
	sort.SliceStable(survivors, func(i, j int) bool {
		a, b := survivors[i], survivors[j]
		if a.ThemeRank != b.ThemeRank {
			return a.ThemeRank < b.ThemeRank
		}
		ai, bi := 0, 0
		if a.TrendScore != nil {
			ai = *a.TrendScore
		}
		if b.TrendScore != nil {
			bi = *b.TrendScore
		}
		if ai != bi {
			return ai > bi
		}
		return a.Ticker < b.Ticker
	})
	return survivors, tally
}

// assignThemeRanks sets ThemeRank (1-based) within each theme by Fwd PEG asc,
// nulls last. Mutates the slice in place.
func assignThemeRanks(cands []EntryCandidate) {
	byTheme := map[string][]int{}
	for i := range cands {
		byTheme[cands[i].Theme] = append(byTheme[cands[i].Theme], i)
	}
	for _, idxs := range byTheme {
		sort.SliceStable(idxs, func(a, b int) bool {
			pa, pb := cands[idxs[a]].FwdPeg, cands[idxs[b]].FwdPeg
			if (pa == nil) != (pb == nil) {
				return pa != nil // non-nil (cheaper known) ranks ahead of nil
			}
			if pa != nil && pb != nil && *pa != *pb {
				return *pa < *pb
			}
			return cands[idxs[a]].Ticker < cands[idxs[b]].Ticker
		})
		for rank, ci := range idxs {
			cands[ci].ThemeRank = rank + 1
		}
	}
}
