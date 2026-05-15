package heatmap

import (
	"fmt"
	"math"
)

// Color stops — ported byte-for-byte from lib/utils/heatmap-color.ts.
//   NEUTRAL  matches surface-raised
//   GAIN_MAX matches --color-gain
//   LOSS_MAX matches --color-loss
var (
	colorNeutral = struct{ R, G, B int }{38, 46, 60}
	colorGainMax = struct{ R, G, B int }{16, 200, 124}
	colorLossMax = struct{ R, G, B int }{245, 80, 110}
)

// HeldMarkColor is the accent stripe drawn on tiles for tickers the user
// holds. Always reads as the brand accent.
const HeldMarkColor = "rgb(255,184,0)"

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

// TileColor maps a daily % change to a background color.
// Symmetric around zero: 0% → near-neutral gray; +3% saturated green;
// −3% saturated red. Beyond ±3% clamps to the extreme.
func TileColor(changePct float64) string {
	clamped := math.Max(-3, math.Min(3, changePct))
	t := math.Abs(clamped) / 3
	target := colorGainMax
	if clamped < 0 {
		target = colorLossMax
	}
	return fmt.Sprintf("rgb(%d,%d,%d)",
		int(lerp(float64(colorNeutral.R), float64(target.R), t)),
		int(lerp(float64(colorNeutral.G), float64(target.G), t)),
		int(lerp(float64(colorNeutral.B), float64(target.B), t)),
	)
}

// TextColor returns the foreground color for the tile, picked to keep
// contrast readable across the gradient.
func TextColor(changePct float64) string {
	if math.Abs(changePct) >= 1.5 {
		return "rgb(255,255,255)"
	}
	return "rgb(234,238,246)"
}
