package market

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// FetchFearGreed pulls the current crypto Fear & Greed Index value from
// alternative.me. No API key required.
//
// Endpoint: https://api.alternative.me/fng/
// Response:
//
//	{
//	  "data": [
//	    { "value": "42", "value_classification": "Fear", "timestamp": "...", "time_until_update": "..." }
//	  ]
//	}
func FetchFearGreed(ctx context.Context) (*FearGreed, error) {
	var resp struct {
		Data []struct {
			Value          string `json:"value"`
			Classification string `json:"value_classification"`
			Timestamp      string `json:"timestamp"`
		} `json:"data"`
	}
	if err := httpGetJSON(ctx, "https://api.alternative.me/fng/?limit=1", &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("alternative.me: empty data")
	}
	v, err := strconv.Atoi(resp.Data[0].Value)
	if err != nil {
		return nil, fmt.Errorf("parse value: %w", err)
	}
	return &FearGreed{
		Value:          v,
		Classification: resp.Data[0].Classification,
		FetchedAt:      time.Now().UTC(),
	}, nil
}
