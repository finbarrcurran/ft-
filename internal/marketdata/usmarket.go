// Spec 2 D5 originally shipped a US-only top-bar pill. Spec 5 generalises
// the engine — this file now keeps the legacy `USMarketStatus(now) USStatus`
// shape working by delegating to the generic Status("US", now) so existing
// callers (handleMarketStatus → /api/marketstatus) don't break.
//
// New code should call marketdata.Status / marketdata.AllStatuses directly.

package marketdata

import "time"

// USStatus is the original Spec 2 shape. Kept verbatim for back-compat.
type USStatus struct {
	Open           bool      `json:"open"`
	NextChange     time.Time `json:"nextChange"`
	NextChangeKind string    `json:"nextChangeKind"`
}

// USMarketStatus returns the legacy US-only shape. Internally a thin shim
// over Status("US", now).
func USMarketStatus(now time.Time) USStatus {
	s := Status("US", now)
	return USStatus{
		Open:           s.Open,
		NextChange:     s.NextChange,
		NextChangeKind: s.NextChangeKind,
	}
}
