package heatmap

import (
	"math"
	"sort"
)

// Rect is an axis-aligned rectangle in user units (pixels at viewBox scale).
type Rect struct{ X, Y, W, H float64 }

// Placed is one tile positioned in the layout.
type Placed struct {
	Tile *MarketTile
	Rect Rect
}

// LayoutOptions controls inner-tile gap and per-sector top padding.
type LayoutOptions struct {
	PaddingInner float64 // gap (px) between adjacent tiles within a sector
	PaddingTop   float64 // top padding (px) inside each sector box for the label
}

// LayoutHierarchical groups tiles by sector, sizes each sector box by its
// total market cap with the squarified treemap algorithm, then recursively
// squarifies the tiles within each sector box.
//
// Returns: positioned tiles and sector bounding boxes (so the renderer can
// draw sector labels above each group).
func LayoutHierarchical(tiles []*MarketTile, r Rect, opts LayoutOptions) (placed []Placed, sectorBoxes map[string]Rect) {
	if len(tiles) == 0 || r.W <= 0 || r.H <= 0 {
		return nil, nil
	}

	// Group by sector preserving the canonical Sectors order so the legend
	// reads predictably across reloads.
	bySector := map[string][]*MarketTile{}
	for _, t := range tiles {
		bySector[t.Sector] = append(bySector[t.Sector], t)
	}
	type sectorEntry struct {
		Name  string
		Total float64
		Tiles []*MarketTile
	}
	var sectors []sectorEntry
	for _, name := range Sectors {
		ts := bySector[name]
		if len(ts) == 0 {
			continue
		}
		sum := 0.0
		for _, t := range ts {
			sum += t.MarketCapB
		}
		sectors = append(sectors, sectorEntry{name, sum, ts})
	}
	// Any sectors not in the canonical list (defensive) append at the end.
	for name, ts := range bySector {
		known := false
		for _, s := range Sectors {
			if s == name {
				known = true
				break
			}
		}
		if !known {
			sum := 0.0
			for _, t := range ts {
				sum += t.MarketCapB
			}
			sectors = append(sectors, sectorEntry{name, sum, ts})
		}
	}

	// Outer layout: position sector boxes.
	sectorRects := squarify(
		len(sectors),
		func(i int) float64 { return sectors[i].Total },
		r,
	)

	sectorBoxes = make(map[string]Rect, len(sectors))
	for i, srect := range sectorRects {
		sectorBoxes[sectors[i].Name] = srect
		// Carve out the label band at the top, plus a small interior pad.
		body := Rect{
			X: srect.X + opts.PaddingInner/2,
			Y: srect.Y + opts.PaddingTop,
			W: srect.W - opts.PaddingInner,
			H: srect.H - opts.PaddingTop - opts.PaddingInner/2,
		}
		if body.W <= 0 || body.H <= 0 {
			continue
		}

		// Inner layout: tiles within this sector.
		ts := sectors[i].Tiles
		tileRects := squarify(
			len(ts),
			func(j int) float64 { return ts[j].MarketCapB },
			body,
		)
		for j, tr := range tileRects {
			// Apply a small inner padding so tiles read as separate units.
			pad := opts.PaddingInner
			padded := Rect{
				X: tr.X + pad/2,
				Y: tr.Y + pad/2,
				W: tr.W - pad,
				H: tr.H - pad,
			}
			if padded.W <= 1 || padded.H <= 1 {
				padded = tr // tile too small to bear padding
			}
			placed = append(placed, Placed{Tile: ts[j], Rect: padded})
		}
	}
	return placed, sectorBoxes
}

// squarify runs the squarified treemap algorithm on n items where item i has
// value valueFn(i), packing them into rect r.
//
// Returns one Rect per item in ORIGINAL input order. Internally we sort
// descending by value (the algorithm requires it) then map back at the end.
//
// Reference: Bruls, Huijsen, van Wijk, "Squarified Treemaps" (2000).
// Implementation mirrors d3-hierarchy's `treemapSquarify` strategy.
func squarify(n int, valueFn func(int) float64, r Rect) []Rect {
	if n == 0 || r.W <= 0 || r.H <= 0 {
		return nil
	}

	type indexed struct {
		idx int
		val float64
	}
	items := make([]indexed, n)
	total := 0.0
	for i := 0; i < n; i++ {
		v := valueFn(i)
		if v < 0 {
			v = 0
		}
		items[i] = indexed{i, v}
		total += v
	}
	if total <= 0 {
		return make([]Rect, n) // all zero rects
	}
	sort.Slice(items, func(i, j int) bool { return items[i].val > items[j].val })

	scale := (r.W * r.H) / total

	sortedRects := make([]Rect, n)
	var work func(start int, rect Rect)
	work = func(start int, rect Rect) {
		if start >= n {
			return
		}
		if start == n-1 {
			sortedRects[start] = rect
			return
		}
		short := math.Min(rect.W, rect.H)
		if short <= 0 {
			return
		}

		// Greedily extend the current row while aspect ratios improve.
		row := []float64{items[start].val * scale}
		bestWorst := worstAspect(row, short)
		j := start + 1
		for j < n {
			row = append(row, items[j].val*scale)
			nextWorst := worstAspect(row, short)
			if nextWorst > bestWorst {
				row = row[:len(row)-1]
				break
			}
			bestWorst = nextWorst
			j++
		}
		rowEnd := start + len(row) // exclusive

		// Place the row.
		rowSum := 0.0
		for _, v := range row {
			rowSum += v
		}

		var newRect Rect
		if rect.W <= rect.H {
			// Width is short — row packs along the top edge.
			rowH := rowSum / rect.W
			x := rect.X
			for k, v := range row {
				w := v / rowH
				sortedRects[start+k] = Rect{X: x, Y: rect.Y, W: w, H: rowH}
				x += w
			}
			newRect = Rect{X: rect.X, Y: rect.Y + rowH, W: rect.W, H: rect.H - rowH}
		} else {
			// Height is short — row packs along the left edge.
			rowW := rowSum / rect.H
			y := rect.Y
			for k, v := range row {
				h := v / rowW
				sortedRects[start+k] = Rect{X: rect.X, Y: y, W: rowW, H: h}
				y += h
			}
			newRect = Rect{X: rect.X + rowW, Y: rect.Y, W: rect.W - rowW, H: rect.H}
		}

		work(rowEnd, newRect)
	}
	work(0, r)

	// Reorder to original index order.
	result := make([]Rect, n)
	for sortIdx, item := range items {
		result[item.idx] = sortedRects[sortIdx]
	}
	return result
}

// worstAspect returns the worst (max) aspect ratio in a row packed along the
// shorter dimension `short`. Closed-form from the Bruls paper:
//
//	worst(R, w) = max(w² * r_max / s², s² / (w² * r_min))
//
// where s = sum(R), r_max = max(R), r_min = min(R).
func worstAspect(row []float64, short float64) float64 {
	if len(row) == 0 || short <= 0 {
		return math.Inf(1)
	}
	s := 0.0
	rMax, rMin := row[0], row[0]
	for _, v := range row {
		s += v
		if v > rMax {
			rMax = v
		}
		if v < rMin {
			rMin = v
		}
	}
	if s == 0 || rMin == 0 {
		return math.Inf(1)
	}
	w2 := short * short
	s2 := s * s
	return math.Max(w2*rMax/s2, s2/(w2*rMin))
}
