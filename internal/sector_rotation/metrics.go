// Spec 9f D3 — metrics computation.
//
// Pure functions over a sector's snapshot history. Returns the full payload
// the rotation tab needs in one pass:
//   - multi-window returns (1W / 1M / 3M / 6M / YTD)
//   - relative strength vs SPY at 3M (tag-driving)
//   - tag classification per locked rules
//   - 26-week sparkline points (weekly closes)
//   - holdings + watchlist counts (joined by the handler)
//
// 4-hour in-memory cache; daily ingestion naturally invalidates the
// underlying data so the next read picks up fresh numbers.

package sector_rotation

import (
	"context"
	"ft/internal/store"
	"sync"
	"time"
)

// SectorMetrics is the per-row payload returned by the rotation handler.
type SectorMetrics struct {
	SectorID         int64    `json:"sectorId"`
	Code             string   `json:"code"`
	DisplayName      string   `json:"displayName"`
	ParentGICS       string   `json:"parentGics"`
	JordiStage       *int     `json:"jordiStage,omitempty"`
	ETFTickerPrimary string   `json:"etfTickerPrimary"`
	Return1W         *float64 `json:"return1w,omitempty"`
	Return1M         *float64 `json:"return1m,omitempty"`
	Return3M         *float64 `json:"return3m,omitempty"`
	Return6M         *float64 `json:"return6m,omitempty"`
	ReturnYTD        *float64 `json:"returnYtd,omitempty"`
	RSvsSPY3M        *float64 `json:"rsVsSpy3m,omitempty"`
	Tag              string   `json:"tag"` // "rotating_in" | "rotating_out" | "neutral" | "no_data"
	Sparkline        []float64 `json:"sparkline,omitempty"`
	HoldingsCount    int      `json:"holdingsCount"`
	WatchlistCount   int      `json:"watchlistCount"`
	LastSnapshot     string   `json:"lastSnapshot,omitempty"`
	DisplayOrderAuto int      `json:"displayOrderAuto"`
	DisplayOrderUser *int     `json:"displayOrderUser,omitempty"`
}

// ComputeAll runs metrics for every active sector. Honors user ordering
// when present; otherwise sorts by Return3M descending per spec lock #3.
func ComputeAll(ctx context.Context, st *store.Store, userID int64) ([]SectorMetrics, error) {
	if c := getCached(); c != nil {
		// Cache stores the unsorted-by-id payload; ordering is applied per
		// call below since user ordering may have changed.
		out := make([]SectorMetrics, len(c))
		copy(out, c)
		// Re-attach counts in case watchlist changed without ingest.
		hcount, _ := st.HoldingsCountBySector(ctx, userID)
		wcount, _ := st.WatchlistCountBySector(ctx, userID)
		for i := range out {
			out[i].HoldingsCount = hcount[out[i].SectorID]
			out[i].WatchlistCount = wcount[out[i].SectorID]
		}
		applyOrdering(out)
		return out, nil
	}

	sectors, err := st.ListSectorUniverse(ctx)
	if err != nil {
		return nil, err
	}
	hcount, _ := st.HoldingsCountBySector(ctx, userID)
	wcount, _ := st.WatchlistCountBySector(ctx, userID)

	out := make([]SectorMetrics, 0, len(sectors))
	for _, s := range sectors {
		snaps, err := st.ListRecentSnapshots(ctx, s.ID, 400)
		if err != nil {
			continue
		}
		m := SectorMetrics{
			SectorID:         s.ID,
			Code:             s.Code,
			DisplayName:      s.DisplayName,
			ParentGICS:       s.ParentGICS,
			JordiStage:       s.JordiStage,
			ETFTickerPrimary: s.ETFTickerPrimary,
			Tag:              "no_data",
			HoldingsCount:    hcount[s.ID],
			WatchlistCount:   wcount[s.ID],
			DisplayOrderAuto: s.DisplayOrderAuto,
			DisplayOrderUser: s.DisplayOrderUser,
		}
		if len(snaps) == 0 {
			out = append(out, m)
			continue
		}
		m.LastSnapshot = snaps[len(snaps)-1].Date

		// Build a date-indexed primary-close map for window lookups.
		byDate := make(map[string]float64, len(snaps))
		dates := make([]string, 0, len(snaps))
		closes := make([]float64, 0, len(snaps))
		for _, sn := range snaps {
			byDate[sn.Date] = sn.ClosePrimary
			dates = append(dates, sn.Date)
			closes = append(closes, sn.ClosePrimary)
		}
		latestClose := closes[len(closes)-1]
		latestDate := dates[len(dates)-1]

		m.Return1W = computeReturn(byDate, dates, latestClose, 7)
		m.Return1M = computeReturn(byDate, dates, latestClose, 30)
		m.Return3M = computeReturn(byDate, dates, latestClose, 90)
		m.Return6M = computeReturn(byDate, dates, latestClose, 180)
		m.ReturnYTD = computeYTD(byDate, dates, latestClose, latestDate)

		// RS vs SPY at 3M = sector_3m_return / spy_3m_return (both as
		// 1+pct). 0/0 guarded.
		if m.Return3M != nil {
			spyR := computeSPYReturn(snaps, 90)
			if spyR != nil && (*spyR+1) != 0 {
				rs := (*m.Return3M + 1) / (*spyR + 1)
				m.RSvsSPY3M = &rs
				m.Tag = classifyTag(rs)
			} else {
				m.Tag = "neutral"
			}
		}

		// Sparkline: 26 weekly closes (every 7th trading day from the
		// end, walking backwards). Cheaper than picking literal weeks
		// and good enough for the inline SVG.
		m.Sparkline = weeklySparkline(closes, 26)

		out = append(out, m)
	}
	setCached(out)
	applyOrdering(out)
	return out, nil
}

