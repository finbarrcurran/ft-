package visser46

import (
	"context"
	"errors"
	"fmt"
	"ft/internal/market"
	"ft/internal/store"
	"io"
	"log/slog"
	"sort"
	"time"
)

// IndexKey is the only index Part B serves.
const IndexKey = "visser46"

// Thresholds.
const (
	BackfillDays    = 365 // days asked of market_chart, and closes that mean "already backfilled"
	ThinCloses      = 200 // a token with fewer closes than this after backfill is flagged
	StockSkipCloses = 250 // about a year of trading sessions
)

// Service holds Part B's pieces. Zero values of the optional fields use production defaults.
type Service struct {
	Store *store.Store
	Gate  *Gate
	C     *Client

	Interval  time.Duration // refresh interval, default 30 minutes
	Gap       time.Duration // gap between backfill calls, default 2.5 s
	Now       func() time.Time
	StockBars func(ctx context.Context, ticker, rng string) ([]market.OHLCBar, error) // default Yahoo
}

// New builds the production service.
func New(st *store.Store) *Service {
	g := &Gate{S: st}
	return &Service{Store: st, Gate: g, C: &Client{Gate: g}}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) stockBars(ctx context.Context, t, rng string) ([]market.OHLCBar, error) {
	if s.StockBars != nil {
		return s.StockBars(ctx, t, rng)
	}
	return market.FetchYahooDailyBars(ctx, t, rng)
}

func (s *Service) members(ctx context.Context) (tokens, stocks []store.CryptoIndexMember, err error) {
	all, err := s.Store.ListCryptoIndexMembers(ctx, IndexKey)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range all {
		switch {
		case m.AssetKind == "token" && m.CoinGeckoID != nil && *m.CoinGeckoID != "":
			tokens = append(tokens, m)
		case m.AssetKind == "stock" && m.StockTicker != nil && *m.StockTicker != "":
			stocks = append(stocks, m)
		}
	}
	return tokens, stocks, nil
}

// ---- Backfill ----------------------------------------------------------------------------------

// BackfillOptions controls Backfill.
type BackfillOptions struct {
	DryRun bool
	Out    io.Writer // progress lines; nil = silent
}

// BackfillReport is what a backfill did (or, for a dry run, would do).
type BackfillReport struct {
	DryRun          bool
	PlannedCalls    int      // CoinGecko calls needed (tokens below 365 closes)
	TokensAttempted int      // tokens whose call was started (0 in a dry run); a retried 429 is counted by the call counters, not here
	TokensSkipped   []string // already had >= 365 closes
	StocksPlanned   []string
	StocksDone      []string
	Thin            []string // "SYMBOL (n closes)" below ThinCloses after the backfill (or before, in a dry run)
	Failed          []string
	StoppedBy       error // the gate refused partway (kill switch / soft stop)
}

func (s *Service) gap() time.Duration {
	if s.Gap > 0 {
		return s.Gap
	}
	return 2500 * time.Millisecond
}

func say(w io.Writer, format string, a ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", a...)
	}
}

