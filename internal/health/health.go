// Package health is the choke point for provider-health tracking (Spec 7).
//
// Providers in internal/market, internal/news, etc. call health.Record after
// every external HTTP call. The package-level recorder is injected at boot
// (cmd/ft/main.go) — keeping providers free of any internal/store import
// dependency.
//
// Design:
//   - Init() registers a Recorder (typically *store.Store).
//   - Record(ctx, provider, err) is a no-op until Init runs, then writes
//     synchronously. SQLite UPSERT is fast enough that we don't bother
//     batching.
//   - Errors from Record itself are swallowed (slog warn) — provider-health
//     bookkeeping must never break the calling code path.
package health

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
)

// Recorder is the minimal store-shaped interface health depends on. The real
// *store.Store satisfies it.
type Recorder interface {
	RecordProviderHealth(ctx context.Context, provider string, ok bool, errMsg string) error
}

var recorder atomic.Pointer[Recorder]

// Init wires the recorder. Safe to call multiple times.
func Init(r Recorder) {
	recorder.Store(&r)
}

// Record persists one provider call's outcome. err==nil → success.
// No-op if Init hasn't been called (e.g. unit tests).
func Record(ctx context.Context, provider string, err error) {
	rp := recorder.Load()
	if rp == nil {
		return
	}
	r := *rp
	if r == nil {
		return
	}
	if err == nil {
		if e := r.RecordProviderHealth(ctx, provider, true, ""); e != nil {
			slog.Warn("provider_health record failed", "provider", provider, "err", e)
		}
		return
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if e := r.RecordProviderHealth(ctx, provider, false, msg); e != nil {
		slog.Warn("provider_health record failed", "provider", provider, "err", e)
	}
}

// EndpointRecorder counts CoinGecko calls per endpoint (Jordi-46 Part B). The real *store.Store satisfies it.
type EndpointRecorder interface {
	RecordCoinGeckoCall(ctx context.Context, endpoint, feature string, keyed bool, ok bool, is429 bool) error
}

var endpointRecorder atomic.Pointer[EndpointRecorder]

// InitEndpoint wires the per-endpoint recorder. Safe to call multiple times.
func InitEndpoint(r EndpointRecorder) { endpointRecorder.Store(&r) }

// CoinGeckoEndpoint names the endpoint of a CoinGecko URL: markets, market_chart, global, simple_price, or
// other (coin lookups and anything new).
func CoinGeckoEndpoint(rawURL string) string {
	path := rawURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case strings.HasSuffix(path, "/coins/markets"):
		return "markets"
	case strings.HasSuffix(path, "/market_chart") || strings.Contains(path, "/market_chart/"):
		return "market_chart"
	case strings.HasSuffix(path, "/global") || strings.Contains(path, "/global/"):
		return "global"
	case strings.HasSuffix(path, "/simple/price"):
		return "simple_price"
	}
	return "other"
}

// RecordCoinGecko counts one CoinGecko attempt under its endpoint. No-op until InitEndpoint runs.
func RecordCoinGecko(ctx context.Context, rawURL string, keyed bool, err error) {
	rp := endpointRecorder.Load()
	if rp == nil || *rp == nil {
		return
	}
	is429 := err != nil && strings.Contains(err.Error(), "http 429")
	if e := (*rp).RecordCoinGeckoCall(ctx, CoinGeckoEndpoint(rawURL), FeatureOf(ctx), keyed, err == nil, is429); e != nil {
		slog.Warn("coingecko call record failed", "err", e)
	}
}

type featureKey struct{}

// WithFeature tags CoinGecko calls made with the returned context with a feature name ("partb_refresh",
// "partb_backfill"), so the per-endpoint counters can report added calls by feature.
func WithFeature(ctx context.Context, feature string) context.Context {
	return context.WithValue(ctx, featureKey{}, feature)
}

// FeatureOf returns the feature tag of ctx, or "" (the existing features).
func FeatureOf(ctx context.Context) string {
	f, _ := ctx.Value(featureKey{}).(string)
	return f
}