// computeReturn returns (latestClose / closeNdaysAgo) - 1.
// Falls back to the closest earlier date when the exact day is missing
// (weekends, holidays).
func computeReturn(byDate map[string]float64, dates []string, latestClose float64, daysAgo int) *float64 {
	if len(dates) == 0 || latestClose <= 0 || daysAgo <= 0 {
		return nil
	}
	latest, err := time.Parse("2006-01-02", dates[len(dates)-1])
	if err != nil {
		return nil
	}
	target := latest.AddDate(0, 0, -daysAgo)
	// Walk dates from the start; pick the last date ≤ target.
	var refClose float64
	for _, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if t.After(target) {
			break
		}
		refClose = byDate[d]
	}
	if refClose <= 0 {
		return nil
	}
	ret := (latestClose / refClose) - 1
	return &ret
}

// computeYTD walks back to the first trading day of the latest snapshot's
// calendar year.
func computeYTD(byDate map[string]float64, dates []string, latestClose float64, latestDate string) *float64 {
	if latestClose <= 0 || len(dates) == 0 {
		return nil
	}
	t, err := time.Parse("2006-01-02", latestDate)
	if err != nil {
		return nil
	}
	year := t.Year()
	prefix := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	var refClose float64
	for _, d := range dates {
		dt, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if dt.Before(prefix) {
			continue
		}
		refClose = byDate[d]
		break // first trading day of year
	}
	if refClose <= 0 {
		return nil
	}
	ret := (latestClose / refClose) - 1
	return &ret
}

// computeSPYReturn — same window math, anchored on benchmark_spy_close.
func computeSPYReturn(snaps []store.SectorSnapshot, daysAgo int) *float64 {
	if len(snaps) == 0 || daysAgo <= 0 {
		return nil
	}
	latest := snaps[len(snaps)-1]
	if latest.BenchmarkSPY <= 0 {
		return nil
	}
	latestDate, err := time.Parse("2006-01-02", latest.Date)
	if err != nil {
		return nil
	}
	target := latestDate.AddDate(0, 0, -daysAgo)
	var refSPY float64
	for _, sn := range snaps {
		t, err := time.Parse("2006-01-02", sn.Date)
		if err != nil {
			continue
		}
		if t.After(target) {
			break
		}
		refSPY = sn.BenchmarkSPY
	}
	if refSPY <= 0 {
		return nil
	}
	ret := (latest.BenchmarkSPY / refSPY) - 1
	return &ret
}