// Backfill fetches a year of daily closes for each token of the index below 365 stored closes, one keyed
// market_chart call each, 2.5 s apart, and a year of closes for the stocks through Yahoo. A dry run prints
// the plan and makes no call of any kind.
func (s *Service) Backfill(ctx context.Context, opt BackfillOptions) (*BackfillReport, error) {
	tokens, stocks, err := s.members(ctx)
	if err != nil {
		return nil, err
	}
	rep := &BackfillReport{DryRun: opt.DryRun}
	type job struct {
		m   store.CryptoIndexMember
		has int
	}
	var jobs []job
	for _, m := range tokens {
		n, _, err := s.Store.CountPriceHistory(ctx, m.Symbol, "crypto")
		if err != nil {
			return nil, err
		}
		if n >= BackfillDays {
			rep.TokensSkipped = append(rep.TokensSkipped, m.Symbol)
			continue
		}
		jobs = append(jobs, job{m, n})
	}
	rep.PlannedCalls = len(jobs)
	for _, m := range stocks {
		n, _, err := s.Store.CountPriceHistory(ctx, *m.StockTicker, "stock")
		if err != nil {
			return nil, err
		}
		if n < StockSkipCloses {
			rep.StocksPlanned = append(rep.StocksPlanned, *m.StockTicker)
		}
	}

	if opt.DryRun {
		say(opt.Out, "DRY RUN: no call is made.")
		say(opt.Out, "kill switch: %v, key configured: %v", onOff(s.Store.PartBEnabled(ctx)), market.CoinGeckoKeyed())
		for _, j := range jobs {
			say(opt.Out, "  would call GET /coins/%s/market_chart?vs_currency=usd&days=%d&interval=daily  (%s, %d closes now)", *j.m.CoinGeckoID, BackfillDays, j.m.Symbol, j.has)
			if j.has < ThinCloses {
				rep.Thin = append(rep.Thin, fmt.Sprintf("%s (%d closes now)", j.m.Symbol, j.has))
			}
		}
		for _, t := range rep.StocksPlanned {
			say(opt.Out, "  would fetch %s daily bars from Yahoo (stock history path), not a CoinGecko call", t)
		}
		say(opt.Out, "skipped (>= %d closes already): %v", BackfillDays, rep.TokensSkipped)
		say(opt.Out, "CoinGecko calls it would make: %d keyed market_chart; Yahoo stock fetches: %d; gap %s => about %s", rep.PlannedCalls, len(rep.StocksPlanned), s.gap(), (time.Duration(rep.PlannedCalls) * s.gap()).Round(time.Second))
		return rep, nil
	}

	today := s.now().Format("2006-01-02")
	for i, j := range jobs {
		if i > 0 {
			select {
			case <-ctx.Done():
				return rep, ctx.Err()
			case <-time.After(s.gap()):
			}
		}
		rep.TokensAttempted++
		closes, err := s.C.MarketChart(ctx, *j.m.CoinGeckoID, BackfillDays)
		if err != nil {
			if isGateErr(err) {
				rep.StoppedBy = err
				say(opt.Out, "stopped before %s: %v", j.m.Symbol, err)
				rep.TokensAttempted-- // refused before the call, not made
				break
			}
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", j.m.Symbol, err))
			say(opt.Out, "FAILED %s: %v", j.m.Symbol, err)
			continue
		}
		pts := make([]store.PricePoint, 0, len(closes))
		for _, c := range closes {
			if c.Date == today { // today's point is an intraday price, not a close; the daily close job writes it
				continue
			}
			pts = append(pts, store.PricePoint{Date: c.Date, Close: c.Close})
		}
		if err := s.Store.InsertPriceHistoryBatch(ctx, j.m.Symbol, "crypto", pts); err != nil {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: write: %v", j.m.Symbol, err))
			continue
		}
		n, _, _ := s.Store.CountPriceHistory(ctx, j.m.Symbol, "crypto")
		say(opt.Out, "ok %s: %d closes stored", j.m.Symbol, n)
		if n < ThinCloses {
			rep.Thin = append(rep.Thin, fmt.Sprintf("%s (%d closes)", j.m.Symbol, n))
		}
	}

	// Stocks: the existing stock history path (Yahoo daily bars). Kill switch applies; the keyed budget does not.
	for _, t := range rep.StocksPlanned {
		if err := s.Gate.KillSwitchOnly(ctx); err != nil {
			rep.StoppedBy = err
			break
		}
		bars, err := s.stockBars(ctx, t, "2y")
		if err != nil {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %v", t, err))
			continue
		}
		cutoff := s.now().AddDate(0, 0, -BackfillDays).Format("2006-01-02")
		pts := []store.PricePoint{}
		for _, b := range bars {
			if b.Date >= cutoff && b.Date < today {
				pts = append(pts, store.PricePoint{Date: b.Date, Close: b.Close})
			}
		}
		if err := s.Store.InsertPriceHistoryBatch(ctx, t, "stock", pts); err != nil {
			rep.Failed = append(rep.Failed, fmt.Sprintf("%s: write: %v", t, err))
			continue
		}
		rep.StocksDone = append(rep.StocksDone, t)
		say(opt.Out, "ok %s (stock): %d closes", t, len(pts))
	}
	sort.Strings(rep.Thin)
	return rep, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func isGateErr(err error) bool {
	for _, g := range []error{ErrKillSwitchOff, ErrSoftStop, ErrNoKey} {
		if errors.Is(err, g) {
			return true
		}
	}
	return false
}

// ---- Refresh -----------------------------------------------------------------------------------

// RefreshOnce makes ONE keyed /coins/markets call for every token of the index and stores the snapshot.
// Ids CoinGecko does not return are logged and keep their previous row; nothing is filled in.
func (s *Service) RefreshOnce(ctx context.Context) (stored int, missing []string, err error) {
	tokens, _, err := s.members(ctx)
	if err != nil {
		return 0, nil, err
	}
	ids := make([]string, 0, len(tokens))
	idSym := map[string]string{}
	for _, m := range tokens {
		ids = append(ids, *m.CoinGeckoID)
		idSym[*m.CoinGeckoID] = m.Symbol
	}
	rows, err := s.C.Markets(ctx, ids)
	if err != nil {
		return 0, nil, err
	}
	at := s.now().Unix()
	snaps := make([]store.IndexSnapshot, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		sym, ok := idSym[r.ID]
		if !ok {
			continue
		}
		seen[r.ID] = true
		snaps = append(snaps, store.IndexSnapshot{CoinGeckoID: r.ID, Symbol: sym, FetchedAt: at,
			PriceUSD: r.Price, MarketCap: r.MarketCap, MCapRank: r.Rank, Chg24h: r.D24, Chg7d: r.D7, Chg30d: r.D30})
	}
	for _, id := range ids {
		if !seen[id] {
			missing = append(missing, idSym[id])
		}
	}
	if err := s.Store.UpsertIndexSnapshots(ctx, snaps); err != nil {
		return 0, missing, err
	}
	return len(snaps), missing, nil
}

func (s *Service) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return 30 * time.Minute
}

