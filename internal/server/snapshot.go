package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ft/internal/cryptoindicators/freshness"
	"ft/internal/metrics"
	"ft/internal/monitor"
	"ft/internal/snapshot"
)

// SC-46 / P5 — the weekly FT snapshot. Everything is read through the SAME in-process handlers
// the connector tools call (callHandlerJSON), so the numbers cannot differ from what the
// connector returns at the same moment. A section whose source fails is omitted with a note;
// nothing is invented.

// SnapshotDir is where the rendered file is written for the Drive transport
// (ops/bridge/ft-bridge-snapshot pushes it to FT-Bridge/reports/ft-snapshot.md).
const SnapshotDir = "/var/lib/ft/data/bridge/snapshot"

// SnapshotOptions control one run.
type SnapshotOptions struct {
	DryRun       bool
	WeekOverride string // YYYY-MM-DD Monday; used by the Monday catch-up for the week that just ended
	OutDir       string // default SnapshotDir
	Notify       monitor.Notifier
}

// SnapshotResult is what a run produced.
type SnapshotResult struct {
	Payload  *snapshot.Payload
	Report   *snapshot.Report
	Markdown string
	Seeded   []string // keys that came from history tables for weeks with no stored snapshot
	Saved    bool
}

func dublin() *time.Location {
	if l, err := time.LoadLocation("Europe/Dublin"); err == nil {
		return l
	}
	return time.UTC
}

func dig(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func fnum(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func fstr(v any) string {
	s, _ := v.(string)
	return s
}

func arr(m map[string]any, key string) []any {
	a, _ := m[key].([]any)
	return a
}

func firstArrayLen(m map[string]any) (int, bool) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if a, ok := m[k].([]any); ok {
			return len(a), true
		}
	}
	return 0, false
}

