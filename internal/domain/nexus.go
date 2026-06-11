package domain

// SC-36 AI Nexus — domain types crossing the store/server boundary.
//
// Snapshot rows are keyed (Ticker, AsOf, Source). Source is 'upload' (a Visser
// sheet ingested verbatim) or 'computed' (FT's own daily recompute, W3+).
// Nullable numeric cells use pointers so a blank sheet cell marshals to null
// rather than a misleading 0. Company/Theme/Membership are read-time joins from
// nexus_universe + live holdings/watchlist; they are not stored on the snapshot.

// NexusUniverseRow is a row of nexus_universe (plus read-time membership).
type NexusUniverseRow struct {
	Ticker     string  `json:"ticker"`
	Company    string  `json:"company"`
	Theme      *string `json:"theme"` // NULL for non-nexus members
	IsNexus    bool    `json:"isNexus"`
	Active     bool    `json:"active"`
	Membership string  `json:"membership,omitempty"` // owned|watchlist|nexus|other (read-time)
}

// NexusTechnical is one Trend-Score snapshot row (Signal Sheet, verbatim).
type NexusTechnical struct {
	Ticker     string   `json:"ticker"`
	AsOf       string   `json:"asOf"`   // YYYY-MM-DD
	Source     string   `json:"source"` // upload|computed
	Price      *float64 `json:"price"`
	TrendScore *int     `json:"trendScore"` // 0–100
	SetupLabel *string  `json:"setupLabel"`
	RSI14      *float64 `json:"rsi14"`
	Ret1W      *float64 `json:"ret1w"`
	Ret1M      *float64 `json:"ret1m"`
	Ret3M      *float64 `json:"ret3m"`
	Vs20D      *float64 `json:"vs20d"`
	Vs50D      *float64 `json:"vs50d"`
	Vs200D     *float64 `json:"vs200d"`
	Slope50D   *float64 `json:"slope50d"`
	Slope200D  *float64 `json:"slope200d"`
	Dist52WHi  *float64 `json:"dist52wHi"`
	ATRPct     *float64 `json:"atrPct"`
	VolRatio   *float64 `json:"volRatio"`
	RSSpy      *float64 `json:"rsSpy"`
	RSQqq      *float64 `json:"rsQqq"`
	RSRank     *int     `json:"rsRank"`
	MondayNote *string  `json:"mondayNote"`
	Components string   `json:"-"` // components_json, raw

	// Read-time joins (not stored on the snapshot).
	Company    string  `json:"company,omitempty"`
	Theme      *string `json:"theme,omitempty"`
	Membership string  `json:"membership,omitempty"`
}

// NexusExhaustion is one Exhaustion snapshot row.
type NexusExhaustion struct {
	Ticker       string   `json:"ticker"`
	AsOf         string   `json:"asOf"`
	Source       string   `json:"source"`
	Price        *float64 `json:"price"`
	ExhScore     *float64 `json:"exhScore"` // 1–100
	Band         *string  `json:"band"`     // Extreme|Elevated|Moderate|Low
	RSI14        *float64 `json:"rsi14"`
	RSI5         *float64 `json:"rsi5"`
	WilliamsR    *float64 `json:"williamsR"`
	Pos20D       *float64 `json:"pos20d"`
	Ext20DATR    *float64 `json:"ext20dAtr"`
	Ext50DATR    *float64 `json:"ext50dAtr"`
	RetVol1M     *float64 `json:"retVol1m"`
	Imp5DATR     *float64 `json:"imp5dAtr"`
	VolRatio     *float64 `json:"volRatio"`
	ATRExpansion *float64 `json:"atrExpansion"`
	TDSetup      *int     `json:"tdSetup"`
	TDCountdown  *int     `json:"tdCountdown"`
	TDScore      *float64 `json:"tdScore"`
	ATRPct       *float64 `json:"atrPct"`
	Ret1M        *float64 `json:"ret1m"`
	Ret5D        *float64 `json:"ret5d"`
	DataWtPct    *float64 `json:"dataWtPct"`
	Components   string   `json:"-"`

	Company    string  `json:"company,omitempty"`
	Theme      *string `json:"theme,omitempty"`
	Membership string  `json:"membership,omitempty"`
}

// NexusFundamentals is one Fundamentals snapshot row.
type NexusFundamentals struct {
	Ticker          string   `json:"ticker"`
	AsOf            string   `json:"asOf"`
	Source          string   `json:"source"`
	MarketCap       *float64 `json:"marketCap"`
	FwdPE           *float64 `json:"fwdPe"`
	NextFYEPSGrowth *float64 `json:"nextFyEpsGrowth"` // decimal
	FwdPEG          *float64 `json:"fwdPeg"`
	Price           *float64 `json:"price"`
	CurrentFYEPS    *float64 `json:"currentFyEps"`
	NextFYEPS       *float64 `json:"nextFyEps"`
	CurrentFYEnd    *string  `json:"currentFyEnd"`
	NextFYEnd       *string  `json:"nextFyEnd"`
	DataStatus      *string  `json:"dataStatus"`

	Company    string  `json:"company,omitempty"`
	Theme      *string `json:"theme,omitempty"`
	Membership string  `json:"membership,omitempty"`
}

// NexusIngestResult is what the upload endpoint / CLI ingest returns per file.
type NexusIngestResult struct {
	Kind   string `json:"kind"` // technical|exhaustion|fundamentals
	AsOf   string `json:"asOf"`
	Rows   int    `json:"rows"`
	Source string `json:"source"`
}
