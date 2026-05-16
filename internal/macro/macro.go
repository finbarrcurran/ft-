// Package macro loads hand-curated economic-calendar JSON files at startup
// (one per year). Used by Spec 9b D11 to render upcoming + recent macro
// cards on the News tab.
//
// Calendar refresh cadence: quarterly. Spec 7 (diagnostics, later) will
// surface "no events defined for current quarter" warnings when this slips.

package macro

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed calendar-*.json
var calendarsFS embed.FS

// Event is one row in the calendar.
type Event struct {
	Date  string `json:"date"`  // ISO YYYY-MM-DD
	Label string `json:"label"` // human readable, e.g. "US CPI (May)"
	Kind  string `json:"kind"`  // "cpi" | "fomc" | "nfp" | "pce" | "gdp" | ...
	URL   string `json:"url"`
}

type yearFile struct {
	Year   int     `json:"year"`
	Events []Event `json:"events"`
}

var (
	mu       sync.RWMutex
	all      []Event
	loadedAt time.Time
)

// Load reads every embedded calendar JSON. Bad files log a warning and are
// skipped. Safe to call multiple times.
func Load() error {
	entries, err := calendarsFS.ReadDir(".")
	if err != nil {
		return err
	}
	combined := []Event{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := calendarsFS.ReadFile(e.Name())
		if err != nil {
			slog.Warn("macro: read failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		var y yearFile
		if err := json.Unmarshal(raw, &y); err != nil {
			slog.Warn("macro: parse failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		combined = append(combined, y.Events...)
	}
	sort.Slice(combined, func(i, j int) bool { return combined[i].Date < combined[j].Date })
	mu.Lock()
	all = combined
	loadedAt = time.Now().UTC()
	mu.Unlock()
	slog.Info("macro calendar loaded", "events", len(combined))
	return nil
}

// Upcoming returns events strictly in the future, capped at `withinDays`
// (default 14). Returns ascending by date.
func Upcoming(withinDays int) []Event {
	if withinDays <= 0 {
		withinDays = 14
	}
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, withinDays).Format("2006-01-02")
	today := now.Format("2006-01-02")
	mu.RLock()
	defer mu.RUnlock()
	out := []Event{}
	for _, e := range all {
		if e.Date >= today && e.Date <= cutoff {
			out = append(out, e)
		}
	}
	return out
}

// Recent returns events that happened within the past `withinDays` (default 7).
// Returns descending by date (most recent first).
func Recent(withinDays int) []Event {
	if withinDays <= 0 {
		withinDays = 7
	}
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, -withinDays).Format("2006-01-02")
	today := now.Format("2006-01-02")
	mu.RLock()
	defer mu.RUnlock()
	out := []Event{}
	for _, e := range all {
		if e.Date >= cutoff && e.Date < today {
			out = append(out, e)
		}
	}
	// reverse for desc
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// LoadedAt returns the last load timestamp. Useful for diagnostics.
func LoadedAt() time.Time {
	mu.RLock()
	defer mu.RUnlock()
	return loadedAt
}

// DaysUntil returns "+N" days until the event from `now` (negative = past).
func DaysUntil(eventDate string, now time.Time) (int, error) {
	t, err := time.Parse("2006-01-02", eventDate)
	if err != nil {
		return 0, fmt.Errorf("bad date %q: %w", eventDate, err)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	delta := t.Sub(today)
	return int(delta.Hours() / 24), nil
}
