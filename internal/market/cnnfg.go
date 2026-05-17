package market

import (
	"context"
	"fmt"
	"ft/internal/health"
	"time"
)

// FetchStockFearGreed pulls CNN's stock-market Fear & Greed index.
//
// Endpoint: https://production.dataviz.cnn.io/index/fearandgreed/graphdata
// Unofficial — CNN ships this for their website's gauge. No API key required
// but the endpoint rejects requests without a browser-like User-Agent. We
// reuse the same UA the Yahoo crumb path uses.
//
// Response shape (abridged):
//
//	{
//	  "fear_and_greed": {
//	    "score": 45.0,
//	    "rating": "fear",
//	    "timestamp": "2026-05-15T12:34:56+0000",
//	    "previous_close": 42.5,
//	    "previous_1_week": 38.1,
//	    ...
//	  },
//	  "fear_and_greed_historical": { ... }
//	}
//
// Some snapshots use `fear_and_greed.now.value` instead of `fear_and_greed.score`
// — we tolerate both.
func FetchStockFearGreed(ctx context.Context) (fg *FearGreed, retErr error) {
	defer func() { health.Record(ctx, "cnn_feargreed", retErr) }()
	var resp struct {
		FearAndGreed struct {
			Score  *float64 `json:"score"`
			Rating string   `json:"rating"`
			Now    struct {
				Value          *float64 `json:"value"`
				Classification string   `json:"value_classification"`
			} `json:"now"`
			Timestamp string `json:"timestamp"`
		} `json:"fear_and_greed"`
	}
	if err := httpGetJSON(ctx, "https://production.dataviz.cnn.io/index/fearandgreed/graphdata", &resp); err != nil {
		return nil, err
	}

	// Prefer top-level score+rating; fall back to nested now.* shape.
	var value int
	var classification string
	switch {
	case resp.FearAndGreed.Score != nil:
		value = int(*resp.FearAndGreed.Score + 0.5) // round
		classification = titleCase(resp.FearAndGreed.Rating)
	case resp.FearAndGreed.Now.Value != nil:
		value = int(*resp.FearAndGreed.Now.Value + 0.5)
		classification = resp.FearAndGreed.Now.Classification
	default:
		return nil, fmt.Errorf("cnn: no score/value in response")
	}

	if value < 0 || value > 100 {
		return nil, fmt.Errorf("cnn: value %d out of range", value)
	}

	return &FearGreed{
		Value:          value,
		Classification: classification,
		FetchedAt:      time.Now().UTC(),
	}, nil
}

// titleCase upper-cases the first letter only — CNN's "rating" field comes back
// lowercase ("fear", "extreme fear", etc.); UI matches alternative.me's title-
// cased convention.
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	out := make([]byte, 0, len(s))
	upperNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case upperNext && c >= 'a' && c <= 'z':
			out = append(out, c-32)
			upperNext = false
		case c == ' ':
			out = append(out, c)
			upperNext = true
		default:
			out = append(out, c)
			upperNext = false
		}
	}
	return string(out)
}
