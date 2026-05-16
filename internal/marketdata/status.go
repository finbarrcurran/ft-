package marketdata

import "time"

// MarketStatus is the unified shape returned by Status() and surfaced on the
// holdings table + top bar (Spec 5 D3/D4).
//
// `NextChangeKind` is one of: "open", "close", "break_start", "break_end".
// `LocalName` carries a short label for the timezone abbreviation (e.g. "ET",
// "BST", "JST"). Client uses NextChange (ISO) to compute the countdown in
// browser-local time per spec D5.
type MarketStatus struct {
	Exchange       string    `json:"exchange"`       // "US", "LSE", …
	Name           string    `json:"name"`           // human label
	Open           bool      `json:"open"`           // strict: open = trading right now (false during lunch)
	OnBreak        bool      `json:"onBreak"`        // true only during a lunch window
	NextChange     time.Time `json:"nextChange"`     // ISO UTC; client converts to local
	NextChangeKind string    `json:"nextChangeKind"` // "open" | "close" | "break_start" | "break_end"
	TZName         string    `json:"tzName"`         // IANA, for client local time label if desired
}

// Status returns the trading state of `exchangeCode` at `now` (UTC or any TZ).
// Unknown exchange → zero MarketStatus, all-zero NextChange.
func Status(exchangeCode string, now time.Time) MarketStatus {
	e := FindExchange(exchangeCode)
	if e == nil {
		return MarketStatus{}
	}
	return e.Status(now)
}

// Status on the Exchange itself. Same logic; cleaner call site.
func (e Exchange) Status(now time.Time) MarketStatus {
	loc := e.Loc()
	local := now.In(loc)

	out := MarketStatus{
		Exchange: e.Code,
		Name:     e.Name,
		TZName:   e.TZName,
	}

	// Closed for the day (weekend or holiday)?
	if isDayClosed(local, e.Code) {
		out.Open = false
		out.NextChange = nextOpenFor(e, local)
		out.NextChangeKind = "open"
		return out
	}

	openT := e.dateAt(local, e.OpenHM)
	closeT := e.dateAt(local, e.CloseHM)

	// Before the bell.
	if local.Before(openT) {
		out.Open = false
		out.NextChange = openT
		out.NextChangeKind = "open"
		return out
	}

	// After the close.
	if !local.Before(closeT) {
		out.Open = false
		out.NextChange = nextOpenFor(e, local)
		out.NextChangeKind = "open"
		return out
	}

	// Lunch break check (TSE, HKEX).
	if e.Lunch != nil {
		breakStart := e.dateAt(local, e.Lunch.StartHM)
		breakEnd := e.dateAt(local, e.Lunch.EndHM)
		// Before break: open, next change is break_start.
		if local.Before(breakStart) {
			out.Open = true
			out.NextChange = breakStart
			out.NextChangeKind = "break_start"
			return out
		}
		// During break: closed (onBreak), next change is break_end.
		if local.Before(breakEnd) {
			out.Open = false
			out.OnBreak = true
			out.NextChange = breakEnd
			out.NextChangeKind = "break_end"
			return out
		}
	}

	// Open, next change is close.
	out.Open = true
	out.NextChange = closeT
	out.NextChangeKind = "close"
	return out
}

// isDayClosed reports weekend OR holiday for the given exchange.
// `local` should be a time already converted into the exchange's TZ.
func isDayClosed(local time.Time, exchangeCode string) bool {
	switch local.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	}
	_, ok := IsHoliday(exchangeCode, local.Format("2006-01-02"))
	return ok
}

// nextOpenFor walks forward day-by-day until it finds a trading day, then
// returns OpenHM on that day in the exchange's local TZ.
func nextOpenFor(e Exchange, after time.Time) time.Time {
	loc := e.Loc()
	// Start at today's open (in exchange-local). Bump forward as needed.
	cand := time.Date(after.Year(), after.Month(), after.Day(), e.OpenHM.H, e.OpenHM.M, 0, 0, loc)
	for !cand.After(after) || isDayClosed(cand, e.Code) {
		cand = cand.AddDate(0, 0, 1)
		// Re-anchor to OpenHM in case AddDate crossed a DST boundary.
		cand = time.Date(cand.Year(), cand.Month(), cand.Day(), e.OpenHM.H, e.OpenHM.M, 0, 0, loc)
	}
	return cand
}

// AllStatuses returns a slice in AllExchanges order. Used by the top-bar pill
// dropdown.
func AllStatuses(now time.Time) []MarketStatus {
	out := make([]MarketStatus, 0, len(AllExchanges))
	for _, e := range AllExchanges {
		out = append(out, e.Status(now))
	}
	return out
}
