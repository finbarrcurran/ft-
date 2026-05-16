// Package sparkline renders an inline SVG mini-line-chart of a close-price
// series. Spec 3 D8 (100×24 table cell) + D9 (220×60 hover popover).
//
// No axes, no labels — pure stroke. Color is green when the last close is
// above the first close, red otherwise.
package sparkline

import (
	"fmt"
	"strings"
)

// Defaults match the spec: 100×24 px for the table column.
const (
	DefaultWidth  = 100
	DefaultHeight = 24
	LargeWidth    = 220
	LargeHeight   = 60

	// Stroke colours mirror FT's CSS tokens.
	upColor   = "#16a34a" // --c-good
	downColor = "#dc2626" // --c-bad
	flatColor = "#94a3b8" // --c-muted
)

// Render returns the SVG markup for a sparkline as a string. Empty / too-short
// series return "—" so callers don't have to special-case.
//
// `closes` is expected chronological-ascending. Series with < 5 points renders
// as a dash, per spec.
func Render(closes []float64, width, height int) string {
	if len(closes) < 5 {
		return `<span class="sparkline-empty">—</span>`
	}
	if width <= 0 {
		width = DefaultWidth
	}
	if height <= 0 {
		height = DefaultHeight
	}

	min, max := closes[0], closes[0]
	for _, v := range closes {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	span := max - min
	if span == 0 {
		span = 1 // flat line: avoid div-by-zero, draw mid-line
	}

	// Map (i, close) → (x, y) inside [pad, width-pad] × [pad, height-pad].
	const pad = 2.0
	innerW := float64(width) - 2*pad
	innerH := float64(height) - 2*pad
	n := len(closes)
	pts := make([]string, n)
	for i, v := range closes {
		x := pad + innerW*float64(i)/float64(n-1)
		// Invert Y: max close at top.
		y := pad + innerH*(1-(v-min)/span)
		pts[i] = fmt.Sprintf("%.2f,%.2f", x, y)
	}

	color := flatColor
	switch {
	case closes[n-1] > closes[0]:
		color = upColor
	case closes[n-1] < closes[0]:
		color = downColor
	}

	// `vector-effect="non-scaling-stroke"` keeps the line crisp if the SVG
	// gets scaled by CSS. preserveAspectRatio=none lets us fill any width.
	return fmt.Sprintf(
		`<svg class="sparkline" width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none" aria-hidden="true"><polyline fill="none" stroke="%s" stroke-width="1.5" vector-effect="non-scaling-stroke" points="%s"/></svg>`,
		width, height, width, height, color, strings.Join(pts, " "),
	)
}

// RenderDefault is shorthand for Render(closes, DefaultWidth, DefaultHeight).
func RenderDefault(closes []float64) string {
	return Render(closes, DefaultWidth, DefaultHeight)
}

// RenderLarge is shorthand for the D9 hover popover size.
func RenderLarge(closes []float64) string {
	return Render(closes, LargeWidth, LargeHeight)
}

// Direction returns "up", "down", or "flat" for the 30-day directional cue.
// Used by tooltips and the hover popover.
func Direction(closes []float64) string {
	if len(closes) < 2 {
		return "flat"
	}
	first, last := closes[0], closes[len(closes)-1]
	switch {
	case last > first:
		return "up"
	case last < first:
		return "down"
	default:
		return "flat"
	}
}

// ChangePct returns the percentage change from first to last close. Returns 0
// for series too short to be meaningful (< 5 points).
func ChangePct(closes []float64) float64 {
	if len(closes) < 5 || closes[0] == 0 {
		return 0
	}
	return (closes[len(closes)-1] - closes[0]) / closes[0] * 100
}
