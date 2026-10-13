// Package snapshot is the weekly FT snapshot (SC-46 / P5): a deterministic, LLM-free record of
// FT's state that is persisted per ISO week so that "what changed since last week" and 4-week
// trends can be answered. This file holds the types and the pure comparison; render.go turns a
// payload plus its comparison into the short Markdown file, store.go persists the rows.
package snapshot

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is bumped when the payload shape changes.
const SchemaVersion = 2

// Payload is one week's snapshot. Keys are "<section>.<name>"; the section prefix groups them
// in the rendered file.
type Payload struct {
	Version     int                 `json:"version"`
	WeekStart   string              `json:"weekStart"`
	GeneratedAt string              `json:"generatedAt"`
	Metrics     map[string]float64  `json:"metrics"`          // numeric values with deltas and trends
	Texts       map[string]string   `json:"texts"`            // categorical values: "changed from X to Y"
	Sets        map[string][]string `json:"sets"`             // sorted members: added / removed lines
	Lines       map[string][]string `json:"lines,omitempty"`  // informational lines per section (no deltas)
	Notes       []string            `json:"notes,omitempty"`  // omitted sections and other caveats, never silent
	Seeded      bool                `json:"seeded,omitempty"` // built from history tables for a week with no stored snapshot
}

// New returns an empty payload for a week.
func New(weekStart string, generated time.Time) *Payload {
	return &Payload{Version: SchemaVersion, WeekStart: weekStart, GeneratedAt: generated.UTC().Format(time.RFC3339),
		Metrics: map[string]float64{}, Texts: map[string]string{}, Sets: map[string][]string{}, Lines: map[string][]string{}}
}

// WeekStart is the Monday (YYYY-MM-DD) of the ISO week containing t, in loc.
func WeekStart(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	back := (int(t.Weekday()) + 6) % 7 // Monday = 0
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -back)
	return d.Format("2006-01-02")
}

// Series is one numeric metric compared with the previous snapshot.
type Series struct {
	Key   string
	Cur   float64
	Prev  *float64
	Delta *float64
	Trend []float64 // up to the last 4 values, oldest first, ending with Cur
}

// TextChange is a categorical value that changed.
type TextChange struct{ Key, From, To string }

// SetChange is a set whose membership changed.
type SetChange struct {
	Key            string
	Added, Removed []string
}

// Report is the comparison of a payload with its history.
type Report struct {
	First       bool // no earlier snapshot at all: nothing to compare with
	PrevWeek    string
	Metrics     []Series
	TextChanges []TextChange
	SetChanges  []SetChange
	Top         []string // at most TopMax lines, ranked by a fixed rule
	ChangeCount int
	ValueNotes  map[string]ValueNote // by metric key; only used where that metric changed
}

// IsState reports whether a categorical value is a real, set state. An unset or unclassified
// value is not a state, so moving to or from it is never reported as a change.
func IsState(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "unset", "unclassified", "unknown":
		return false
	}
	return true
}

// TopMax is the length of the Top changes block.
const TopMax = 8

// ValueNote explains what sits inside a change of a value: holdings changes and capital flows
// that the history tables can show, and a plain statement of what cannot be separated.
type ValueNote struct{ Short, Long string }

// Compare compares cur with history (older payloads, newest first; stored snapshots or seeds
// that carry only the keys the history tables hold). A key absent from every older payload has
// no previous value: it is reported as new, never as a change from zero.
func Compare(cur *Payload, history []*Payload) *Report { return CompareWith(cur, history, nil) }

// CompareWith is Compare plus the value notes the caller computed for the period.
func CompareWith(cur *Payload, history []*Payload, notes map[string]ValueNote) *Report {
	r := &Report{First: len(history) == 0, ValueNotes: notes}
	if len(history) > 0 {
		r.PrevWeek = history[0].WeekStart
	}
	keys := make([]string, 0, len(cur.Metrics))
	for k := range cur.Metrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := Series{Key: k, Cur: cur.Metrics[k]}
		var older []float64 // newest first
		for _, h := range history {
			if v, ok := h.Metrics[k]; ok {
				older = append(older, v)
			}
		}
		if len(older) > 0 {
			p := older[0]
			d := s.Cur - p
			s.Prev, s.Delta = &p, &d
		}
		n := len(older)
		if n > 3 {
			n = 3
		}
		for i := n - 1; i >= 0; i-- {
			s.Trend = append(s.Trend, older[i])
		}
		s.Trend = append(s.Trend, s.Cur)
		r.Metrics = append(r.Metrics, s)
	}

	tkeys := make([]string, 0, len(cur.Texts))
	for k := range cur.Texts {
		tkeys = append(tkeys, k)
	}
	sort.Strings(tkeys)
	for _, k := range tkeys {
		for _, h := range history {
			if v, ok := h.Texts[k]; ok {
				if v != cur.Texts[k] && IsState(v) && IsState(cur.Texts[k]) {
					r.TextChanges = append(r.TextChanges, TextChange{Key: k, From: v, To: cur.Texts[k]})
				}
				break
			}
		}
	}

	skeys := make([]string, 0, len(cur.Sets))
	for k := range cur.Sets {
		skeys = append(skeys, k)
	}
	sort.Strings(skeys)
	for _, k := range skeys {
		for _, h := range history {
			if prev, ok := h.Sets[k]; ok {
				add, rem := diffSets(prev, cur.Sets[k])
				if len(add) > 0 || len(rem) > 0 {
					r.SetChanges = append(r.SetChanges, SetChange{Key: k, Added: add, Removed: rem})
				}
				break
			}
		}
	}

	r.Top = topChanges(r)
	r.ChangeCount = len(r.TextChanges)
	for _, sc := range r.SetChanges {
		r.ChangeCount += len(sc.Added) + len(sc.Removed)
	}
	for _, s := range r.Metrics {
		if s.Delta != nil && math.Abs(*s.Delta) > 1e-9 {
			r.ChangeCount++
		}
	}
	return r
}

