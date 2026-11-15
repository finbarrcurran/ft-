package visser46

import (
	"context"
	"fmt"
	"ft/internal/store"
	"sort"
	"time"
)

// Breadth is computed by FT from stored closes (price_history), never taken from a published figure.
// "Rising" compares the moving average now with the same average 5 observations earlier: 5 calendar days for
// tokens (one close a day, every day), 5 trading sessions for stocks (the stored closes are sessions).
// A series that is missing, too short, stale or gappy is LISTED in Missing and left out of that metric's
// denominator; nothing is filled in.
const risingLookback = 5

// Metric is one breadth figure: Yes of N eligible members.
type Metric struct {
	Yes int      `json:"yes"`
	N   int      `json:"n"`
	Pct *float64 `json:"pct"` // nil when N == 0
}

func (m *Metric) add(yes bool) {
	m.N++
	if yes {
		m.Yes++
	}
}

func (m *Metric) finish() {
	if m.N > 0 {
		p := float64(m.Yes) * 100 / float64(m.N)
		m.Pct = &p
	}
}

// GroupBreadth is one vertical (or the total).
type GroupBreadth struct {
	Name      string `json:"name"`
	Members   int    `json:"members"`
	Above50   Metric `json:"above50"`
	Above200  Metric `json:"above200"`
	Rising50  Metric `json:"rising50"`
	Rising200 Metric `json:"rising200"`
}

// MissingSeries is a member left out of a metric, with the reason.
type MissingSeries struct {
	Symbol   string   `json:"symbol"`
	Vertical string   `json:"vertical"`
	Kind     string   `json:"kind"`
	Closes   int      `json:"closes"`
	Newest   string   `json:"newest,omitempty"`
	Reasons  []string `json:"reasons"`
}

// BreadthResult is the whole view.
type BreadthResult struct {
	AsOf    string          `json:"asOf"`
	Total   GroupBreadth    `json:"total"`
	Groups  []GroupBreadth  `json:"verticals"`
	Missing []MissingSeries `json:"missing"`
	Note    string          `json:"note"`
}

// seriesFlags is the per-series result of the arithmetic.
type seriesFlags struct {
	has50, has200, hasR50, hasR200 bool
	above50, above200, r50, r200   bool
}

