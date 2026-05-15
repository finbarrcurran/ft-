// Package donut renders compact donut charts as inline SVG. Designed to
// match the heatmap's server-side rendering style: no client-side chart
// library, no JS dependency at the call site, just stringly-emitted SVG that
// can be embedded into a JSON response or served directly.
//
// Implementation uses one <circle> per slice with stroke-dasharray + stroke-
// dashoffset rather than path arcs. The math is simpler and there are no
// floating-point edge cases around the 0° / 360° wrap.
package donut

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// Slice is one wedge of the donut.
type Slice struct {
	Label string
	Value float64
	Color string // any valid SVG paint: "rgb(r,g,b)", "#hex", named colour
}

// Options controls the rendered SVG.
type Options struct {
	Width, Height float64 // SVG viewBox size (square recommended)
	CenterText    string  // main text (e.g. dollar total)
	CenterSub     string  // sub-text below center (e.g. "value")
}

// FallbackColor is the muted neutral used when slice list is empty.
const FallbackColor = "rgb(38,46,60)"

// Render returns a complete <svg>…</svg> string for the donut.
func Render(slices []Slice, opts Options) string {
	if opts.Width <= 0 {
		opts.Width = 200
	}
	if opts.Height <= 0 {
		opts.Height = 200
	}

	cx, cy := opts.Width/2, opts.Height/2
	radius := math.Min(opts.Width, opts.Height) / 2 * 0.85
	strokeWidth := radius * 0.35
	midR := radius - strokeWidth/2 // middle of the stroke band
	circumference := 2 * math.Pi * midR

	total := 0.0
	for _, s := range slices {
		if s.Value > 0 {
			total += s.Value
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" width="%g" height="%g" style="display:block">`,
		opts.Width, opts.Height, opts.Width, opts.Height,
	)

	// Background ring. Catches gaps from float rounding; also handles the
	// empty-data state cleanly.
	fmt.Fprintf(&b,
		`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="%g" />`,
		cx, cy, midR, FallbackColor, strokeWidth,
	)

	if total > 0 {
		offset := 0.0
		for _, s := range slices {
			if s.Value <= 0 {
				continue
			}
			fraction := s.Value / total
			sliceLen := fraction * circumference
			fmt.Fprintf(&b,
				`<circle cx="%g" cy="%g" r="%g" fill="none" stroke="%s" stroke-width="%g" stroke-dasharray="%g %g" stroke-dashoffset="%g" transform="rotate(-90 %g %g)"><title>%s</title></circle>`,
				cx, cy, midR, s.Color, strokeWidth,
				sliceLen, circumference-sliceLen,
				-offset,
				cx, cy,
				html.EscapeString(sliceTooltip(s, total)),
			)
			offset += sliceLen
		}
	}

	// Center text. Main value above, sub label below — both centered.
	if opts.CenterText != "" {
		mainY := cy
		if opts.CenterSub != "" {
			mainY = cy - 6
		}
		fmt.Fprintf(&b,
			`<text x="%g" y="%g" text-anchor="middle" dominant-baseline="middle" fill="rgb(234,238,246)" font-family="'IBM Plex Mono',ui-monospace,monospace" font-weight="600" font-size="14">%s</text>`,
			cx, mainY, html.EscapeString(opts.CenterText),
		)
	}
	if opts.CenterSub != "" {
		fmt.Fprintf(&b,
			`<text x="%g" y="%g" text-anchor="middle" dominant-baseline="middle" fill="rgb(140,152,170)" font-family="'IBM Plex Sans',sans-serif" font-size="9" letter-spacing="0.12em" style="text-transform:uppercase">%s</text>`,
			cx, cy+12, html.EscapeString(opts.CenterSub),
		)
	}

	b.WriteString("</svg>")
	return b.String()
}

func sliceTooltip(s Slice, total float64) string {
	if total <= 0 {
		return s.Label
	}
	pct := (s.Value / total) * 100
	return fmt.Sprintf("%s — %.1f%%", s.Label, pct)
}

// Palette returns up to n colours for slice rotation. Cycles after running out.
// Tuned to the FT design tokens so donuts read as part of the dashboard.
func Palette(n int) []string {
	base := []string{
		"rgb(255,184,0)",   // accent
		"rgb(16,200,124)",  // gain
		"rgb(64,230,154)",  // gain-strong
		"rgb(255,178,64)",  // amber
		"rgb(140,152,170)", // text-muted
		"rgb(245,80,110)",  // loss
		"rgb(56,68,88)",    // border-strong
		"rgb(255,110,140)", // loss-strong
	}
	if n <= len(base) {
		return base[:n]
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = base[i%len(base)]
	}
	return out
}
