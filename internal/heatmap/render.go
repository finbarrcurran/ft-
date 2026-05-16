package heatmap

import (
	"fmt"
	"html"
	"math"
	"strings"
)

// RenderOptions controls SVG dimensions and which tickers are flagged as held.
type RenderOptions struct {
	Width        float64
	Height       float64
	PaddingInner float64         // tile gap (default 2)
	PaddingTop   float64         // sector top padding for label (default 18)
	Held         map[string]bool // ticker → true
	Sector       string          // optional filter: only render this sector (e.g., "Technology")

	// Spec 6 D3 — when non-nil, render this slice instead of the package-level
	// S&P 500 sample dataset. Used for "my holdings" mode: caller passes one
	// MarketTile per holding (size via MarketCapB → position value USD).
	// Empty slice renders an empty SVG. Live overlay is NOT applied to the
	// override path; the caller is responsible for fresh values.
	Source []MarketTile
}

// Defaults applied if a zero value is passed.
func (o *RenderOptions) defaults() {
	if o.Width <= 0 {
		o.Width = 1100
	}
	if o.Height <= 0 {
		o.Height = 600
	}
	if o.PaddingInner <= 0 {
		o.PaddingInner = 2
	}
	if o.PaddingTop <= 0 {
		o.PaddingTop = 18
	}
}

