package marketdata

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

//go:embed holidays/*.json
var holidaysFS embed.FS

// holidaySet is exchange → date ("YYYY-MM-DD") → name. Populated at init.
type holidayEntry struct {
	Date string `json:"date"`
	Name string `json:"name"`
}
type holidayFile struct {
	Exchange string         `json:"exchange"`
	Year     int            `json:"year"`
	Closures []holidayEntry `json:"closures"`
	HalfDays []holidayEntry `json:"half_days"` // reserved; treated as full closed in v1
}

var (
	holidayMu   sync.RWMutex
	holidaySet  = map[string]map[string]string{} // exchange → date → name
	holidaysOK  bool
)

// LoadHolidays reads every embedded holidays/*.json. Bad files log a warning
// and are skipped. Safe to call multiple times.
//
// Called from main() at startup. Returns the count loaded per exchange.
func LoadHolidays() error {
	entries, err := holidaysFS.ReadDir("holidays")
	if err != nil {
		return fmt.Errorf("read holidays dir: %w", err)
	}
	next := map[string]map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := holidaysFS.ReadFile("holidays/" + e.Name())
		if err != nil {
			slog.Warn("holidays: read failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		var f holidayFile
		if err := json.Unmarshal(raw, &f); err != nil {
			slog.Warn("holidays: parse failed; skipping", "file", e.Name(), "err", err)
			continue
		}
		exch := strings.ToUpper(strings.TrimSpace(f.Exchange))
		if exch == "" {
			slog.Warn("holidays: empty exchange; skipping", "file", e.Name())
			continue
		}
		if next[exch] == nil {
			next[exch] = map[string]string{}
		}
		for _, c := range f.Closures {
			if c.Date == "" {
				continue
			}
			next[exch][c.Date] = c.Name
		}
		// half_days treated as full closures for v1 per spec.
		for _, c := range f.HalfDays {
			next[exch][c.Date] = c.Name + " (half day)"
		}
	}
	holidayMu.Lock()
	holidaySet = next
	holidaysOK = true
	holidayMu.Unlock()

	counts := map[string]int{}
	for ex, m := range next {
		counts[ex] = len(m)
	}
	slog.Info("holidays loaded", "byExchange", counts)
	return nil
}

// IsHoliday returns true (and the human name) if the date is a market closure
// for the given exchange. Date is "YYYY-MM-DD". Case-insensitive on exchange.
func IsHoliday(exchange, date string) (string, bool) {
	holidayMu.RLock()
	defer holidayMu.RUnlock()
	m := holidaySet[strings.ToUpper(exchange)]
	if m == nil {
		return "", false
	}
	name, ok := m[date]
	return name, ok
}
