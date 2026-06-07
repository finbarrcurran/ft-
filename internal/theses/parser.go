// Package theses implements Spec 15 — Thesis Library.
//
// Pulls locked-thesis markdown files from the user's cross_sector_research
// GitHub repo (cloned locally on jarvis), parses the headers, populates the
// theses_index table, and surfaces a sortable/filterable UI through
// /api/theses endpoints. Also accepts new theses via drag-and-drop and
// commits + pushes them back to GitHub.
package theses

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Header captures the structured metadata parsed from a locked-thesis MD
// file's front-matter prose. Format established by LLY_v1_locked.md and
// formalised across cross_sector_research/theses/.
//
// Example block:
//
//	# LLY — Eli Lilly and Company — Locked Thesis v1
//
//	> **Ticker:** LLY (NYSE)
//	> **Adapter:** Pharma (`metabolic-obesity` sub-type)
//	> **Status:** Locked — 2026-05-18
//	> **Final Score:** 12 / 16 — Strong watchlist conviction (lower bound)
//	> **Personal use only. Not investment advice.**
type Header struct {
	Ticker      string // "LLY"
	CompanyName string // "Eli Lilly and Company"
	Version     int    // 1
	Adapter     string // "pharma" (canonical lowercase, underscore-separated)
	SubType     string // "metabolic-obesity"
	Status      string // "locked" | "draft" | "superseded"
	LockedDate  string // "2026-05-18"
	Score       *int   // pointer so we can distinguish 0 from unparseable
	MaxScore    int    // 16 (default; could be 8 etc.)
}

var (
	// "# LLY — Eli Lilly and Company — Locked Thesis v1"
	// "# LLY — Eli Lilly and Company — Locked Thesis v2"
	reTitle = regexp.MustCompile(`(?m)^#\s+([A-Z0-9\.\-]+)\s+[—\-]\s+(.+?)\s+[—\-]\s+Locked\s+Thesis\s+v(\d+)\s*$`)

	// "> **Ticker:** LLY (NYSE)"   — fallback if title doesn't parse
	reTicker = regexp.MustCompile(`(?m)^>\s*\*\*Ticker:\*\*\s+([A-Z0-9\.\-]+)`)

	// "> **Adapter:** Pharma (`metabolic-obesity` sub-type)"
	// "> **Adapter:** AI-Infra/Semi (sub-type: hyperscaler hardware ODM ...)"
	// "> **Primary Adapter:** Energy-Power Infrastructure (`gen-disp` sub-type)"
	//   ↑ multi-segment names use the "Primary Adapter:" form per the
	//   doctrine note established 2026-05-18 via RR.L.
	// Separate from reFramework so that operating-stock theses which include
	// BOTH a `Framework:` header (declaring 8-Q Operating Stock) AND an
	// `Adapter:` line (carrying the slug) prefer the Adapter row.
	reAdapter = regexp.MustCompile(`(?m)^>\s*\*\*(?:Primary\s+)?Adapter:\*\*\s+([A-Za-z0-9\-\/\s&]+?)(?:\s*\(|$)`)

	// "> **Framework:** Asset-Hedge Scorecard (4-pillar /8)"
	//   ↑ asset-hedge theses (GLD, SLV, etc.) use Framework: not Adapter:
	//   per the Spec 9i three-framework architecture (2026-05-18 via GLD).
	// Used as a fallback only when no Adapter: line is present.
	reFramework = regexp.MustCompile(`(?m)^>\s*\*\*Framework:\*\*\s+([A-Za-z0-9\-\/\s&]+?)(?:\s*\(|$)`)

	// "> **Instrument Type:** Physical-gold-backed ETF — price-tracking ..."
	//   For asset-hedge theses without a backtick sub-type on the
	//   Framework: line, fall back to Instrument Type for the sub-type
	//   column on the Theses tab.
	reInstrumentType = regexp.MustCompile(`(?m)^>\s*\*\*Instrument Type:\*\*\s+([^—\n]+?)(?:\s*[—\-]|$)`)
	// Within the adapter line, optional sub-type in backticks: `metabolic-obesity`
	reSubTypeBacktick = regexp.MustCompile("`([a-z0-9\\-]+)`")
	// Or "sub-type: hyperscaler hardware ODM"
	reSubTypeColon = regexp.MustCompile(`(?i)sub-type[:\s]+([A-Za-z0-9\-\s]+?)(?:[\)\,]|$)`)

	// "> **Status:** Locked — 2026-05-18"
	reStatus = regexp.MustCompile(`(?m)^>\s*\*\*Status:\*\*\s+(Locked|Draft|Superseded)(?:\s*[—\-]\s*(\d{4}-\d{2}-\d{2}))?`)

	// "> **Final Score:** 12 / 16 — Strong watchlist conviction (lower bound)"
	reScore = regexp.MustCompile(`(?m)^>\s*\*\*Final Score:\*\*\s+(\d+)\s*/\s*(\d+)`)
)

