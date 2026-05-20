package providers

import (
	"context"
	"ft/internal/market"
)

// FearGreed wraps the existing internal/market.FetchFearGreed so the
// indicator refresher has a single Reading-shaped surface. Avoids
// duplicating the alternative.me HTTP call that's already proven.
func FetchFearGreed(ctx context.Context) Reading {
	fg, err := market.FetchFearGreed(ctx)
	if err != nil || fg == nil {
		msg := "fear-greed: nil response"
		if err != nil {
			msg = "fear-greed: " + err.Error()
		}
		return Reading{Err: msg}
	}
	v := float64(fg.Value)
	return Reading{Value: &v}
}