// BuildSnapshot gathers the live state into a payload.
func (s *Server) BuildSnapshot(ctx context.Context, weekStart string, now time.Time) (*snapshot.Payload, error) {
	if s.demoModeOn(ctx) {
		return nil, errors.New("demo mode is on: the handlers return a synthetic book, so no snapshot is taken")
	}
	const userID = int64(1)
	p := snapshot.New(weekStart, now)
	omit := func(section string, err error) {
		p.Notes = append(p.Notes, fmt.Sprintf("Section omitted — %s: %v", section, err))
		slog.Warn("snapshot: section omitted", "section", section, "err", err)
	}
	call := func(path string, q url.Values, h http.HandlerFunc) (map[string]any, error) {
		out, err := s.callHandlerJSON(ctx, userID, path, q, h)
		if err != nil {
			return nil, err
		}
		if st, _ := out["_httpStatus"].(int); st != 0 && st != 200 {
			return nil, fmt.Errorf("%s answered HTTP %d", path, st)
		}
		return out, nil
	}

	// --- Portfolio (the Summary KPIs) ---
	if sum, err := call("/api/summary", nil, s.handleSummary); err != nil {
		omit("portfolio", err)
	} else {
		if v, ok := fnum(dig(sum, "kpis", "totalValue")); ok {
			p.Metrics["portfolio.total_value_usd"] = v
		}
		if v, ok := fnum(dig(sum, "kpis", "cashUsd")); ok {
			p.Metrics["portfolio.cash_usd"] = v
		}
		for _, l := range arr(dig(sum, "legends").(map[string]any), "assetClass") {
			m, _ := l.(map[string]any)
			if v, ok := fnum(m["value"]); ok {
				switch fstr(m["label"]) {
				case "Stocks":
					p.Metrics["portfolio.stocks_value_usd"] = v
				case "Crypto":
					p.Metrics["portfolio.crypto_value_usd"] = v
				}
			}
		}
		ns, _ := fnum(dig(sum, "counts", "stocks"))
		nc, _ := fnum(dig(sum, "counts", "crypto"))
		p.Metrics["portfolio.holdings_count"] = ns + nc
		s.snapshotTop5(ctx, userID, p, omit)
	}

	// --- Performance (eToro, USD) ---
	if perf, err := call("/api/etoro/performance", nil, s.handleEtoroPerformance); err != nil {
		omit("performance", err)
	} else {
		ytdYear := 0
		for _, y := range arr(perf, "years") {
			ym, _ := y.(map[string]any)
			if b, _ := ym["isYtd"].(bool); !b {
				continue
			}
			ytdYear = int(func() float64 { f, _ := fnum(ym["year"]); return f }())
			var disc, cp float64
			for _, a := range arr(ym, "assets") {
				am, _ := a.(map[string]any)
				d, _ := fnum(am["realisedDiscUsd"])
				c, _ := fnum(am["realisedCopyUsd"])
				disc += d
				cp += c
			}
			get := func(k string) float64 { f, _ := fnum(ym[k]); return f }
			p.Metrics["performance.ytd_realised_disc_usd"] = disc
			p.Metrics["performance.ytd_realised_copy_usd"] = cp
			p.Metrics["performance.ytd_realised_usd"] = get("realisedPnlUsd")
			p.Metrics["performance.ytd_dividends_usd"] = get("dividendsUsd")
			p.Metrics["performance.ytd_fees_usd"] = get("feesUsd")
			p.Metrics["performance.ytd_income_other_usd"] = get("interestUsd") + get("refundsUsd") + get("otherIncomeUsd")
			p.Metrics["performance.ytd_net_usd"] = get("netUsd")
		}
		if e := fstr(dig(perf, "coverage", "end")); e != "" {
			p.Texts["performance.coverage_end"] = e
		}
		if ytdYear == 0 {
			p.Notes = append(p.Notes, "Performance: no year-to-date statement data is stored; trade statistics omitted.")
		} else {
			q := url.Values{"scope": {"discretionary"}, "year": {fmt.Sprint(ytdYear)}}
			if ts, err := call("/api/etoro/trade-stats", q, s.handleEtoroTradeStats); err != nil {
				omit("performance trade statistics", err)
			} else if st, ok := ts["stats"].(map[string]any); ok {
				n, _ := fnum(st["n"])
				p.Metrics["performance.stats_n"] = n
				if v, ok := fnum(st["winRate"]); ok {
					p.Metrics["performance.stats_win_rate_pct"] = v * 100
				}
				if v, ok := fnum(st["profitFactor"]); ok {
					p.Metrics["performance.stats_profit_factor"] = v
				}
				if v, ok := fnum(st["expectancy"]); ok {
					p.Metrics["performance.stats_expectancy_usd"] = v
				}
				if b, _ := st["smallSample"].(bool); b {
					p.Lines["performance"] = append(p.Lines["performance"], fmt.Sprintf("Trade statistics are a small sample (n=%d): read them with care.", int(n)))
				}
			}
		}
	}

	// --- Theses, scorecards ---
	if th, err := call("/api/theses", nil, s.handleListTheses); err != nil {
		omit("stock theses", err)
	} else {
		var locked, due []string
		for _, t := range arr(th, "theses") {
			tm, _ := t.(map[string]any)
			tk := fstr(tm["ticker"])
			if tk == "" || fstr(tm["status"]) != "locked" {
				continue
			}
			locked = append(locked, tk)
			if sc, ok := fnum(tm["score"]); ok {
				p.Metrics["thesis.score."+tk] = sc
			}
			switch fstr(tm["earningsUrgency"]) {
			case "amber", "red", "revision_needed":
				due = append(due, tk)
			}
		}
		sort.Strings(locked)
		sort.Strings(due)
		p.Metrics["theses.locked_count"] = float64(len(locked))
		p.Metrics["theses.rescore_due_count"] = float64(len(due))
		p.Sets["theses.locked"], p.Sets["theses.rescore_due"] = locked, due
		p.Notes = append(p.Notes, "Conviction bands are not stored in FT (a thesis has a score out of 16 and a locked/draft status), so score changes per thesis are shown instead of band counts.")
	}
	if ct, err := call("/api/crypto/theses", nil, s.handleCryptoThesesList); err != nil {
		omit("crypto theses", err)
	} else if n, ok := firstArrayLen(ct); ok {
		p.Metrics["theses.crypto_count"] = float64(n)
	}
	if sc, err := call("/api/scorecards", nil, s.handleScorecardsList); err != nil {
		omit("stock scorecards", err)
	} else if n, ok := firstArrayLen(sc); ok {
		p.Metrics["theses.scorecards_stock"] = float64(n)
	}
	if ca, err := call("/api/crypto/adapters", nil, s.handleCryptoAdaptersList); err != nil {
		omit("crypto scorecards", err)
	} else if n, ok := firstArrayLen(ca); ok {
		p.Metrics["theses.scorecards_crypto"] = float64(n)
	}

	// --- Gap report ---
	if g, err := call("/api/theses/gaps", nil, s.handleThesesGaps); err != nil {
		omit("gap report", err)
	} else {
		open := []string{}
		byKind := map[string]int{}
		for _, x := range arr(g, "gaps") {
			gm, _ := x.(map[string]any)
			k, tk := fstr(gm["kind"]), fstr(gm["ticker"])
			byKind[k]++
			open = append(open, k+":"+tk)
		}
		sort.Strings(open)
		for k, n := range byKind {
			p.Metrics["gaps.count."+k] = float64(n)
		}
		p.Metrics["gaps.total"] = float64(len(open))
		p.Sets["gaps.open"] = open
	}

	// --- Watchlist ---
	if w, err := call("/api/watchlist", nil, s.handleListWatchlist); err != nil {
		omit("watchlist", err)
	} else {
		in := []string{}
		items := arr(w, "watchlist")
		for _, x := range items {
			wm, _ := x.(map[string]any)
			if b, _ := wm["inRange"].(bool); b {
				in = append(in, fstr(wm["ticker"]))
			}
		}
		sort.Strings(in)
		p.Metrics["watchlist.count"] = float64(len(items))
		p.Metrics["watchlist.in_zone_count"] = float64(len(in))
		p.Sets["watchlist.in_zone"] = in
	}

	// A3: process flag, not a recommendation: a ticker in the entry zone that has no thesis.
	{
		gapTickers := map[string]bool{}
		for _, g := range p.Sets["gaps.open"] {
			if i := strings.Index(g, ":"); i >= 0 {
				gapTickers[g[i+1:]] = true
			}
		}
		if _, ok := p.Sets["gaps.open"]; ok {
			if _, ok2 := p.Sets["watchlist.in_zone"]; ok2 {
				both := []string{}
				for _, tk := range p.Sets["watchlist.in_zone"] {
					if gapTickers[tk] {
						both = append(both, tk)
					}
				}
				sort.Strings(both)
				p.Sets["watchlist.in_zone_no_thesis"] = both
			}
		}
	}

	// --- Alerts: the last 7 days by kind (notification_log), plus the live count ---
	if err := s.snapshotAlerts(ctx, p, now); err != nil {
		omit("alerts", err)
	}
	if al, err := call("/api/bot/alerts", url.Values{"only_unnotified": {"0"}}, s.handleBotAlerts); err == nil {
		bySev := map[string]int{}
		for _, a := range arr(al, "alerts") {
			am, _ := a.(map[string]any)
			sev := fstr(am["severity"])
			if sev == "" {
				sev = "unspecified"
			}
			bySev[sev]++
		}
		total := 0
		for k, n := range bySev {
			p.Metrics["alerts.current."+k] = float64(n)
			total += n
		}
		p.Metrics["alerts.current_total"] = float64(total)
	}

	// --- Regime ---
	if rg, err := call("/api/regime", nil, s.handleGetRegime); err != nil {
		omit("regime", err)
	} else {
		if v := fstr(rg["effective"]); v != "" {
			p.Texts["regime.effective"] = v
		}
		for _, fw := range []struct{ key, name string }{{"jordi", "Jordi"}, {"cowen", "Cowen"}} {
			m, _ := rg[fw.key].(map[string]any)
			value, detail, stale := regimeState(m, now)
			p.Texts["regime."+fw.key] = value
			p.Texts["regime."+fw.key+".detail"] = detail
			if stale {
				p.Lines["data_health"] = append(p.Lines["data_health"], fmt.Sprintf("%s regime: %s", fw.name, detail))
			}
		}
	}
	if mr, err := call("/api/macro/regime", nil, s.handleMacroRegime); err != nil {
		omit("macro regime", err)
	} else if b, _ := mr["hasRegime"].(bool); b {
		if v := fstr(dig(mr, "regime", "quadrant")); v != "" {
			p.Texts["regime.macro_quadrant"] = v
		}
		if v := fstr(dig(mr, "regime", "shorthand")); v != "" {
			p.Texts["regime.macro_shorthand"] = v
		}
	}

	// --- Crypto indicators with their data dates ---
	if ci, err := call("/api/crypto-indicators", url.Values{"historyDays": {"1"}}, s.handleListCryptoIndicators); err != nil {
		omit("crypto indicators", err)
	} else {
		for _, x := range arr(ci, "indicators") {
			im, _ := x.(map[string]any)
			id := fstr(im["id"])
			if v, ok := fnum(im["currentValue"]); ok && id != "" {
				p.Metrics["crypto_ind."+id] = v
				if d := fstr(im["dataAsOf"]); d != "" {
					p.Texts["crypto_ind."+id+".as_of"] = d
					if lvl := fstr(im["staleLevel"]); lvl != "" {
						p.Lines["crypto_ind"] = append(p.Lines["crypto_ind"], fmt.Sprintf("%s: data as of %s is %s (%s)", fstr(im["displayName"]), d, lvl, freshnessRuleText(id)))
					}
				}
			}
		}
	}

	// --- Data health: every monitor entry not in the ok state ---
	mon := &monitor.Monitor{DB: s.store.DB, Checks: monitor.DefaultChecks(s.store.DB)}
	bad := 0
	for _, r := range mon.Snapshot(ctx) {
		if r.Status == "ok" {
			continue
		}
		bad++
		last := "no successful run on record"
		if r.AgeSec != nil {
			last = "last good " + humanAge(*r.AgeSec) + " ago"
		}
		line := fmt.Sprintf("%s: %s, %s", r.Label, strings.ToUpper(r.Status), last)
		if r.Detail != "" {
			line += " — " + r.Detail
		}
		p.Lines["data_health"] = append(p.Lines["data_health"], line)
	}
	if bad == 0 {
		p.Lines["data_health"] = append([]string{"Monitored external sources: all ok."}, p.Lines["data_health"]...)
	}

	// --- Video digest: the latest week ---
	if vd, err := call("/api/video-digest", nil, s.handleVideoDigestList); err != nil {
		omit("video digest", err)
	} else {
		cutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
		n := 0
		for _, x := range arr(vd, "digests") {
			dm, _ := x.(map[string]any)
			if fstr(dm["publishedAt"]) < cutoff {
				continue
			}
			n++
			if n > 3 {
				break
			}
			line := fmt.Sprintf("%s — %s (%s)", fstr(dm["sourceName"]), fstr(dm["title"]), fstr(dm["publishedAt"]))
			p.Lines["video"] = append(p.Lines["video"], line)
			if sm := fstr(dm["executiveSummary"]); sm != "" {
				if len(sm) > 300 {
					sm = sm[:300] + "…"
				}
				p.Lines["video"] = append(p.Lines["video"], "  Summary: "+sm)
			}
			var themes []string
			for i, t := range arr(dm, "themes") {
				if i >= 4 {
					break
				}
				if ts, ok := t.(string); ok {
					themes = append(themes, ts)
				}
			}
			if len(themes) > 0 {
				p.Lines["video"] = append(p.Lines["video"], "  Themes: "+strings.Join(themes, "; "))
			}
			var ms []string
			for i, m := range arr(dm, "mentions") {
				if i >= 5 {
					break
				}
				mm, _ := m.(map[string]any)
				ms = append(ms, fstr(mm["name"])+" ("+fstr(mm["stance"])+")")
			}
			if len(ms) > 0 {
				p.Lines["video"] = append(p.Lines["video"], "  Mentions: "+strings.Join(ms, ", "))
			}
		}
		if n == 0 {
			p.Notes = append(p.Notes, "Video digest omitted: no digest was published in the last 7 days.")
		}
	}
	return p, nil
}