// canonical adapter names — folder slugs used in theses/<adapter>/.
// Keep in sync with the directory list in cross_sector_research/theses/.
var adapterAliases = map[string]string{
	"pharma":                             "pharma",
	"ai-infra/semi":                      "ai_infra_semi",
	"ai-infra":                           "ai_infra_semi",
	"ai infra/semi":                      "ai_infra_semi",
	"ai infra semi":                      "ai_infra_semi",
	"hydrocarbons":                       "hydrocarbons",
	"energy-power":                       "energy_power",
	"energy power":                       "energy_power",
	"energy-power infrastructure":        "energy_power",
	"energy power infrastructure":        "energy_power",
	"power-infrastructure":               "energy_power",
	"power infrastructure":               "energy_power",
	"defense":                            "defense",
	"mining-metals":                      "mining_metals",
	"mining & metals":                    "mining_metals",
	"mining and metals":                  "mining_metals",
	"industrial-electrical":              "industrial_electrical",
	"industrial electrical":              "industrial_electrical",
	"industrial-electrical-equipment":    "industrial_electrical_equipment",
	"industrial electrical equipment":    "industrial_electrical_equipment",
	"industrial electrical equipment v1": "industrial_electrical_equipment",
	"cloud-infra":                        "cloud_infra",
	"cloud infra":                        "cloud_infra",
	"ai-frontier-tech":                   "ai_frontier_tech",
	"ai frontier tech":                   "ai_frontier_tech",
	"frontier-tech":                      "ai_frontier_tech",
	"frontier tech":                      "ai_frontier_tech",
	"heavy-machinery":                    "heavy_machinery",
	"heavy machinery":                    "heavy_machinery",
	"heavy-machinery-equipment":          "heavy_machinery",
	"heavy machinery equipment":          "heavy_machinery",
	// Spec 9i — 4-pillar Asset-Hedge framework (GLD, SLV, IAU, future
	// commodity hedge ETFs). Header line uses `Framework:` not `Adapter:`.
	"asset-hedge":           "asset_hedge",
	"asset hedge":           "asset_hedge",
	"asset-hedge scorecard": "asset_hedge",
	"asset hedge scorecard": "asset_hedge",
	"hedge":                 "asset_hedge",
	// Utilities / IPP adapter (17th). Remapped /16, pillars U1–U8.
	// Sub-types (captured free-form, not validated): regulated-utility,
	// ipp-merchant, hybrid-utility, renewables-yieldco.
	"utilities_ipp":               "utilities_ipp",
	"utilities/ipp":               "utilities_ipp",
	"utilities / ipp":             "utilities_ipp",
	"utilities-ipp":               "utilities_ipp",
	"utilities ipp":               "utilities_ipp",
	"utilities":                   "utilities_ipp",
	"utility":                     "utilities_ipp",
	"utilities/ipp v1":            "utilities_ipp",
	"ipp":                         "utilities_ipp",
	"independent power producer":  "utilities_ipp",
	"independent power producers": "utilities_ipp",
	// Financials adapter (18th). Remapped /16, pillars F1–F8.
	// Sub-types (captured free-form, not validated): bank, insurer,
	// asset-manager, exchange-marketinfra, fintech-balancesheet, mreit.
	"financials":    "financials",
	"financial":     "financials",
	"financials v1": "financials",
	"banks":         "financials",
	"insurers":      "financials",
}