func diffSets(prev, cur []string) (added, removed []string) {
	p := map[string]bool{}
	for _, x := range prev {
		p[x] = true
	}
	c := map[string]bool{}
	for _, x := range cur {
		c[x] = true
		if !p[x] {
			added = append(added, x)
		}
	}
	for _, x := range prev {
		if !c[x] {
			removed = append(removed, x)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return
}

// topChanges ranks changes by a fixed rule: (1) regime and thesis-score changes, (2) set
// changes (watchlist entering/leaving the entry zone, gaps opened/closed, theses locked,
// re-scores due, top-5 holdings), (3) the largest numeric movers by relative size
// (|delta| / max(|previous|, 1)). Ties break on key, so the same inputs give the same lines.
func topChanges(r *Report) []string {
	type item struct {
		tier  int
		score float64
		key   string
		line  string
	}
	var items []item
	for _, t := range r.TextChanges {
		items = append(items, item{0, 1e18, t.Key, fmt.Sprintf("%s: %s → %s", Label(t.Key), t.From, t.To)})
	}
	for _, s := range r.Metrics {
		if s.Delta == nil || math.Abs(*s.Delta) < 1e-9 {
			continue
		}
		if strings.HasPrefix(s.Key, "thesis.score.") {
			items = append(items, item{0, math.Abs(*s.Delta), s.Key, fmt.Sprintf("%s: %s → %s", Label(s.Key), FormatNumber(s.Key, *s.Prev), FormatNumber(s.Key, s.Cur))})
			continue
		}
		rel := math.Abs(*s.Delta) / math.Max(math.Abs(*s.Prev), 1)
		line := fmt.Sprintf("%s: %s → %s (%s)", Label(s.Key), FormatNumber(s.Key, *s.Prev), FormatNumber(s.Key, s.Cur), FormatDelta(s.Key, *s.Delta))
		if n, ok := r.ValueNotes[s.Key]; ok && n.Short != "" {
			line += " — " + n.Short
		}
		items = append(items, item{2, rel, s.Key, line})
	}
	for _, sc := range r.SetChanges {
		for _, a := range sc.Added {
			items = append(items, item{1, 0, sc.Key + "+" + a, fmt.Sprintf("%s: %s %s", Label(sc.Key), a, setVerb(sc.Key, true))})
		}
		if sc.Key == "watchlist.in_zone_no_thesis" {
			continue // only a ticker ENTERING the state is worth a Top-changes line
		}
		for _, a := range sc.Removed {
			items = append(items, item{1, 0, sc.Key + "-" + a, fmt.Sprintf("%s: %s %s", Label(sc.Key), a, setVerb(sc.Key, false))})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.score != b.score {
			return a.score > b.score
		}
		return a.key < b.key
	})
	var out []string
	for i := 0; i < len(items) && i < TopMax; i++ {
		out = append(out, items[i].line)
	}
	return out
}

func setVerb(key string, added bool) string {
	switch key {
	case "watchlist.in_zone":
		if added {
			return "entered the entry zone"
		}
		return "left the entry zone"
	case "watchlist.in_zone_no_thesis":
		if added {
			return "is in the entry zone without a thesis"
		}
		return "no longer in the entry zone without a thesis"
	case "gaps.open":
		if added {
			return "new gap"
		}
		return "gap closed"
	case "theses.locked":
		if added {
			return "newly locked"
		}
		return "no longer locked"
	case "theses.rescore_due":
		if added {
			return "re-score due"
		}
		return "re-score no longer due"
	case "portfolio.top5":
		if added {
			return "entered the top 5 holdings"
		}
		return "left the top 5 holdings"
	}
	if added {
		return "added"
	}
	return "removed"
}
