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