// classifyTag — locked thresholds (Decision #1).
func classifyTag(rsVsSPY3M float64) string {
	if rsVsSPY3M >= 1.05 {
		return "rotating_in"
	}
	if rsVsSPY3M <= 0.95 {
		return "rotating_out"
	}
	return "neutral"
}

// weeklySparkline picks every 5th close (≈ weekly) from the tail until
// we have `wantPoints` samples, then reverses to chronological order.
func weeklySparkline(closes []float64, wantPoints int) []float64 {
	if len(closes) == 0 {
		return nil
	}
	step := 5 // approx one trading week
	if len(closes) < wantPoints*step {
		// Short series — return as-is.
		out := make([]float64, len(closes))
		copy(out, closes)
		return out
	}
	out := make([]float64, 0, wantPoints)
	for i := len(closes) - 1; i >= 0 && len(out) < wantPoints; i -= step {
		out = append(out, closes[i])
	}
	// Reverse → chronological.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// applyOrdering sorts in place: user ordering when any rows have one,
// otherwise Return3M descending per Spec 9f Decision #3.
func applyOrdering(rows []SectorMetrics) {
	hasUser := false
	for _, r := range rows {
		if r.DisplayOrderUser != nil {
			hasUser = true
			break
		}
	}
	if hasUser {
		// User ordering: rows with DisplayOrderUser come first (asc),
		// then rows without (sorted by DisplayOrderAuto).
		insertionSort(rows, func(a, b SectorMetrics) bool {
			if a.DisplayOrderUser != nil && b.DisplayOrderUser != nil {
				return *a.DisplayOrderUser < *b.DisplayOrderUser
			}
			if a.DisplayOrderUser != nil {
				return true
			}
			if b.DisplayOrderUser != nil {
				return false
			}
			return a.DisplayOrderAuto < b.DisplayOrderAuto
		})
		return
	}
	// Default: Return3M descending; NaN/nil last.
	insertionSort(rows, func(a, b SectorMetrics) bool {
		if a.Return3M == nil && b.Return3M == nil {
			return a.DisplayOrderAuto < b.DisplayOrderAuto
		}
		if a.Return3M == nil {
			return false
		}
		if b.Return3M == nil {
			return true
		}
		return *a.Return3M > *b.Return3M
	})
}

// insertionSort is fine for 34 items and keeps us free of the sort.Sort
// indirection.
func insertionSort(rows []SectorMetrics, less func(a, b SectorMetrics) bool) {
	for i := 1; i < len(rows); i++ {
		j := i
		for j > 0 && less(rows[j], rows[j-1]) {
			rows[j], rows[j-1] = rows[j-1], rows[j]
			j--
		}
	}
}

// ----- 4-hour cache ------------------------------------------------------
//
// The cache stores the unordered-by-id result so callers see the same
// metrics regardless of who triggered the recompute. Counts are re-fetched
// per call (cheap) since holdings/watchlist mutate independently of the
// daily ingest.

var (
	cacheMu    sync.RWMutex
	cacheData  []SectorMetrics
	cacheStamp time.Time
)

const cacheTTL = 4 * time.Hour

func getCached() []SectorMetrics {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if cacheData == nil {
		return nil
	}
	if time.Since(cacheStamp) > cacheTTL {
		return nil
	}
	out := make([]SectorMetrics, len(cacheData))
	copy(out, cacheData)
	return out
}

func setCached(rows []SectorMetrics) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cacheData = make([]SectorMetrics, len(rows))
	copy(cacheData, rows)
	cacheStamp = time.Now()
}

// BustCache forces the next ComputeAll() to recompute from snapshots.
// Called after ingest runs.
func BustCache() {
	cacheMu.Lock()
	cacheData = nil
	cacheStamp = time.Time{}
	cacheMu.Unlock()
}
