// Package macroregime implements Spec 9p — the Macro Regime layer that sits
// on top of the existing Sector Rotation tab. It fetches a set of FRED
// series (reusing cryptoindicators/providers.FREDClient), frames each as a
// rate-of-change reading, and classifies a deterministic growth × inflation
// quadrant with augmenting state + confidence (§A). P2 adds the curated
// playbook doctrine (§F) and the suggest-a-Jordi-regime read (D8).
//
// Source of truth: macro_indicators / macro_indicator_snapshots /
// macro_regime_history / regime_playbook / ism_manual (migration 0037).
package macroregime

// Axis / group constants for SeriesDef.
const (
	AxisGrowth    = "growth"
	AxisInflation = "inflation"
	AxisAugment   = "augment"
)

// SeriesDef describes one FRED-backed macro indicator.
type SeriesDef struct {
	ID         string // our stable key
	FREDID     string // FRED series_id
	Name       string
	Axis       string // AxisGrowth | AxisInflation | AxisAugment
	Group      string // regional_fed | employment | output | cpi | rates | liquidity | curve | credit | dollar
	AroundZero bool   // diffusion indices / spreads oscillating near zero → absolute trend
	Invert     bool   // rising value is growth-NEGATIVE (UNRATE, ICSA)
	YoY        bool   // compute YoY + RoC-of-YoY from history (CPI)
}

// Series is the canonical 9p indicator set (§A). Missing/obscure series fail
// gracefully — the classifier averages whatever is available.
var Series = []SeriesDef{
	// --- Growth axis ---
	{ID: "empire", FREDID: "GACDISA066MSFRBNY", Name: "Empire State Mfg (NY Fed)", Axis: AxisGrowth, Group: "regional_fed", AroundZero: true},
	{ID: "philly", FREDID: "GACDFSA066MSFRBPHIL", Name: "Philadelphia Fed Mfg", Axis: AxisGrowth, Group: "regional_fed", AroundZero: true},
	{ID: "dallas", FREDID: "BACTSAMFRBDAL", Name: "Dallas Fed Mfg (Gen. Activity)", Axis: AxisGrowth, Group: "regional_fed", AroundZero: true},
	{ID: "indpro", FREDID: "INDPRO", Name: "Industrial Production", Axis: AxisGrowth, Group: "output"},
	{ID: "unrate", FREDID: "UNRATE", Name: "Unemployment Rate", Axis: AxisGrowth, Group: "employment", AroundZero: true, Invert: true},
	{ID: "claims", FREDID: "ICSA", Name: "Initial Jobless Claims", Axis: AxisGrowth, Group: "employment", Invert: true},
	// --- Inflation axis ---
	{ID: "cpi_headline", FREDID: "CPIAUCSL", Name: "CPI Headline (YoY)", Axis: AxisInflation, Group: "cpi", YoY: true},
	{ID: "cpi_core", FREDID: "CPILFESL", Name: "CPI Core (YoY)", Axis: AxisInflation, Group: "cpi", YoY: true},
	// --- Augmenting state ---
	{ID: "fedfunds", FREDID: "FEDFUNDS", Name: "Fed Funds Rate", Axis: AxisAugment, Group: "rates"},
	{ID: "m2", FREDID: "WM2NS", Name: "M2 Money Supply", Axis: AxisAugment, Group: "liquidity"},
	{ID: "curve", FREDID: "T10Y2Y", Name: "Yield Curve (10Y-2Y)", Axis: AxisAugment, Group: "curve", AroundZero: true},
	{ID: "credit", FREDID: "BAMLH0A0HYM2", Name: "High-Yield OAS (credit)", Axis: AxisAugment, Group: "credit"},
	{ID: "dxy", FREDID: "DTWEXBGS", Name: "US Dollar (Broad)", Axis: AxisAugment, Group: "dollar"},
}

// SeriesByID indexes Series for lookups.
var SeriesByID = func() map[string]SeriesDef {
	m := make(map[string]SeriesDef, len(Series))
	for _, d := range Series {
		m[d.ID] = d
	}
	return m
}()

// Indicator is one persisted macro_indicators row (with optional sparkline).
type Indicator struct {
	SeriesID   string    `json:"seriesId"`
	FREDID     string    `json:"fredId"`
	Name       string    `json:"name"`
	Source     string    `json:"source"`
	Axis       string    `json:"axis"`
	Group      string    `json:"group"`
	Value      *float64  `json:"value"`
	Prior      *float64  `json:"prior"`
	RoC        *float64  `json:"roc"`
	Direction  string    `json:"direction"` // up|down|flat
	AsOf       string    `json:"asOf"`
	FetchError string    `json:"fetchError,omitempty"`
	UpdatedAt  int64     `json:"updatedAt"`
	History    []float64 `json:"history,omitempty"` // recent snapshot values, oldest→newest (sparkline)
}

// RegimeState is one macro_regime_history row (latest = current).
type RegimeState struct {
	Quadrant          string   `json:"quadrant"`  // Q1|Q2|Q3|Q4|unclassified
	Shorthand         string   `json:"shorthand"` // Goldilocks|Reflation|Stagflation|Deflation/Recession
	GrowthDir         string   `json:"growthDir"`
	InflationDir      string   `json:"inflationDir"`
	RatesRegime       string   `json:"ratesRegime"`
	LiquidityRegime   string   `json:"liquidityRegime"`
	CurveRegime       string   `json:"curveRegime"`
	CreditRegime      string   `json:"creditRegime"`
	DollarRegime      string   `json:"dollarRegime"`
	Confidence        string   `json:"confidence"`
	ThematicFlags     []string `json:"thematicFlags"`
	GrowthMomentum    float64  `json:"growthMomentum"`
	InflationMomentum float64  `json:"inflationMomentum"`
	SuggestedJordi    string   `json:"suggestedJordi"`
	ComputedAt        int64    `json:"computedAt"`
}

// PlaybookRow is one regime_playbook doctrine row.
type PlaybookRow struct {
	ID            int64  `json:"id"`
	RegimeKey     string `json:"regimeKey"`
	AssetOrSector string `json:"assetOrSector"`
	Stance        string `json:"stance"` // favored|neutral|avoid
	Rationale     string `json:"rationale"`
	Source        string `json:"source"`
	SortOrder     int    `json:"sortOrder"`
}

// ISMStatus reports the manual ISM override + freshness.
type ISMStatus struct {
	Value     *float64 `json:"value"`
	EnteredAt int64    `json:"enteredAt"`
	Fresh     bool     `json:"fresh"` // within ISMStaleDays
}

// ISMStaleDays — manual ISM override falls back to the nowcast after this.
const ISMStaleDays = 35

// shorthandFor maps a quadrant key to its plain-language name.
func shorthandFor(quadrant string) string {
	switch quadrant {
	case "Q1":
		return "Goldilocks"
	case "Q2":
		return "Reflation"
	case "Q3":
		return "Stagflation"
	case "Q4":
		return "Deflation/Recession"
	default:
		return "Unclassified"
	}
}