// regimeState returns the regime value, a one-line detail with its set-at date and age, and
// whether it is stale (the handler's own flag: older than 14 days). An unclassified regime is
// reported as "unset" with the date it has been unset since.
func regimeState(m map[string]any, now time.Time) (value, detail string, stale bool) {
	if m == nil {
		return "unset", "no regime record", true
	}
	value = fstr(m["regime"])
	stale, _ = m["stale"].(bool)
	age := ""
	date := ""
	if t, err := time.Parse(time.RFC3339, fstr(m["set_at"])); err == nil {
		date = t.UTC().Format("2006-01-02")
		age = fmt.Sprintf("%d days", int(now.Sub(t).Hours()/24))
	}
	if !snapshot.IsState(value) {
		value = "unset"
		if date != "" {
			return value, fmt.Sprintf("unset since %s (%s)", date, age), true
		}
		return value, "unset", true
	}
	detail = "set " + date
	if age != "" {
		detail += " (" + age + " ago"
		if src := fstr(m["source"]); src != "" {
			detail += ", " + src
		}
		detail += ")"
	}
	if stale {
		detail += " — STALE (older than 14 days)"
	}
	return value, detail, stale
}

func freshnessRuleText(id string) string {
	if r, ok := freshness.For(id); ok {
		return fmt.Sprintf("amber at %d, red at %d %s", r.Amber, r.Red, r.UnitLabel())
	}
	return "stale"
}