func mean(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// computeFlags does the moving-average arithmetic on closes (oldest first).
func computeFlags(c []float64) seriesFlags {
	var f seriesFlags
	n := len(c)
	if n >= 50 {
		f.has50, f.above50 = true, c[n-1] > mean(c[n-50:])
	}
	if n >= 50+risingLookback {
		f.hasR50, f.r50 = true, mean(c[n-50:]) > mean(c[n-50-risingLookback:n-risingLookback])
	}
	if n >= 200 {
		f.has200, f.above200 = true, c[n-1] > mean(c[n-200:])
	}
	if n >= 200+risingLookback {
		f.hasR200, f.r200 = true, mean(c[n-200:]) > mean(c[n-200-risingLookback:n-risingLookback])
	}
	return f
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// gapReason returns why a series cannot be trusted for a window of `win` closes, or "". Tokens have a close
// every calendar day, so the last `win` closes must span at most win-1+3 days (3 days of slack); stocks are
// sessions, so the span may be about 7/5 of the window plus 10 days of holidays.
func gapReason(pts []store.PricePoint, win int, token bool) string {
	n := len(pts)
	if n < win {
		return ""
	}
	a, ok1 := parseDay(pts[n-win].Date)
	b, ok2 := parseDay(pts[n-1].Date)
	if !ok1 || !ok2 {
		return "bad dates"
	}
	span := int(b.Sub(a).Hours() / 24)
	limit := win - 1 + 3
	if !token {
		limit = (win*7+4)/5 + 10
	}
	if span > limit {
		return fmt.Sprintf("gaps in the last %d closes (%d days)", win, span)
	}
	return ""
}

// Breadth computes the view as of `now` from price_history for the index members.
func (s *Service) Breadth(ctx context.Context) (*BreadthResult, error) {
	all, err := s.Store.ListCryptoIndexMembers(ctx, IndexKey)
	if err != nil {
		return nil, err
	}
	now := s.now()
	res := &BreadthResult{
		AsOf:  now.Format("2006-01-02"),
		Total: GroupBreadth{Name: "Total"},
		Note:  "Computed by FT from stored daily closes. Above = last close over the simple average of the last 50/200 closes; rising = that average over the same average 5 observations earlier (tokens: calendar days; stocks: trading sessions). Members without enough clean history are listed under Missing and left out of that figure; nothing is filled in.",
	}
	groups := map[string]*GroupBreadth{}
	var order []string
	for _, m := range all {
		g, ok := groups[m.Vertical]
		if !ok {
			g = &GroupBreadth{Name: m.Vertical}
			groups[m.Vertical] = g
			order = append(order, m.Vertical)
		}
		g.Members++
		res.Total.Members++

		kind, ticker, token := "crypto", m.Symbol, m.AssetKind == "token"
		if !token {
			kind = "stock"
			if m.StockTicker != nil {
				ticker = *m.StockTicker
			}
		}
		pts, err := s.Store.ListPriceHistory(ctx, ticker, kind)
		if err != nil {
			return nil, err
		}
		ms := MissingSeries{Symbol: m.Symbol, Vertical: m.Vertical, Kind: m.AssetKind, Closes: len(pts)}
		if len(pts) == 0 {
			ms.Reasons = []string{"no stored closes"}
			res.Missing = append(res.Missing, ms)
			continue
		}
		ms.Newest = pts[len(pts)-1].Date
		// Stale series: a token's newest close must be within 3 days, a stock's within 7 (long weekends, holidays).
		if nd, ok := parseDay(ms.Newest); ok {
			maxAge := 3
			if !token {
				maxAge = 7
			}
			if age := int(now.Sub(nd).Hours() / 24); age > maxAge {
				ms.Reasons = append(ms.Reasons, fmt.Sprintf("stale: newest close %s is %d days old", ms.Newest, age))
				res.Missing = append(res.Missing, ms)
				continue
			}
		}
		closes := make([]float64, len(pts))
		for i, p := range pts {
			closes[i] = p.Close
		}
		f := computeFlags(closes)
		use := func(has bool, win int, name string, add func(bool), val bool) {
			switch {
			case !has:
				ms.Reasons = append(ms.Reasons, fmt.Sprintf("%s: needs %d closes, has %d", name, win, len(pts)))
			default:
				if why := gapReason(pts, win, token); why != "" {
					ms.Reasons = append(ms.Reasons, name+": "+why)
				} else {
					add(val)
				}
			}
		}
		use(f.has50, 50, "above 50D", func(v bool) { g.Above50.add(v); res.Total.Above50.add(v) }, f.above50)
		use(f.has200, 200, "above 200D", func(v bool) { g.Above200.add(v); res.Total.Above200.add(v) }, f.above200)
		use(f.hasR50, 50+risingLookback, "rising 50D", func(v bool) { g.Rising50.add(v); res.Total.Rising50.add(v) }, f.r50)
		use(f.hasR200, 200+risingLookback, "rising 200D", func(v bool) { g.Rising200.add(v); res.Total.Rising200.add(v) }, f.r200)
		if len(ms.Reasons) > 0 {
			res.Missing = append(res.Missing, ms)
		}
	}
	sort.Strings(order)
	for _, name := range order {
		g := groups[name]
		g.Above50.finish()
		g.Above200.finish()
		g.Rising50.finish()
		g.Rising200.finish()
		res.Groups = append(res.Groups, *g)
	}
	res.Total.Above50.finish()
	res.Total.Above200.finish()
	res.Total.Rising50.finish()
	res.Total.Rising200.finish()
	return res, nil
}