// Render returns a complete `<svg>…</svg>` document representing the heatmap.
// The svg is self-contained: inline fills, native `<title>` tooltips, no
// external font references.
func Render(opts RenderOptions) string {
	opts.defaults()

	// Build per-request tile slice. Two paths:
	//   * Source override (Spec 6 my-holdings) → use caller-supplied tiles
	//     verbatim; the caller already has live values.
	//   * Default → merge package-level Tiles with live overlay.
	var source []MarketTile
	if opts.Source != nil {
		source = opts.Source
	} else {
		source = applyLive()
	}
	tiles := make([]*MarketTile, 0, len(source))
	for i := range source {
		t := source[i] // copy
		if opts.Sector != "" && t.Sector != opts.Sector {
			continue
		}
		if opts.Held[t.Ticker] {
			t.Held = true
		}
		tiles = append(tiles, &t)
	}

	// When filtered to a single sector, lay out flat (no sector grouping).
	var placed []Placed
	var sectorBoxes map[string]Rect
	if opts.Sector != "" {
		flat := squarify(len(tiles), func(i int) float64 { return tiles[i].MarketCapB },
			Rect{X: 0, Y: 0, W: opts.Width, H: opts.Height})
		placed = make([]Placed, 0, len(tiles))
		for i, r := range flat {
			pad := opts.PaddingInner
			padded := Rect{X: r.X + pad/2, Y: r.Y + pad/2, W: r.W - pad, H: r.H - pad}
			if padded.W <= 1 || padded.H <= 1 {
				padded = r
			}
			placed = append(placed, Placed{Tile: tiles[i], Rect: padded})
		}
	} else {
		placed, sectorBoxes = LayoutHierarchical(tiles,
			Rect{X: 0, Y: 0, W: opts.Width, H: opts.Height},
			LayoutOptions{PaddingInner: opts.PaddingInner, PaddingTop: opts.PaddingTop},
		)
	}

	var b strings.Builder
	fmt.Fprintf(&b,
		`<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%g" viewBox="0 0 %g %g" preserveAspectRatio="xMidYMid meet" style="display:block;font-family:'IBM Plex Mono',ui-monospace,monospace;background:rgb(10,13,18)">`,
		opts.Width, opts.Height, opts.Width, opts.Height,
	)

	// 1) Sector background fills (faintly visible behind the tile padding gaps).
	for name, sb := range sectorBoxes {
		_ = name
		fmt.Fprintf(&b,
			`<rect x="%g" y="%g" width="%g" height="%g" fill="rgb(18,22,30)" stroke="rgb(38,46,60)" stroke-width="0.5"/>`,
			sb.X, sb.Y, sb.W, sb.H,
		)
	}

	// 2) Tiles.
	for _, p := range placed {
		renderTile(&b, p)
	}

	// 3) Sector labels last (on top).
	for name, sb := range sectorBoxes {
		// Only draw the label if the sector box is wide enough to contain it.
		if sb.W < 70 || sb.H < 20 {
			continue
		}
		fmt.Fprintf(&b,
			`<text x="%g" y="%g" fill="rgb(140,152,170)" font-family="'IBM Plex Sans',sans-serif" font-size="10" font-weight="600" letter-spacing="0.12em" style="text-transform:uppercase;pointer-events:none">%s</text>`,
			sb.X+6, sb.Y+13, html.EscapeString(name),
		)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func renderTile(b *strings.Builder, p Placed) {
	t := p.Tile
	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		return
	}
	fill := TileColor(t.ChangePct)
	textColor := TextColor(t.ChangePct)

	// Build the tooltip text.
	heldTag := ""
	if t.Held {
		heldTag = " · HELD"
	}
	sign := ""
	if t.ChangePct > 0 {
		sign = "+"
	}
	tooltip := fmt.Sprintf("%s — %s\n$%.2f · %s%.2f%% · Mkt cap $%gB · Vol %.1fM%s",
		t.Ticker, t.Name,
		t.Price, sign, t.ChangePct, t.MarketCapB, t.VolumeM, heldTag,
	)

	// <g> with <title> gives free native browser tooltips on hover.
	fmt.Fprintf(b, `<g>`)
	fmt.Fprintf(b, `<title>%s</title>`, html.EscapeString(tooltip))

	// Background rect.
	fmt.Fprintf(b,
		`<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`,
		r.X, r.Y, r.W, r.H, fill,
	)

	// Held accent stripe down the left edge.
	if t.Held {
		fmt.Fprintf(b,
			`<rect x="%g" y="%g" width="3" height="%g" fill="%s"/>`,
			r.X, r.Y, r.H, HeldMarkColor,
		)
	}

	// Text: only render labels if the tile is big enough.
	if r.W >= 28 && r.H >= 22 {
		fontSize := math.Max(10, math.Min(16, math.Sqrt(r.W*r.H)/8))
		cx := r.X + r.W/2

		// Ticker
		fmt.Fprintf(b,
			`<text x="%g" y="%g" fill="%s" font-weight="600" font-size="%g" text-anchor="middle" dominant-baseline="middle" style="pointer-events:none;letter-spacing:0.02em">%s</text>`,
			cx, r.Y+r.H/2-fontSize*0.4, textColor, fontSize, html.EscapeString(t.Ticker),
		)
		// Change %
		fmt.Fprintf(b,
			`<text x="%g" y="%g" fill="%s" font-size="%g" text-anchor="middle" dominant-baseline="middle" style="pointer-events:none;opacity:0.95">%s%.2f%%</text>`,
			cx, r.Y+r.H/2+fontSize*0.9, textColor, fontSize*0.82, sign, t.ChangePct,
		)
		// Price (only when tile is big enough)
		if r.W >= 100 && r.H >= 64 {
			fmt.Fprintf(b,
				`<text x="%g" y="%g" fill="%s" font-size="%g" text-anchor="middle" dominant-baseline="middle" style="pointer-events:none;opacity:0.72">$%s</text>`,
				cx, r.Y+r.H/2+fontSize*2.0, textColor, fontSize*0.72, formatPrice(t.Price),
			)
		}
		// Full name in the bottom-left corner on large tiles.
		if r.W >= 130 && r.H >= 80 {
			name := t.Name
			if len(name) > 16 {
				name = name[:16]
			}
			fmt.Fprintf(b,
				`<text x="%g" y="%g" fill="%s" font-family="'IBM Plex Sans',sans-serif" font-size="9" style="pointer-events:none;opacity:0.6;letter-spacing:0.1em;text-transform:uppercase">%s</text>`,
				r.X+6, r.Y+r.H-6, textColor, html.EscapeString(name),
			)
		}
	}

	fmt.Fprintf(b, `</g>`)
}

func formatPrice(p float64) string {
	// US-style thousands separators; whole-dollar precision (tooltips show
	// full precision).
	if p >= 1000 {
		intPart := int(p)
		s := fmt.Sprintf("%d", intPart)
		// insert commas
		n := len(s)
		if n <= 3 {
			return s
		}
		var out []byte
		first := n % 3
		if first > 0 {
			out = append(out, s[:first]...)
			if n > first {
				out = append(out, ',')
			}
		}
		for i := first; i < n; i += 3 {
			out = append(out, s[i:i+3]...)
			if i+3 < n {
				out = append(out, ',')
			}
		}
		return string(out)
	}
	return fmt.Sprintf("%.0f", p)
}
