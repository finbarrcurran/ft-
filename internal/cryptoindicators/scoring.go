// Package cryptoindicators implements Spec 9e — Crypto Indicators Tab.
//
// Aggregates 12 BTC-relevant indicators across 4 buckets (Cowen / Pal /
// Universal / Sentiment) into a -100..+100 composite score with mapped
// action bands (strong_accumulate → distribute_wait). Indicator
// thresholds and scoring functions live in
// definitions/crypto_indicators.json so the methodology is editable
// post-launch without recompile.
//
// Phase 1 (v1.8.0): schema + scoring engine + composite + tab shell +
//                   F&G migration. No external data fetchers yet —
//                   indicators stay NULL until Phase 2 wires providers.
// Phase 2 (v1.8.1): FRED + DefiLlama + Farside + ISM + Cowen log-band
//                   local fit + daily snapshot cron.
// Phase 3 (v1.8.2): composite gauge SVG + bucket cards + Cowen manual
//                   override + Top/Worst performers table.
package cryptoindicators

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed definitions/crypto_indicators.json
var definitionsFS embed.FS

// IndicatorDef is one entry in definitions/crypto_indicators.json.
type IndicatorDef struct {
	ID          string         `json:"id"`
	Bucket      string         `json:"bucket"`
	DisplayName string         `json:"display_name"`
	Unit        string         `json:"unit"`
	ScoringFn   string         `json:"scoring_fn"` // linear | step | level_and_trend | trend_only
	Thresholds  []Threshold    `json:"thresholds"`
}

// Threshold is one row in an indicator's scoring table. Fields are
// shape-shifted by scoring_fn:
//
//	linear / trend_only: uses Value or TrendMin + Score
//	step:                uses Band + Score
//	level_and_trend:     uses LevelMin/LevelMax + Trend4w + Score
type Threshold struct {
	Value     *float64 `json:"value,omitempty"`
	Band      string   `json:"band,omitempty"`
	LevelMin  *float64 `json:"level_min,omitempty"`
	LevelMax  *float64 `json:"level_max,omitempty"`
	Trend4w   string   `json:"trend_4w,omitempty"` // positive | negative | flat | any
	TrendMin  *float64 `json:"trend_4w_min,omitempty"`
	Score     float64  `json:"score"`
}

// Defs is the parsed list of indicator definitions, loaded once at startup.
var Defs []IndicatorDef

// DefsByID lets the rest of the codebase look up by id quickly.
var DefsByID = map[string]IndicatorDef{}

// init loads the bundled JSON. Panics if malformed — better to fail at
// startup than serve garbage scores.
func init() {
	raw, err := definitionsFS.ReadFile("definitions/crypto_indicators.json")
	if err != nil {
		panic(fmt.Sprintf("cryptoindicators: read definitions: %v", err))
	}
	var wrap struct {
		Indicators []IndicatorDef `json:"indicators"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		panic(fmt.Sprintf("cryptoindicators: parse definitions: %v", err))
	}
	Defs = wrap.Indicators
	for _, d := range Defs {
		DefsByID[d.ID] = d
	}
}

// ScoringInputs bundles the values an indicator might consume. Not all
// indicators use every field — only the relevant ones to the scoring_fn
// matter. Caller fills what's available; missing inputs return (0, false).
type ScoringInputs struct {
	Value   *float64 // for linear / step (via Band) / level_and_trend
	Band    string   // for step
	Trend4w *float64 // for level_and_trend / trend_only — signed % change over 4 weeks
}

// Score returns the indicator's normalised score in [-1, +1] given the
// input. Returns (0, false) if inputs are insufficient for the function
// type. Bucketing of the (false) case is up to the caller (typically:
// indicator marked stale, excluded from composite, weights redistributed).
func (d IndicatorDef) Score(in ScoringInputs) (float64, bool) {
	switch d.ScoringFn {
	case "linear":
		return scoreLinear(d.Thresholds, in.Value)
	case "step":
		return scoreStep(d.Thresholds, in.Band)
	case "level_and_trend":
		return scoreLevelAndTrend(d.Thresholds, in.Value, in.Trend4w)
	case "trend_only":
		return scoreTrendOnly(d.Thresholds, in.Trend4w)
	default:
		return 0, false
	}
}

// scoreLinear: piecewise-linear interpolation between threshold points,
// sorted by Value ascending. Below first → first score; above last →
// last score; in between → linear interp.
func scoreLinear(th []Threshold, v *float64) (float64, bool) {
	if v == nil {
		return 0, false
	}
	pts := make([]Threshold, 0, len(th))
	for _, t := range th {
		if t.Value != nil {
			pts = append(pts, t)
		}
	}
	if len(pts) == 0 {
		return 0, false
	}
	sort.Slice(pts, func(i, j int) bool { return *pts[i].Value < *pts[j].Value })
	if *v <= *pts[0].Value {
		return pts[0].Score, true
	}
	if *v >= *pts[len(pts)-1].Value {
		return pts[len(pts)-1].Score, true
	}
	for i := 1; i < len(pts); i++ {
		lo, hi := pts[i-1], pts[i]
		if *v >= *lo.Value && *v <= *hi.Value {
			span := *hi.Value - *lo.Value
			if span == 0 {
				return lo.Score, true
			}
			t := (*v - *lo.Value) / span
			return lo.Score + t*(hi.Score-lo.Score), true
		}
	}
	return 0, false
}

// scoreStep: exact band match (used for log-band thirds).
func scoreStep(th []Threshold, band string) (float64, bool) {
	if band == "" {
		return 0, false
	}
	for _, t := range th {
		if strings.EqualFold(t.Band, band) {
			return t.Score, true
		}
	}
	return 0, false
}

// scoreLevelAndTrend: picks the first threshold row whose level and
// trend conditions both match. Iteration order is JSON file order, so
// the user controls precedence by listing more-specific rows first.
func scoreLevelAndTrend(th []Threshold, v, trend *float64) (float64, bool) {
	if v == nil {
		return 0, false
	}
	for _, t := range th {
		// Level match.
		if t.LevelMin != nil && *v < *t.LevelMin {
			continue
		}
		if t.LevelMax != nil && *v > *t.LevelMax {
			continue
		}
		// Trend match.
		if !trendMatches(t.Trend4w, trend) {
			continue
		}
		return t.Score, true
	}
	return 0, false
}

// scoreTrendOnly: picks first threshold whose trend_4w_min ≤ trend.
// Rows should be ordered most-restrictive first (highest trend_4w_min).
func scoreTrendOnly(th []Threshold, trend *float64) (float64, bool) {
	if trend == nil {
		return 0, false
	}
	for _, t := range th {
		if t.TrendMin == nil {
			continue
		}
		if *trend >= *t.TrendMin {
			return t.Score, true
		}
	}
	return 0, false
}

// trendMatches reports whether `trend` (signed % change) satisfies the
// named direction. "any" always matches; "flat" = within ±0.5%.
func trendMatches(direction string, trend *float64) bool {
	d := strings.ToLower(direction)
	if d == "" || d == "any" {
		return true
	}
	if trend == nil {
		return false
	}
	switch d {
	case "positive":
		return *trend > 0.5
	case "negative":
		return *trend < -0.5
	case "flat":
		return *trend >= -0.5 && *trend <= 0.5
	default:
		return false
	}
}