func humanAge(sec int64) string {
	d := time.Duration(sec) * time.Second
	if d < 2*time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 72*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// snapshotTop5 is the share of the invested book held by the five largest positions.
func (s *Server) snapshotTop5(ctx context.Context, userID int64, p *snapshot.Payload, omit func(string, error)) {
	stocks, cryptos, err := s.loadHoldings(ctx, userID)
	if err != nil {
		omit("portfolio concentration", err)
		return
	}
	type pos struct {
		name string
		v    float64
	}
	var all []pos
	total := 0.0
	for _, h := range stocks {
		if m := metrics.ComputeStock(h); m.CurrentValueUSD != nil {
			name := "?"
			if h.Ticker != nil {
				name = *h.Ticker
			}
			all = append(all, pos{name, *m.CurrentValueUSD})
			total += *m.CurrentValueUSD
		}
	}
	for _, c := range cryptos {
		if m := metrics.ComputeCrypto(c); m.CurrentValueUSD != nil {
			all = append(all, pos{c.Symbol, *m.CurrentValueUSD})
			total += *m.CurrentValueUSD
		}
	}
	if total <= 0 {
		return
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].name < all[j].name
	})
	top, sum := []string{}, 0.0
	for i := 0; i < len(all) && i < 5; i++ {
		top = append(top, all[i].name)
		sum += all[i].v
	}
	sort.Strings(top)
	p.Metrics["portfolio.top5_pct"] = sum / total * 100
	p.Sets["portfolio.top5"] = top
}

