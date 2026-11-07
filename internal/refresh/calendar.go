package refresh

// SC-56 — report (earnings) dates for every holding, stock-watchlist row and live thesis.
//
// The state machine in applyCalendar is pure (no database, no network) so fixtures can drive it.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"ft/internal/market"
	"ft/internal/store"
)

// CalendarSource is where report dates come from. Production uses Yahoo; tests use fixtures.
type CalendarSource interface {
	// Events is the primary source (quoteSummary calendarEvents).
	Events(ctx context.Context, symbol string) (*market.CalendarEvents, error)
	// EarningsPage is the second source, asked only for the NEXT date when the primary one has passed or is missing.
	EarningsPage(ctx context.Context, symbol string) ([]market.EarningsRow, error)
}

type yahooCalendarSource struct{}

func (yahooCalendarSource) Events(ctx context.Context, symbol string) (*market.CalendarEvents, error) {
	return market.FetchYahooCalendarEvents(ctx, symbol)
}
func (yahooCalendarSource) EarningsPage(ctx context.Context, symbol string) ([]market.EarningsRow, error) {
	return market.FetchYahooEarningsPage(ctx, symbol)
}

// errSecondBudget: this run already used its share of second-source page fetches (each page is about 0.8 MB).
var errSecondBudget = errors.New("second-source budget for this run is used")

// secondPerRun caps second-source page fetches per hourly run; the rest wait for the next run.
const secondPerRun = 8

const (
	sourceCalendar = "yahoo_calendar"
	sourcePage     = "yahoo_earnings_page"
	secondEvery    = 24 * time.Hour
	secondRetry    = 2 * time.Hour
)

// CalendarSummary is what one pass did, for the hourly log line and the tests.
type CalendarSummary struct {
	Total      int
	Fetched    int // provider answered
	Failed     []string
	NotUpdated []string // not reached before the context ended
	ByState    map[string]int
}

func (c CalendarSummary) String() string {
	var parts []string
	keys := make([]string, 0, len(c.ByState))
	for k := range c.ByState {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, c.ByState[k]))
	}
	return strings.Join(parts, " ")
}

