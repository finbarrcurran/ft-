package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"ft/internal/snapshot"
	"ft/internal/store"
)

// snapshotHistory returns the three weeks before weekStart, newest first: the stored snapshot
// where there is one, otherwise a SEED holding only the metrics that existing history tables
// really hold (portfolio value, alerts raised, regimes, thesis scores and lock dates). Nothing
// else is reconstructed or estimated.
func (s *Server) snapshotHistory(ctx context.Context, weekStart string) ([]*snapshot.Payload, []string, error) {
	ws, err := time.Parse("2006-01-02", weekStart)
	if err != nil {
		return nil, nil, err
	}
	var out []*snapshot.Payload
	seededKeys := map[string]bool{}
	stored, err := snapshot.Before(ctx, s.store.DB, weekStart, 3)
	if err != nil {
		return nil, nil, err
	}
	have := map[string]*snapshot.Payload{}
	for _, p := range stored {
		have[p.WeekStart] = p
	}
	for k := 1; k <= 3; k++ {
		w := ws.AddDate(0, 0, -7*k)
		wk := w.Format("2006-01-02")
		if p, ok := have[wk]; ok {
			out = append(out, p)
			continue
		}
		seed, err := s.seedPayload(ctx, wk)
		if err != nil {
			return nil, nil, err
		}
		if len(seed.Metrics)+len(seed.Texts)+len(seed.Sets) > 0 {
			out = append(out, seed)
			for key := range seed.Metrics {
				seededKeys[key] = true
			}
			for key := range seed.Texts {
				seededKeys[key] = true
			}
			for key := range seed.Sets {
				seededKeys[key] = true
			}
		}
	}
	// A stored snapshot older than the three-week window still serves as "previous" when
	// nothing newer exists at all.
	if len(out) == 0 && len(stored) > 0 {
		out = append(out, stored[0])
	}
	// Order newest first by week.
	sort.SliceStable(out, func(i, j int) bool { return out[i].WeekStart > out[j].WeekStart })
	keys := make([]string, 0, len(seededKeys))
	for k := range seededKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return out, keys, nil
}

// seedPayload builds the history-table part of a past week, as of that week's Sunday.
func (s *Server) seedPayload(ctx context.Context, weekStart string) (*snapshot.Payload, error) {
	ws, _ := time.Parse("2006-01-02", weekStart)
	sunday := ws.AddDate(0, 0, 6)
	asOf := sunday.Format("2006-01-02")
	endTS := sunday.Add(24*time.Hour - time.Second).Unix()
	p := snapshot.New(weekStart, time.Now())
	p.Seeded = true
	db := s.store.DB

	var tot, st, cr sql.NullFloat64
	err := db.QueryRowContext(ctx, `SELECT total_value_usd, stocks_value_usd, crypto_value_usd FROM portfolio_value_history WHERE date <= ? ORDER BY date DESC LIMIT 1`, asOf).Scan(&tot, &st, &cr)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("seed portfolio: %w", err)
	}
	if err == nil && tot.Valid {
		p.Metrics["portfolio.total_value_usd"] = tot.Float64
		p.Metrics["portfolio.stocks_value_usd"] = st.Float64
		p.Metrics["portfolio.crypto_value_usd"] = cr.Float64
	}

	counts, err := alertCounts7d(ctx, db, sunday)
	if err != nil {
		return nil, fmt.Errorf("seed alerts: %w", err)
	}
	total := 0
	for k, n := range counts {
		p.Metrics["alerts.7d."+k] = float64(n)
		total += n
	}
	if len(counts) > 0 {
		p.Metrics["alerts.7d_total"] = float64(total)
	}

	for _, fw := range []string{"jordi", "cowen"} {
		var regime string
		err := db.QueryRowContext(ctx, `SELECT regime FROM regime_history WHERE framework_id = ? AND ts <= ? AND seed_excluded = 0 ORDER BY ts DESC LIMIT 1`, fw, endTS).Scan(&regime)
		if err == nil {
			p.Texts["regime."+fw] = regime
		} else if err != sql.ErrNoRows {
			return nil, fmt.Errorf("seed regime: %w", err)
		}
	}
	var q, sh string
	err = db.QueryRowContext(ctx, `SELECT quadrant, shorthand FROM macro_regime_history WHERE computed_at <= ? ORDER BY computed_at DESC LIMIT 1`, endTS).Scan(&q, &sh)
	if err == nil {
		p.Texts["regime.macro_quadrant"], p.Texts["regime.macro_shorthand"] = q, sh
	} else if err != sql.ErrNoRows {
		return nil, fmt.Errorf("seed macro regime: %w", err)
	}

	// Theses: per ticker the highest version already locked by that Sunday.
	rows, err := db.QueryContext(ctx, `
		SELECT t.ticker, t.score FROM theses_index t
		 WHERE t.locked_date IS NOT NULL AND t.locked_date <= ? AND t.status IN ('locked','superseded')
		   AND t.version = (SELECT MAX(v.version) FROM theses_index v WHERE v.ticker = t.ticker AND v.locked_date IS NOT NULL AND v.locked_date <= ? AND v.status IN ('locked','superseded'))`, asOf, asOf)
	if err != nil {
		return nil, fmt.Errorf("seed theses: %w", err)
	}
	defer rows.Close()
	locked := []string{}
	for rows.Next() {
		var tk string
		var sc sql.NullInt64
		if err := rows.Scan(&tk, &sc); err != nil {
			return nil, err
		}
		locked = append(locked, tk)
		if sc.Valid {
			p.Metrics["thesis.score."+tk] = float64(sc.Int64)
		}
	}
	if len(locked) > 0 {
		sort.Strings(locked)
		p.Sets["theses.locked"] = locked
		p.Metrics["theses.locked_count"] = float64(len(locked))
	}
	return p, rows.Err()
}

