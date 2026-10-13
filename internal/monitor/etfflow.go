package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"ft/internal/cryptoindicators/providers"
	"ft/internal/marketdata"
)

// FarsideFile is where the nightly Playwright scraper writes the ETF-flow data.
const FarsideFile = "/var/lib/ft/data/farside/etf-flow.json"

// scraperMaxQuiet is how long the scraper may go without a successful run before the
// alert blames the scraper rather than the source (it runs nightly at 00:25 UTC).
const scraperMaxQuiet = 30 * time.Hour

// ETFFlowState is the freshness of the ETF-flow data as the monitor and Diagnostics see it.
type ETFFlowState struct {
	AsOf           string    `json:"asOf"`           // newest row date in the file
	AgeTradingDays int       `json:"ageTradingDays"` // US trading days since AsOf
	Level          string    `json:"level"`          // "", "amber", "red"
	ScraperLastOK  time.Time `json:"scraperLastOk"`  // the file's own fetchedAt (last successful scrape)
	ScraperOK      bool      `json:"scraperOk"`      // scraper succeeded within scraperMaxQuiet
}

// ReadETFFlowState reads the scraper file and judges the DATA's date, never the file's mtime.
func ReadETFFlowState(path string, now time.Time) (*ETFFlowState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		FetchedAt time.Time `json:"fetchedAt"`
		Rows      []struct {
			Date   string  `json:"date"`
			TotalM float64 `json:"totalM"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	rows := make([]providers.FlowRow, 0, len(f.Rows))
	for _, r := range f.Rows {
		rows = append(rows, providers.FlowRow{Date: r.Date, TotalM: r.TotalM})
	}
	newest := providers.EffectiveAsOf(rows) // a trailing zero placeholder row is ignored
	if newest == "" {
		return nil, fmt.Errorf("no rows in %s", path)
	}
	age, err := marketdata.USTradingDaysSince(newest, now)
	if err != nil {
		return nil, err
	}
	return &ETFFlowState{
		AsOf: newest, AgeTradingDays: age, Level: marketdata.StaleLevel(age),
		ScraperLastOK: f.FetchedAt, ScraperOK: now.Sub(f.FetchedAt) <= scraperMaxQuiet,
	}, nil
}

// etfFlowEval is the monitor rule: alert at >= 3 US trading days old (amber/red are display
// levels; the monitor's single alert state starts at amber), and name the cause: the scraper
// not succeeding versus the source not publishing a newer row.
func etfFlowEval(path string) func(ctx context.Context, now time.Time) (bool, time.Time, bool, string, error) {
	return func(_ context.Context, now time.Time) (bool, time.Time, bool, string, error) {
		st, err := ReadETFFlowState(path, now)
		if err != nil {
			if os.IsNotExist(err) {
				return true, time.Time{}, false, "scraper output file not found (" + path + "): the nightly scraper has never succeeded here", nil
			}
			return false, time.Time{}, false, "", err
		}
		asOfT, _ := time.Parse("2006-01-02", st.AsOf)
		detail := fmt.Sprintf("newest ETF-flow row is %s (%d US trading days old)", st.AsOf, st.AgeTradingDays)
		if st.AgeTradingDays < 3 {
			return false, asOfT, true, detail, nil
		}
		if !st.ScraperOK {
			detail += fmt.Sprintf("; cause: SCRAPER failed, its last successful run was %s", st.ScraperLastOK.UTC().Format("2006-01-02 15:04 UTC"))
		} else {
			detail += fmt.Sprintf("; cause: SOURCE not updated, the scraper ran fine (%s) but Farside has no newer row", st.ScraperLastOK.UTC().Format("2006-01-02 15:04 UTC"))
		}
		return true, asOfT, true, detail, nil
	}
}