// RefreshCalendar refreshes every ticker in the set once. It never clears a stored date on partial data and
// logs every per-ticker failure with its error.
func (s *Service) RefreshCalendar(ctx context.Context) CalendarSummary {
	src := s.Calendar
	if src == nil {
		src = yahooCalendarSource{}
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	gap := s.CalendarGap
	if gap == 0 {
		gap = 250 * time.Millisecond
	}
	sum := CalendarSummary{ByState: map[string]int{}}
	secondUsed := 0
	set, err := s.Store.CalendarTickerSet(ctx)
	if err != nil {
		slog.Error("calendar: ticker set", "err", err)
		return sum
	}
	prev, err := s.Store.ListTickerCalendar(ctx)
	if err != nil {
		slog.Error("calendar: read rows", "err", err)
		return sum
	}
	sum.Total = len(set)
	// stalest first, so a run cut short by the timeout rotates through the set
	sort.SliceStable(set, func(i, j int) bool {
		pi, pj := int64(0), int64(0)
		if p := prev[set[i].Ticker]; p != nil {
			pi = p.UpdatedAt
		}
		if p := prev[set[j].Ticker]; p != nil {
			pj = p.UpdatedAt
		}
		return pi < pj
	})
	for i, ct := range set {
		select {
		case <-ctx.Done():
			for _, rest := range set[i:] {
				sum.NotUpdated = append(sum.NotUpdated, rest.Ticker)
			}
			slog.Warn("calendar: context ended before the pass finished", "done", i, "total", len(set), "not_updated", sum.NotUpdated)
			s.finishCalendar(ctx, sum)
			return sum
		default:
		}
		t := now().UTC()
		var ev *market.CalendarEvents
		var evErr error
		if ct.NotApplicable == "" {
			ev, evErr = src.Events(ctx, ct.ProviderSymbol)
			if evErr == nil {
				sum.Fetched++
			}
			time.Sleep(gap)
		}
		c := applyCalendar(prev[ct.Ticker], ct, ev, evErr, func() ([]market.EarningsRow, error) {
			if secondUsed >= secondPerRun {
				return nil, errSecondBudget
			}
			secondUsed++
			rows, e := src.EarningsPage(ctx, ct.ProviderSymbol)
			time.Sleep(gap)
			return rows, e
		}, t)
		if err := s.Store.SaveTickerCalendar(ctx, c); err != nil {
			slog.Error("calendar: save", "ticker", ct.Ticker, "err", err)
			continue
		}
		if c.Status == store.CalFetchError {
			sum.Failed = append(sum.Failed, ct.Ticker+": "+c.LastError)
			slog.Warn("calendar: fetch failed", "ticker", ct.Ticker, "symbol", ct.ProviderSymbol, "err", c.LastError, "consecutive", c.ErrorCount)
		}
		sum.ByState[c.Status]++
	}
	s.finishCalendar(ctx, sum)
	return sum
}

func (s *Service) finishCalendar(ctx context.Context, sum CalendarSummary) {
	if err := s.Store.SyncHoldingCalendarDates(ctx); err != nil {
		slog.Error("calendar: sync holding dates", "err", err)
	}
	slog.Info("calendar refresh complete", "total", sum.Total, "fetched", sum.Fetched, "failed", len(sum.Failed),
		"not_updated", len(sum.NotUpdated), "states", sum.String())
	if s.Alerts != nil {
		s.Alerts.Tick(ctx)
	}
}

// RefreshEarningsOnly is the hourly job entry point (name kept from v1.9.0). userID is unused: the set is
// the union of holdings, watchlist and live theses.
func (s *Service) RefreshEarningsOnly(ctx context.Context, _ int64) {
	s.RefreshCalendar(ctx)
}

func strp(s string) *string { return &s }

// applyCalendar computes the new row for one ticker from the previous row and the provider's answer.
func applyCalendar(prev *store.TickerCalendar, ct store.CalendarTicker, ev *market.CalendarEvents, evErr error,
	second func() ([]market.EarningsRow, error), now time.Time) *store.TickerCalendar {

	today := now.UTC().Format("2006-01-02")
	unix := now.Unix()
	var c store.TickerCalendar
	hadRow := prev != nil && prev.Status != store.CalUnknown
	if prev != nil {
		c = *prev
	}
	c.Ticker, c.ProviderSymbol, c.UpdatedAt = ct.Ticker, ct.ProviderSymbol, unix
	if c.Status == "" {
		c.Status = store.CalUnknown
	}

	// Not applicable by flag (ETFs, delisted names, warrants): no provider call at all.
	if ct.NotApplicable != "" {
		c.Status, c.NotApplicableWhy = store.CalNotApplicable, ct.NotApplicable
		c.EarningsDate, c.IsEstimate = nil, false
		c.LastError, c.ErrorCount, c.ErrorPermanent = "", 0, false
		return &c
	}
	c.NotApplicableWhy = ""

	if evErr != nil {
		var he *market.HTTPError
		c.LastError = evErr.Error()
		c.ErrorCount++
		c.ErrorPermanent = errors.As(evErr, &he) && he.Status == 404
		c.Status = store.CalFetchError // stored dates are left as they were
		return &c
	}

	c.LastError, c.ErrorCount, c.ErrorPermanent = "", 0, false
	fetched := unix
	c.FetchedAt = &fetched
	if ev.ExDividend != "" {
		c.ExDividendDate = strp(ev.ExDividend) // never cleared by an answer that lacks it
	}

	// Yahoo may return a range [start, end]; use the earliest date that has not passed, else the latest past one.
	future, past := "", ""
	for _, d := range ev.EarningsDates {
		if d >= today {
			if future == "" || d < future {
				future = d
			}
		} else if d > past {
			past = d
		}
	}

	setNext := func(date string, estimate bool, source string) {
		old := ""
		if c.EarningsDate != nil {
			old = *c.EarningsDate
		}
		if hadRow && old != date {
			if old != "" {
				c.PrevEarningsDate = strp(old)
			} else {
				c.PrevEarningsDate = nil
			}
			changed := unix
			c.DateChangedAt = &changed
		}
		c.EarningsDate, c.IsEstimate, c.Source = strp(date), estimate, source
		if estimate {
			c.Status = store.CalDateEstimated
		} else {
			c.Status = store.CalDateKnown
		}
	}
	markReported := func(date string) {
		if date != "" && (c.LastReportedDate == nil || date > *c.LastReportedDate) {
			c.LastReportedDate = strp(date)
		}
	}
	trySecond := func() (string, bool) {
		if second == nil {
			return "", false
		}
		if c.SecondCheckedAt != nil && now.Sub(time.Unix(*c.SecondCheckedAt, 0)) < secondEvery {
			return "", false
		}
		rows, err := second()
		chk := unix
		if errors.Is(err, errSecondBudget) {
			return "", false // not attempted; the next run will try
		}
		if err != nil {
			retry := unix - int64(secondEvery/time.Second) + int64(secondRetry/time.Second)
			c.SecondCheckedAt = &retry
			slog.Warn("calendar: second source failed", "ticker", ct.Ticker, "err", err)
			return "", false
		}
		c.SecondCheckedAt = &chk
		next := ""
		for _, r := range rows {
			if r.Reported {
				markReported(r.Date)
				continue
			}
			if r.Date >= today && (next == "" || r.Date < next) {
				next = r.Date
			}
		}
		return next, next != ""
	}

	// A stored next date that has itself passed since the last refresh becomes the reported date, whatever
	// the provider says now.
	if c.EarningsDate != nil && *c.EarningsDate < today {
		markReported(*c.EarningsDate)
		c.EarningsDate, c.IsEstimate, c.Status = nil, false, store.CalReported
	}

	switch {
	case future != "":
		setNext(future, ev.IsEstimate, sourceCalendar)
		// The calendar only names the next date. For a thesis ticker whose last report is unknown, read the
		// page once (budgeted) for the reported rows: revision_needed depends on them. Its next date is ignored.
		if c.LastReportedDate == nil && ct.Thesis {
			_, _ = trySecond()
		}

	case past != "":
		// Yahoo is still returning the date that has passed: remember it as reported, look for the next one.
		markReported(past)
		if d, ok := trySecond(); ok {
			setNext(d, true, sourcePage)
		} else if c.EarningsDate != nil && *c.EarningsDate >= today {
			// a second source we could not reach must not hide a still-valid stored date
		} else {
			c.EarningsDate, c.IsEstimate, c.Source, c.Status = nil, false, sourceCalendar, store.CalReported
		}

	default: // Yahoo answered with no earnings date
		if c.EarningsDate != nil && *c.EarningsDate >= today {
			// partial data (for example an ex-dividend-only answer): never clear a stored future date
			break
		}
		if d, ok := trySecond(); ok {
			setNext(d, true, sourcePage)
		} else if c.Status == store.CalReported {
			// still waiting for the next date after a report
		} else {
			c.EarningsDate, c.IsEstimate, c.Source, c.Status = nil, false, sourceCalendar, store.CalNotAnnounced
		}
	}

	return &c
}
