package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// FGHistoricalPoint is one daily Fear & Greed reading. Used by the
// v1.20 crypto-indicators backfill to populate historical snapshots
// from alternative.me's full archive (data exists from 2018-02-01).
type FGHistoricalPoint struct {
	Date  time.Time
	Value int
}

// FetchFearGreedHistory pulls N days of Fear & Greed Index history from
// alternative.me. limit=0 means "all available" (~3000+ days back to
// early 2018). Returned in chronological order (oldest first).
//
// API shape:
//
//	{
//	  "name": "Fear and Greed Index",
//	  "data": [
//	    {"value":"22","value_classification":"Extreme Fear","timestamp":"1716595200","time_until_update":"…"},
//	    ...
//	  ]
//	}
func FetchFearGreedHistory(ctx context.Context, days int) ([]FGHistoricalPoint, error) {
	if days < 0 {
		days = 0
	}
	url := fmt.Sprintf("https://api.alternative.me/fng/?limit=%d&format=json", days)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FT/1.20 (+https://ft.curranhouse.dev)")
	cl := &http.Client{Timeout: 60 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alt.me F&G fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("alt.me F&G HTTP %d: %s", resp.StatusCode, string(body))
	}
	var raw struct {
		Data []struct {
			Value     string `json:"value"`
			Timestamp string `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("alt.me F&G decode: %w", err)
	}
	out := make([]FGHistoricalPoint, 0, len(raw.Data))
	for _, d := range raw.Data {
		v, err := strconv.Atoi(d.Value)
		if err != nil {
			continue
		}
		tsSec, err := strconv.ParseInt(d.Timestamp, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, FGHistoricalPoint{
			Date:  time.Unix(tsSec, 0).UTC(),
			Value: v,
		})
	}
	// alt.me returns newest-first; reverse to oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
