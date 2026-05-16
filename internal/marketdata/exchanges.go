package marketdata

// Spec 5 — Multi-exchange model.
//
// One Exchange struct per market. NYSE/NASDAQ collapse to the "US" pseudo-
// exchange because they share hours + holidays — keeping them as one entity
// avoids duplicating the holiday calendar.
//
// Lunch breaks are explicit pairs of [start, end] local-time windows.

import "time"

// Exchange is the static definition of a trading venue: name, timezone, hours.
type Exchange struct {
	Code     string      // canonical short code: "US", "LSE", "EURONEXT", "XETRA", "TSE", "HKEX", "B3"
	Name     string      // human label for the dropdown
	TZName   string      // IANA timezone, e.g. "America/New_York"
	OpenHM   localHM     // local open as (hour, minute)
	CloseHM  localHM     // local close as (hour, minute)
	Lunch    *lunchBreak // nil if no break. Times are local to TZName.
}

// localHM is a wall-clock (hour, minute) pair used as a builder for time.Date.
type localHM struct{ H, M int }

// lunchBreak covers exchanges that pause mid-session (TSE, HKEX).
type lunchBreak struct {
	StartHM, EndHM localHM
}

// AllExchanges is the canonical ordered list. Order matters for UI dropdowns
// (top bar pill shows them in this order when none are open).
var AllExchanges = []Exchange{
	{
		Code: "US", Name: "US Markets", TZName: "America/New_York",
		OpenHM: localHM{9, 30}, CloseHM: localHM{16, 0},
	},
	{
		Code: "LSE", Name: "London", TZName: "Europe/London",
		OpenHM: localHM{8, 0}, CloseHM: localHM{16, 30},
	},
	{
		Code: "EURONEXT", Name: "Euronext", TZName: "Europe/Paris",
		OpenHM: localHM{9, 0}, CloseHM: localHM{17, 30},
	},
	{
		Code: "XETRA", Name: "Xetra (Frankfurt)", TZName: "Europe/Berlin",
		OpenHM: localHM{9, 0}, CloseHM: localHM{17, 30},
	},
	{
		Code: "TSE", Name: "Tokyo", TZName: "Asia/Tokyo",
		OpenHM: localHM{9, 0}, CloseHM: localHM{15, 0},
		Lunch:  &lunchBreak{StartHM: localHM{11, 30}, EndHM: localHM{12, 30}},
	},
	{
		Code: "HKEX", Name: "Hong Kong", TZName: "Asia/Hong_Kong",
		OpenHM: localHM{9, 30}, CloseHM: localHM{16, 0},
		Lunch:  &lunchBreak{StartHM: localHM{12, 0}, EndHM: localHM{13, 0}},
	},
	{
		Code: "B3", Name: "São Paulo (B3)", TZName: "America/Sao_Paulo",
		OpenHM: localHM{10, 0}, CloseHM: localHM{17, 0},
	},
}

// FindExchange returns a pointer to the canonical Exchange for `code`, or
// nil if unknown. Case-insensitive.
func FindExchange(code string) *Exchange {
	for i := range AllExchanges {
		if equalFold(AllExchanges[i].Code, code) {
			return &AllExchanges[i]
		}
	}
	return nil
}

// Loc returns the Exchange's IANA location, falling back to UTC if the system
// is missing tzdata.
func (e Exchange) Loc() *time.Location {
	loc, err := time.LoadLocation(e.TZName)
	if err != nil {
		return time.UTC
	}
	return loc
}

// dateAt constructs `Y-M-D HH:MM:00` in the exchange's timezone.
func (e Exchange) dateAt(t time.Time, hm localHM) time.Time {
	local := t.In(e.Loc())
	return time.Date(local.Year(), local.Month(), local.Day(), hm.H, hm.M, 0, 0, e.Loc())
}

// equalFold without bringing in unicode/strings cost. Simple ASCII compare.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'a' && ca <= 'z' {
			ca -= 32
		}
		if cb >= 'a' && cb <= 'z' {
			cb -= 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}