// NormaliseAdapter maps a free-form adapter name from the MD header to one
// of the nine canonical folder slugs. Tries three strategies in order:
//
//  1. Exact alias lookup (preferred — every recognised name is in the map)
//  2. Normalised-then-canonical match (handles slash/dash/& variants)
//  3. Keyword fallback — distinctive substrings route to the right folder
//     even when Gemini/Claude phrases the adapter name in a new way.
//
// Returns "" only when none of the three strategies match.
func NormaliseAdapter(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	if v, ok := adapterAliases[k]; ok {
		return v
	}
	// Pass 2: normalise punctuation then check canonical values.
	norm := strings.NewReplacer(" ", "_", "/", "_", "-", "_", "&", "and").Replace(k)
	for _, canon := range adapterAliases {
		if norm == canon {
			return canon
		}
	}
	// Pass 3: keyword-based fallback. Order matters — more-specific
	// patterns come first (e.g. "industrial_electrical" before "energy"
	// because an Industrial Electrical thesis could mention "power").
	keywordRoutes := []struct {
		needles   []string // ALL must appear in k for the route to fire
		canonical string
	}{
		{[]string{"industrial", "electrical", "equipment"}, "industrial_electrical_equipment"}, // MUST come before the contractor route
		{[]string{"industrial", "electrical"}, "industrial_electrical"},
		{[]string{"heavy", "machinery"}, "heavy_machinery"}, // CAT, DE, Oshkosh — diversified-equipment-major sub-type
		{[]string{"frontier"}, "ai_frontier_tech"},          // AI-Frontier-Tech (RGTI quantum-pre-commercial, IonQ, etc.)
		{[]string{"quantum"}, "ai_frontier_tech"},
		{[]string{"fusion"}, "ai_frontier_tech"},
		{[]string{"semi"}, "ai_infra_semi"}, // catches "Semiconductor", "AI-Semi", etc.
		{[]string{"semiconductor"}, "ai_infra_semi"},
		{[]string{"pharma"}, "pharma"},
		{[]string{"defense"}, "defense"},
		{[]string{"defence"}, "defense"}, // UK spelling
		{[]string{"hydrocarbon"}, "hydrocarbons"},
		{[]string{"oil", "gas"}, "hydrocarbons"},
		{[]string{"mining"}, "mining_metals"},
		{[]string{"precious", "metals"}, "mining_metals"},
		{[]string{"cloud"}, "cloud_infra"},
		{[]string{"hyperscaler"}, "cloud_infra"},
		{[]string{"hedge"}, "asset_hedge"},
		{[]string{"asset", "manager"}, "financials"}, // asset/wealth-manager financial sub-type — before bare "asset" hedge route
		{[]string{"asset"}, "asset_hedge"},           // safer to keep last; "hedge" usually present too
		// Utilities / IPP — MUST precede the generic energy/power routes so a
		// utility/IPP thesis mentioning "power" doesn't mis-route to energy_power.
		{[]string{"utilit"}, "utilities_ipp"}, // "utility" / "utilities"
		{[]string{"ipp"}, "utilities_ipp"},
		{[]string{"merchant", "power"}, "utilities_ipp"},
		{[]string{"regulated", "utility"}, "utilities_ipp"},
		// Financials — banks/insurers/exchanges/fintech-balance-sheet/mREITs.
		{[]string{"financial"}, "financials"},
		{[]string{"bank"}, "financials"},
		{[]string{"insurer"}, "financials"},
		{[]string{"insurance"}, "financials"},
		{[]string{"energy"}, "energy_power"},
		{[]string{"power"}, "energy_power"},
	}
	for _, kw := range keywordRoutes {
		matched := true
		for _, n := range kw.needles {
			if !strings.Contains(k, n) {
				matched = false
				break
			}
		}
		if matched {
			return kw.canonical
		}
	}
	return ""
}

