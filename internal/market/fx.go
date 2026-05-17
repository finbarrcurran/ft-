package market

import (
	"context"
	"ft/internal/health"
	"time"
)

// FetchEURUSD pulls the EUR→USD rate from Frankfurter.
// Returns a *FXRate, never panics. Caller decides what to do on err (fallback
// to last-known stored snapshot).
func FetchEURUSD(ctx context.Context) (rate *FXRate, retErr error) {
	defer func() { health.Record(ctx, "frankfurter", retErr) }()
	var resp struct {
		Amount float64            `json:"amount"`
		Base   string             `json:"base"`
		Date   string             `json:"date"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := httpGetJSON(ctx, "https://api.frankfurter.app/latest?from=EUR&to=USD", &resp); err != nil {
		return nil, err
	}
	r, ok := resp.Rates["USD"]
	if !ok || r <= 0 {
		// Frankfurter delivered something unexpected; return a static fallback.
		return &FXRate{EURToUSD: 1.08, FetchedAt: time.Now().UTC()}, nil
	}
	return &FXRate{EURToUSD: r, FetchedAt: time.Now().UTC()}, nil
}
