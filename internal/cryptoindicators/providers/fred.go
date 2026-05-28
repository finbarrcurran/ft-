// Package providers fetches the raw inputs for Spec 9e indicators.
// Each provider returns a fetched reading + a 4-week trend (where
// computable) + an optional fetch_error string. Caller (refresher.go)
// runs the scoring engine on the reading and upserts into
// crypto_indicators.
//
// Provider conventions:
//   - All providers respect ctx cancellation
//   - All providers return (value=nil, trend=nil, err=non-nil) on failure
//     — the caller logs to crypto_indicators.fetch_error and skips the
//     indicator from the composite for that day.
//   - All HTTP calls have an explicit 20s timeout.
//   - All providers are stateless — no in-package caching. The DB row
//     IS the cache.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Reading is the standardised return shape for every provider.
type Reading struct {
	Value   *float64
	Trend4w *float64 // signed % change over ~28 days, computed from history when available
	Err     string   // fetch_error string for the crypto_indicators row
}

// FREDClient pulls observations from https://api.stlouisfed.org/fred/series.
type FREDClient struct {
	APIKey string
	HTTP   *http.Client
}

// NewFREDClient constructs a client. APIKey must be set; empty key
// causes Fetch to return an explanatory error.
func NewFREDClient(apiKey string) *FREDClient {
	return &FREDClient{
		APIKey: apiKey,
		HTTP:   &http.Client{Timeout: 20 * time.Second},
	}
}

// FRED observations response shape.
type fredObservation struct {
	Date  string `json:"date"`
	Value string `json:"value"` // "." for missing
}
type fredResponse struct {
	Observations []fredObservation `json:"observations"`
}

// FetchSeries returns the latest non-missing observation + 4-week trend
// for a FRED series. Trend is signed % change over ~28 days for
// series whose level is bounded away from zero (DGS2, DTWEXBGS).
//
// For series that oscillate around zero (CFNAI), percentage change is
// meaningless — a tiny absolute movement near zero produces a huge %.
// Call FetchSeriesAbsoluteTrend for those.
func (c *FREDClient) FetchSeries(ctx context.Context, seriesID string) Reading {
	return c.fetchSeries(ctx, seriesID, false)
}

// FetchSeriesAbsoluteTrend is the variant for around-zero series. Trend
// is the raw delta (latest - 28d_ago), not a percentage.
func (c *FREDClient) FetchSeriesAbsoluteTrend(ctx context.Context, seriesID string) Reading {
	return c.fetchSeries(ctx, seriesID, true)
}

// FREDHistoricalPoint is one (date, value) observation, used by the v1.20
// backfill to populate historical snapshots from FRED.
type FREDHistoricalPoint struct {
	Date  time.Time
	Value float64
}

// FetchHistoricalSeries returns ALL non-missing observations for a FRED
// series from `start` to now, sorted chronologically (oldest first).
// Used by the v1.20 crypto-indicators backfill to fill historical
// snapshots from FRED's full archive (DXY back to 2006, DGS2 back to
// 1976, CFNAI back to 1967 — we cap at ~3 years for sanity).
func (c *FREDClient) FetchHistoricalSeries(ctx context.Context, seriesID string, start time.Time) ([]FREDHistoricalPoint, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("FRED_API_KEY not set")
	}
	q := url.Values{}
	q.Set("series_id", seriesID)
	q.Set("api_key", c.APIKey)
	q.Set("file_type", "json")
	q.Set("observation_start", start.Format("2006-01-02"))
	q.Set("sort_order", "asc")
	q.Set("limit", "5000")
	u := "https://api.stlouisfed.org/fred/series/observations?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("FRED fetch %s: %w", seriesID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("FRED %s HTTP %d", seriesID, resp.StatusCode)
	}
	var data fredResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("FRED decode %s: %w", seriesID, err)
	}
	out := make([]FREDHistoricalPoint, 0, len(data.Observations))
	for _, o := range data.Observations {
		if o.Value == "" || o.Value == "." {
			continue
		}
		v, err := strconv.ParseFloat(o.Value, 64)
		if err != nil {
			continue
		}
		t, err := time.Parse("2006-01-02", o.Date)
		if err != nil {
			continue
		}
		out = append(out, FREDHistoricalPoint{Date: t, Value: v})
	}
	return out, nil
}

func (c *FREDClient) fetchSeries(ctx context.Context, seriesID string, absoluteTrend bool) Reading {
	if c.APIKey == "" {
		return Reading{Err: "FRED_API_KEY not set on server"}
	}
	// Pull last 60 observations (more than enough for daily series over
	// 4 weeks; weekly/monthly series like DTWEXBGS gets ~14 months of
	// history at no extra cost).
	q := url.Values{}
	q.Set("series_id", seriesID)
	q.Set("api_key", c.APIKey)
	q.Set("file_type", "json")
	q.Set("sort_order", "desc")
	q.Set("limit", "60")
	u := "https://api.stlouisfed.org/fred/series/observations?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Reading{Err: fmt.Sprintf("FRED request build: %v", err)}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Reading{Err: fmt.Sprintf("FRED fetch %s: %v", seriesID, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Reading{Err: fmt.Sprintf("FRED %s HTTP %d", seriesID, resp.StatusCode)}
	}
	var data fredResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Reading{Err: fmt.Sprintf("FRED decode %s: %v", seriesID, err)}
	}
	if len(data.Observations) == 0 {
		return Reading{Err: fmt.Sprintf("FRED %s: empty observations", seriesID)}
	}

	// Parse + filter out missing values ("."). Keep order: latest first.
	type ob struct {
		date  time.Time
		value float64
	}
	parsed := make([]ob, 0, len(data.Observations))
	for _, o := range data.Observations {
		if o.Value == "" || o.Value == "." {
			continue
		}
		v, err := strconv.ParseFloat(o.Value, 64)
		if err != nil {
			continue
		}
		t, err := time.Parse("2006-01-02", o.Date)
		if err != nil {
			continue
		}
		parsed = append(parsed, ob{date: t, value: v})
	}
	if len(parsed) == 0 {
		return Reading{Err: fmt.Sprintf("FRED %s: no valid observations", seriesID)}
	}
	// Sort latest-first.
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].date.After(parsed[j].date) })

	latest := parsed[0]
	out := Reading{Value: &latest.value}

	// Find the observation nearest to 28 days before the latest.
	target := latest.date.Add(-28 * 24 * time.Hour)
	var prior *ob
	for i := range parsed {
		if !parsed[i].date.After(target) {
			prior = &parsed[i]
			break
		}
	}
	if prior != nil {
		var trend float64
		if absoluteTrend {
			trend = latest.value - prior.value
		} else if prior.value != 0 {
			trend = (latest.value - prior.value) / prior.value * 100
		} else {
			return out
		}
		out.Trend4w = &trend
	}
	return out
}