// ParseHeader extracts structured metadata from a locked-thesis markdown body.
// Returns a populated Header; fields that fail to parse are left at zero.
// Caller decides whether missing fields are fatal.
func ParseHeader(md string) Header {
	h := Header{MaxScore: 16, Status: "locked"}

	if m := reTitle.FindStringSubmatch(md); len(m) == 4 {
		h.Ticker = strings.TrimSpace(m[1])
		h.CompanyName = strings.TrimSpace(m[2])
		if v, err := strconv.Atoi(m[3]); err == nil {
			h.Version = v
		}
	}
	if h.Ticker == "" {
		if m := reTicker.FindStringSubmatch(md); len(m) == 2 {
			h.Ticker = strings.TrimSpace(m[1])
		}
	}
	if h.Version == 0 {
		h.Version = 1
	}

	if m := reAdapter.FindStringSubmatch(md); len(m) == 2 {
		h.Adapter = NormaliseAdapter(m[1])
	}
	if h.Adapter == "" {
		if m := reFramework.FindStringSubmatch(md); len(m) == 2 {
			h.Adapter = NormaliseAdapter(m[1])
		}
	}
	// Look for sub-type on the adapter line (greedy match the full adapter line first).
	// Try Adapter: first, then Framework: (asset-hedge theses).
	for _, marker := range []string{"**Adapter:**", "**Framework:**"} {
		idx := strings.Index(md, marker)
		if idx < 0 {
			continue
		}
		end := strings.IndexByte(md[idx:], '\n')
		if end <= 0 {
			continue
		}
		line := md[idx : idx+end]
		if m := reSubTypeBacktick.FindStringSubmatch(line); len(m) == 2 {
			h.SubType = m[1]
			break
		} else if m := reSubTypeColon.FindStringSubmatch(line); len(m) == 2 {
			h.SubType = strings.TrimSpace(m[1])
			break
		}
	}
	// Asset-hedge fallback: when no sub-type came from the Framework: line,
	// use the Instrument Type: line so the table column shows something
	// meaningful (e.g. "Physical-gold-backed ETF").
	if h.SubType == "" {
		if m := reInstrumentType.FindStringSubmatch(md); len(m) == 2 {
			h.SubType = strings.TrimSpace(m[1])
		}
	}

	if m := reStatus.FindStringSubmatch(md); len(m) >= 2 {
		h.Status = strings.ToLower(m[1])
		if len(m) >= 3 && m[2] != "" {
			h.LockedDate = m[2]
		}
	}

	if m := reScore.FindStringSubmatch(md); len(m) == 3 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			h.Score = &v
		}
		if v, err := strconv.Atoi(m[2]); err == nil {
			h.MaxScore = v
		}
	}

	return h
}

// Validate reports whether the parsed header has the minimum fields needed
// to insert into theses_index. Returns nil if OK, otherwise a descriptive
// error suitable for surfacing to the user on upload.
func (h Header) Validate() error {
	if h.Ticker == "" {
		return fmt.Errorf("could not parse ticker from MD header (expected '# TICKER — Company — Locked Thesis vN')")
	}
	if h.Adapter == "" {
		return fmt.Errorf("could not parse adapter from MD header (expected '> **Adapter:** <name>' for operating stocks, or '> **Framework:** <name>' for asset hedges); see cross_sector_research/theses/ for valid folder names")
	}
	if h.Version < 1 {
		return fmt.Errorf("version must be ≥ 1 (got %d)", h.Version)
	}
	return nil
}

// CanonicalPath returns the repo-relative path where this thesis lives.
// e.g. "theses/pharma/LLY_v1_locked.md"
func (h Header) CanonicalPath() string {
	return fmt.Sprintf("theses/%s/%s_v%d_locked.md", h.Adapter, h.Ticker, h.Version)
}

// CanonicalGitHubURL builds the user-facing blob URL on github.com.
func (h Header) CanonicalGitHubURL(repoOwner, repoName string) string {
	return fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", repoOwner, repoName, h.CanonicalPath())
}