// RunRefreshLoop refreshes about every Interval until ctx ends. The next run is due Interval after the newest
// stored snapshot, so a service restart does not add a call. Each wake asks the gate (inside RefreshOnce)
// before the call; with the switch off the loop only reads one preference per interval.
func (s *Service) RunRefreshLoop(ctx context.Context) {
	for {
		wait := s.interval()
		if last, err := s.Store.NewestIndexSnapshotAt(ctx); err == nil && last > 0 {
			if due := time.Unix(last, 0).Add(s.interval()).Sub(s.now()); due > 0 && due < wait {
				wait = due
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		n, missing, err := s.RefreshOnce(cctx)
		cancel()
		switch {
		case err == nil:
			slog.Info("visser46 refresh", "stored", n, "missing", missing)
		case isGateErr(err):
			slog.Debug("visser46 refresh skipped", "why", err)
		default:
			slog.Warn("visser46 refresh failed", "err", err)
		}
	}
}

// ---- Daily close -------------------------------------------------------------------------------

// CloseResult is one daily-close run.
type CloseResult struct {
	Date          string
	Appended      int
	MissingTokens []string // no snapshot from this UTC day: listed, never filled
	StocksUpdated []string
	StockErrors   []string
}

// DailyClose appends today's (UTC) close for each token from the stored snapshot: no CoinGecko call. CoinGecko's
// daily points are the 00:00 UTC price, so this runs shortly after 00:00 UTC and only accepts a snapshot
// fetched since 00:00 UTC of that day; a token without one is listed, not filled. It is NOT gated by the soft
// stop (that is how the close "continues"); with the refresh stopped, though, the snapshot goes stale and tokens
// are listed as missing. Stocks: when the kill switch is on, the last sessions' closes are refreshed from Yahoo
// (the existing stock path; not CoinGecko).
func (s *Service) DailyClose(ctx context.Context) (*CloseResult, error) {
	now := s.now()
	day := now.Format("2006-01-02")
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()
	res := &CloseResult{Date: day}
	tokens, stocks, err := s.members(ctx)
	if err != nil {
		return nil, err
	}
	snaps, err := s.Store.ListIndexSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range tokens {
		sn, ok := snaps[*m.CoinGeckoID]
		if !ok || sn.FetchedAt < dayStart || sn.FetchedAt > now.Unix() || sn.PriceUSD == nil || *sn.PriceUSD <= 0 {
			res.MissingTokens = append(res.MissingTokens, m.Symbol)
			continue
		}
		if err := s.Store.InsertPriceHistoryBatch(ctx, m.Symbol, "crypto", []store.PricePoint{{Date: day, Close: *sn.PriceUSD}}); err != nil {
			return res, err
		}
		res.Appended++
	}
	if len(stocks) > 0 && s.Gate.KillSwitchOnly(ctx) == nil {
		cutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
		for _, m := range stocks {
			bars, err := s.stockBars(ctx, *m.StockTicker, "1mo")
			if err != nil {
				res.StockErrors = append(res.StockErrors, fmt.Sprintf("%s: %v", *m.StockTicker, err))
				continue
			}
			pts := []store.PricePoint{}
			for _, b := range bars {
				if b.Date >= cutoff && b.Date < day { // completed sessions only
					pts = append(pts, store.PricePoint{Date: b.Date, Close: b.Close})
				}
			}
			if err := s.Store.InsertPriceHistoryBatch(ctx, *m.StockTicker, "stock", pts); err != nil {
				res.StockErrors = append(res.StockErrors, fmt.Sprintf("%s: write: %v", *m.StockTicker, err))
				continue
			}
			res.StocksUpdated = append(res.StocksUpdated, *m.StockTicker)
		}
	}
	return res, nil
}