// prevInstant is when the previous snapshot was taken: its stored time, or for a seeded week the
// Sunday evening it stands for.
func prevInstant(prev *snapshot.Payload) time.Time {
	if !prev.Seeded {
		if t, err := time.Parse(time.RFC3339, prev.GeneratedAt); err == nil {
			return t
		}
	}
	ws, _ := time.Parse("2006-01-02", prev.WeekStart)
	return ws.AddDate(0, 0, 6).Add(18 * time.Hour)
}

type auditAgg struct {
	removals, adds, adjusts int
	removedUSD, addedUSD    float64
	netAdjustUSD            float64
	tickers                 map[string][]string
	estimated               bool
}

// auditWindow aggregates the holdings changes recorded in (from, to], by holding kind.
func (s *Server) auditWindow(ctx context.Context, from, to time.Time) map[string]*auditAgg {
	agg := map[string]*auditAgg{"stock": {tickers: map[string][]string{}}, "crypto": {tickers: map[string][]string{}}}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT holding_kind, COALESCE(ticker, symbol, ''), action, changes_json FROM holdings_audit WHERE ts > ? AND ts <= ? AND action IN ('create','soft_delete','restore','update') ORDER BY ts`, from.Unix(), to.Unix())
	if err != nil {
		return agg
	}
	defer rows.Close()
	for rows.Next() {
		var kind, tk, action, raw string
		if err := rows.Scan(&kind, &tk, &action, &raw); err != nil {
			return agg
		}
		a := agg[kind]
		if a == nil {
			continue
		}
		var ch map[string]any
		_ = json.Unmarshal([]byte(raw), &ch)
		if b, _ := ch["tsEstimated"].(bool); b {
			a.estimated = true
		}
		switch action {
		case "soft_delete":
			a.removals++
			a.removedUSD += numOr(ch["ftInvestedUsd"])
			a.tickers["removed"] = append(a.tickers["removed"], tk)
		case "create", "restore":
			a.adds++
			if nw, ok := ch["new"].(map[string]any); ok {
				a.addedUSD += numOr(nw["investedUsd"])
			}
			a.tickers["added"] = append(a.tickers["added"], tk)
		case "update":
			if inv, ok := ch["investedUsd"].(map[string]any); ok {
				a.adjusts++
				a.netAdjustUSD += numOr(inv["to"]) - numOr(inv["from"])
				a.tickers["adjusted"] = append(a.tickers["adjusted"], tk)
			}
		}
	}
	return agg
}

// describeAgg renders the recorded holdings changes (empty string when there are none).
func describeAgg(as ...*auditAgg) string {
	var rem, add, adj int
	var remU, addU, netU float64
	est := false
	var rt, at, jt []string
	for _, a := range as {
		rem += a.removals
		add += a.adds
		adj += a.adjusts
		remU += a.removedUSD
		addU += a.addedUSD
		netU += a.netAdjustUSD
		est = est || a.estimated
		rt = append(rt, a.tickers["removed"]...)
		at = append(at, a.tickers["added"]...)
		jt = append(jt, a.tickers["adjusted"]...)
	}
	if rem+add+adj == 0 {
		return ""
	}
	var parts []string
	if rem > 0 {
		parts = append(parts, fmt.Sprintf("%d removal%s (%s invested: %s)", rem, plural(rem), snapMoney(remU), tickerList(rt)))
	}
	if add > 0 {
		parts = append(parts, fmt.Sprintf("%d addition%s (%s invested: %s)", add, plural(add), snapMoney(addU), tickerList(at)))
	}
	if adj > 0 {
		parts = append(parts, fmt.Sprintf("%d invested-amount adjustment%s (net %s: %s)", adj, plural(adj), snapMoney(netU), tickerList(jt)))
	}
	out := strings.Join(parts, "; ")
	if est {
		out += " (timestamps of backfilled rows are estimated)"
	}
	return out
}

// valueNotes explains the portfolio value changes: holdings changes recorded in holdings_audit
// since the previous snapshot, and a plain statement that capital flows are not recorded. It
// never estimates an amount: a flow or change it cannot separate is said to be not separable.
func (s *Server) valueNotes(ctx context.Context, prev *snapshot.Payload, now time.Time) map[string]snapshot.ValueNote {
	from := prevInstant(prev)
	agg := s.auditWindow(ctx, from, now)
	flows, flowsShort := s.flowsSentence(ctx, from, now)
	one := func(as ...*auditAgg) snapshot.ValueNote {
		d := describeAgg(as...)
		if d == "" {
			return snapshot.ValueNote{Short: "no holdings changes recorded; " + flowsShort,
				Long: "no holdings changes were recorded since the last snapshot; " + flows + "."}
		}
		var rem, add, adj int
		for _, a := range as {
			rem += a.removals
			add += a.adds
			adj += a.adjusts
		}
		return snapshot.ValueNote{
			Short: fmt.Sprintf("holdings changes: %d removal%s, %d addition%s, %d adjustment%s; %s", rem, plural(rem), add, plural(add), adj, plural(adj), flowsShort),
			Long:  "holdings changes recorded since the last snapshot: " + d + "; " + flows + ".",
		}
	}
	return map[string]snapshot.ValueNote{
		"portfolio.stocks_value_usd": one(agg["stock"]),
		"portfolio.crypto_value_usd": one(agg["crypto"]),
		"portfolio.total_value_usd":  one(agg["stock"], agg["crypto"]),
	}
}

// valueHistoryLines lists, for the three weeks before the previous snapshot, the holdings changes
// recorded in each, so an earlier fall in the "Last 4" column can be read against them.
func (s *Server) valueHistoryLines(ctx context.Context, prev *snapshot.Payload) []string {
	var out []string
	end := prevInstant(prev)
	for k := 0; k < 3; k++ {
		to := end.AddDate(0, 0, -7*k)
		from := to.AddDate(0, 0, -7)
		agg := s.auditWindow(ctx, from, to)
		d := describeAgg(agg["stock"], agg["crypto"])
		fsum, _ := s.store.CapitalFlowsBetween(ctx, 1, from, to)
		line := ""
		if d != "" {
			line = "holdings changes: " + d
		}
		if fsum.Has {
			if line != "" {
				line += "; "
			}
			line += "capital flows from statements: " + flowAmounts(fsum)
		}
		if line != "" {
			out = append(out, fmt.Sprintf("Week ending %s — %s.", to.Format("2006-01-02"), line))
		}
	}
	return out
}

func numOr(v any) float64 {
	f, _ := v.(float64)
	return f
}

func tickerList(t []string) string {
	sort.Strings(t)
	if len(t) > 6 {
		return strings.Join(t[:6], ", ") + fmt.Sprintf(" and %d more", len(t)-6)
	}
	return strings.Join(t, ", ")
}

func snapMoney(v float64) string { return snapshot.FormatNumber("x_usd", v) }

func flowAmounts(f store.FlowSummary) string {
	return fmt.Sprintf("in %s, out %s (net %s)", snapMoney(f.InUSD), snapMoney(f.OutUSD), snapMoney(f.NetUSD))
}

// flowsSentence says what is known about deposits and withdrawals in (from, to]: the amounts
// recorded from statements, "none recorded" where statements cover the period, or that no
// statement reaches it. Never an estimate.
func (s *Server) flowsSentence(ctx context.Context, from, to time.Time) (long, short string) {
	cov, _ := s.store.EtoroCoverageFor(ctx, 1, to)
	sum, _ := s.store.CapitalFlowsBetween(ctx, 1, from, to)
	switch {
	case sum.Has:
		return "capital flows recorded from statements in this period: " + flowAmounts(sum) +
				"; the rest of the change is price movement and items no statement shows, which FT does not split further",
			"capital flows from statements: net " + snapMoney(sum.NetUSD)
	case cov.Has && cov.End >= to.UTC().Format("2006-01-02"):
		return "no capital flows are recorded in this period (statements cover through " + cov.End + "), so the change is price movement plus the holdings changes above",
			"no capital flows recorded"
	case cov.Has:
		return "deposits and withdrawals after " + cov.End + " are not known (the last statement ends then; the next should start " + cov.NextStart + "), so they are not separable from price movement",
			"capital flows after " + cov.End + " not known"
	}
	return "deposits and withdrawals are not known (no statement covers this period), so they are not separable from price movement", "capital flows not known"
}