// snapshotAlerts counts the alerts raised in the 7 days up to asOf, by kind (notification_log).
func (s *Server) snapshotAlerts(ctx context.Context, p *snapshot.Payload, asOf time.Time) error {
	counts, err := alertCounts7d(ctx, s.store.DB, asOf)
	if err != nil {
		return err
	}
	total := 0.0
	for k, n := range counts {
		p.Metrics["alerts.7d."+k] = float64(n)
		total += float64(n)
	}
	p.Metrics["alerts.7d_total"] = total
	return nil
}

func alertCounts7d(ctx context.Context, db *sql.DB, asOf time.Time) (map[string]int, error) {
	to := asOf.Format("2006-01-02")
	from := asOf.AddDate(0, 0, -6).Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `SELECT alert_kind, COUNT(*) FROM notification_log WHERE alert_day >= ? AND alert_day <= ? GROUP BY alert_kind`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// RunWeeklySnapshot builds, compares, renders and (unless DryRun) saves, writes the file for the
// Drive transport and sends the one-line Telegram note.
func (s *Server) RunWeeklySnapshot(ctx context.Context, now time.Time, opt SnapshotOptions) (*SnapshotResult, error) {
	ws := opt.WeekOverride
	if ws == "" {
		ws = snapshot.WeekStart(now, dublin())
	}
	p, err := s.BuildSnapshot(ctx, ws, now)
	if err != nil {
		return nil, err
	}
	hist, seeded, err := s.snapshotHistory(ctx, ws)
	if err != nil {
		return nil, err
	}
	var notes map[string]snapshot.ValueNote
	if len(hist) > 0 {
		notes = s.valueNotes(ctx, hist[0], now)
		if ls := s.valueHistoryLines(ctx, hist[0]); len(ls) > 0 {
			p.Lines["portfolio"] = append(p.Lines["portfolio"], ls...)
			p.Lines["portfolio"] = append(p.Lines["portfolio"], "Earlier value changes above are read against these recorded holdings changes; "+flowsText+".")
		}
	}
	rep := snapshot.CompareWith(p, hist, notes)
	if len(seeded) > 0 {
		p.Notes = append(p.Notes, "Weeks before the first stored snapshot are filled only from FT's existing history tables (portfolio value, alerts raised, regimes, thesis scores and lock dates); every other metric starts at its first snapshot.")
	}
	md := snapshot.Render(p, rep)
	res := &SnapshotResult{Payload: p, Report: rep, Markdown: md, Seeded: seeded}
	if opt.DryRun {
		return res, nil
	}
	if err := snapshot.Save(ctx, s.store.DB, p, now); err != nil {
		return nil, err
	}
	res.Saved = true
	dir := opt.OutDir
	if dir == "" {
		dir = SnapshotDir
	}
	if err := writeFileAtomic(filepath.Join(dir, "ft-snapshot.md"), []byte(md)); err != nil {
		slog.Warn("snapshot: could not write the Drive file (the row is saved)", "err", err)
		p.Notes = append(p.Notes, "The Drive file could not be written: "+err.Error())
	}
	if opt.Notify != nil {
		msg := fmt.Sprintf("FT weekly snapshot ready: %d change%s", rep.ChangeCount, plural(rep.ChangeCount))
		if rep.First {
			msg = "FT weekly snapshot ready: first snapshot (no changes to compare yet)"
		}
		for i, l := range rep.Top {
			if i >= 3 {
				break
			}
			msg += "\n• " + l
		}
		if err := opt.Notify.Send(ctx, msg); err != nil {
			slog.Warn("snapshot: telegram note not sent", "err", err)
		}
	}
	return res, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RunScheduledSnapshot is what the two scheduled jobs call. mode "sunday" (Sunday evening,
// Dublin time) takes the week's snapshot; mode "catchup" (Monday morning) takes the week that
// just ended if Sunday's run did not happen. Any other day does nothing.
func (s *Server) RunScheduledSnapshot(ctx context.Context, now time.Time, mode string, notify monitor.Notifier) error {
	loc := dublin()
	ws := snapshot.WeekStart(now, loc)
	switch {
	case mode == "sunday" && now.In(loc).Weekday() == time.Sunday:
		_, err := s.RunWeeklySnapshot(ctx, now, SnapshotOptions{WeekOverride: ws, Notify: notify})
		return err
	case mode == "catchup" && now.In(loc).Weekday() == time.Monday:
		d, _ := time.Parse("2006-01-02", ws)
		prev := d.AddDate(0, 0, -7).Format("2006-01-02")
		if have, err := snapshot.Get(ctx, s.store.DB, prev); err != nil {
			return err
		} else if have != nil {
			return nil
		}
		slog.Warn("snapshot: Sunday run missed; taking the catch-up snapshot", "week", prev)
		_, err := s.RunWeeklySnapshot(ctx, now, SnapshotOptions{WeekOverride: prev, Notify: notify})
		return err
	}
	return nil
}
