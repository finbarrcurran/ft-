package snapshot

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var sectionTitles = []struct{ Prefix, Title string }{
	{"portfolio", "Portfolio"},
	{"performance", "Performance (eToro, USD)"},
	{"theses", "Theses and scorecards"},
	{"gaps", "Gap report"},
	{"watchlist", "Watchlist"},
	{"alerts", "Alerts"},
	{"regime", "Regime"},
	{"crypto_ind", "Crypto indicators"},
	{"data_health", "Data health"},
	{"video", "Video digest"},
}

var labels = map[string]string{
	"portfolio.total_value_usd":         "Total value (stocks + crypto)",
	"portfolio.stocks_value_usd":        "Stocks value",
	"portfolio.crypto_value_usd":        "Crypto value",
	"portfolio.cash_usd":                "Cash",
	"portfolio.holdings_count":          "Holdings",
	"portfolio.top5_pct":                "Top-5 concentration",
	"performance.ytd_realised_disc_usd": "YTD realised, discretionary",
	"performance.ytd_realised_copy_usd": "YTD realised, copy/CFD",
	"performance.ytd_realised_usd":      "YTD closed realised total",
	"performance.ytd_net_usd":           "YTD net (all lines)",
	"performance.ytd_dividends_usd":     "YTD dividends",
	"performance.ytd_fees_usd":          "YTD fees",
	"performance.ytd_income_other_usd":  "YTD interest, refunds and other",
	"performance.stats_n":               "Trades (discretionary, YTD)",
	"performance.stats_win_rate_pct":    "Win rate",
	"performance.stats_profit_factor":   "Profit factor",
	"performance.stats_expectancy_usd":  "Expectancy per trade",
	"performance.coverage_end":          "Statement data through",
	"alerts.7d_total":                   "Alerts last 7 days (all kinds)",
	"theses.locked_count":               "Locked stock theses",
	"theses.rescore_due_count":          "Re-scores due (earnings)",
	"theses.crypto_count":               "Crypto theses",
	"theses.scorecards_stock":           "Stock scorecards",
	"theses.scorecards_crypto":          "Crypto scorecards",
	"gaps.total":                        "Thesis gaps",
	"watchlist.count":                   "Watchlist entries",
	"watchlist.in_zone_count":           "In entry zone",
	"watchlist.in_zone":                 "Watchlist",
	"gaps.open":                         "Gap report",
	"theses.locked":                     "Thesis",
	"theses.rescore_due":                "Thesis",
	"portfolio.top5":                    "Portfolio",
	"alerts.current_total":              "Live alerts now",
	"regime.effective":                  "Regime (effective)",
	"regime.jordi":                      "Regime (Jordi)",
	"regime.cowen":                      "Regime (Cowen)",
	"regime.macro_quadrant":             "Macro quadrant",
	"regime.macro_shorthand":            "Macro regime",
}

// Label is the human name of a key.
func Label(key string) string {
	if l, ok := labels[key]; ok {
		return l
	}
	switch {
	case strings.HasPrefix(key, "thesis.score."):
		return "Thesis " + strings.TrimPrefix(key, "thesis.score.") + " score"
	case strings.HasPrefix(key, "gaps.count."):
		return "Gaps (" + strings.TrimPrefix(key, "gaps.count.") + ")"
	case strings.HasPrefix(key, "alerts.7d."):
		return "Alerts last 7 days (" + strings.TrimPrefix(key, "alerts.7d.") + ")"
	case strings.HasPrefix(key, "alerts.current."):
		return "Live alerts (" + strings.TrimPrefix(key, "alerts.current.") + ")"
	case strings.HasPrefix(key, "crypto_ind."):
		return strings.TrimPrefix(key, "crypto_ind.")
	}
	return key
}

// FormatNumber formats a value according to its key.
func FormatNumber(key string, v float64) string {
	switch {
	case strings.HasSuffix(key, "_usd"):
		return money(v)
	case strings.HasSuffix(key, "_pct"):
		return fmt.Sprintf("%.1f%%", v)
	case strings.HasSuffix(key, "_count") || strings.HasSuffix(key, "_n") || strings.HasPrefix(key, "gaps.") || strings.HasPrefix(key, "alerts.") || strings.HasSuffix(key, ".holdings_count") || strings.HasPrefix(key, "thesis.score."):
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e12 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'g', 5, 64)
}

// FormatDelta formats a signed change.
func FormatDelta(key string, d float64) string {
	s := FormatNumber(key, math.Abs(d))
	if d < 0 {
		return "-" + s
	}
	return "+" + s
}

func money(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := fmt.Sprintf("%.2f", v)
	ip, fp := s[:len(s)-3], s[len(s)-3:]
	var b strings.Builder
	for i, c := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := "$" + b.String() + fp
	if neg {
		return "-" + out
	}
	return out
}

func trendString(key string, t []float64) string {
	parts := make([]string, len(t))
	for i, v := range t {
		parts[i] = FormatNumber(key, v)
	}
	return strings.Join(parts, " → ")
}

// Render writes the Markdown file: header, Top changes first, then one section per area, then
// the end marker a reader can use to detect a cut-off.
func Render(p *Payload, r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ft_snapshot_version: %d\n", p.Version)
	fmt.Fprintf(&b, "generated_at: %s\n", p.GeneratedAt)
	fmt.Fprintf(&b, "week_start: %s\n\n", p.WeekStart)
	fmt.Fprintf(&b, "# FT weekly snapshot — week of %s\n\n", p.WeekStart)
	b.WriteString("Deterministic summary of FT's state; the database is the source of truth. Personal use only. Not investment advice.\n\n")

	b.WriteString("## Top changes\n\n")
	switch {
	case r.First:
		b.WriteString("First snapshot: no earlier week to compare with, so no changes are shown. Trends build up from here.\n")
	case len(r.Top) == 0:
		fmt.Fprintf(&b, "Nothing moved since the previous snapshot (week of %s).\n", r.PrevWeek)
	default:
		for _, l := range r.Top {
			fmt.Fprintf(&b, "- %s\n", l)
		}
		if r.ChangeCount > len(r.Top) {
			fmt.Fprintf(&b, "\n%d changes in total; the largest are listed. Compared with the week of %s.\n", r.ChangeCount, r.PrevWeek)
		} else {
			fmt.Fprintf(&b, "\nCompared with the week of %s.\n", r.PrevWeek)
		}
	}
	if len(p.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range p.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}

	byPrefix := map[string][]Series{}
	for _, s := range r.Metrics {
		pre := strings.SplitN(s.Key, ".", 2)[0]
		if pre == "thesis" {
			continue // per-ticker scores: only changes are listed, under Theses
		}
		byPrefix[pre] = append(byPrefix[pre], s)
	}
	for _, sec := range sectionTitles {
		series := byPrefix[sec.Prefix]
		lines := p.Lines[sec.Prefix]
		var texts []string
		for k, v := range p.Texts {
			if strings.SplitN(k, ".", 2)[0] == sec.Prefix && !strings.HasSuffix(k, ".as_of") {
				texts = append(texts, k)
				_ = v
			}
		}
		sort.Strings(texts)
		var sets []string
		for k := range p.Sets {
			if strings.SplitN(k, ".", 2)[0] == sec.Prefix || (sec.Prefix == "theses" && strings.HasPrefix(k, "theses.")) {
				sets = append(sets, k)
			}
		}
		sort.Strings(sets)
		if len(series) == 0 && len(lines) == 0 && len(texts) == 0 && len(sets) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n", sec.Title)
		if len(series) > 0 {
			b.WriteString("| Metric | Now | Change vs last week | Last 4 |\n|---|---|---|---|\n")
			for _, s := range series {
				now := FormatNumber(s.Key, s.Cur)
				if sec.Prefix == "crypto_ind" {
					if asOf := p.Texts[s.Key+".as_of"]; asOf != "" {
						now += " (as of " + asOf + ")"
					}
				}
				change := "first snapshot"
				if s.Delta != nil {
					change = FormatDelta(s.Key, *s.Delta)
					if math.Abs(*s.Delta) < 1e-9 {
						change = "no change"
					}
				}
				trend := "—"
				if len(s.Trend) > 1 {
					trend = trendString(s.Key, s.Trend)
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", Label(s.Key), now, change, trend)
			}
		}
		for _, k := range texts {
			fmt.Fprintf(&b, "\n- %s: %s", Label(k), p.Texts[k])
			for _, c := range r.TextChanges {
				if c.Key == k {
					fmt.Fprintf(&b, " (changed from %s to %s)", c.From, c.To)
				}
			}
		}
		if len(texts) > 0 {
			b.WriteString("\n")
		}
		for _, k := range sets {
			members := p.Sets[k]
			shown := strings.Join(members, ", ")
			if len(members) == 0 {
				shown = "none"
			}
			if len(members) > 15 {
				shown = fmt.Sprintf("%d members (listed in the database; only changes are shown here)", len(members))
			}
			fmt.Fprintf(&b, "\n- %s: %s", setHeading(k), shown)
			for _, sc := range r.SetChanges {
				if sc.Key == k {
					if len(sc.Added) > 0 {
						fmt.Fprintf(&b, " — %s: %s", setVerb(k, true), strings.Join(sc.Added, ", "))
					}
					if len(sc.Removed) > 0 {
						fmt.Fprintf(&b, " — %s: %s", setVerb(k, false), strings.Join(sc.Removed, ", "))
					}
				}
			}
		}
		if len(sets) > 0 {
			b.WriteString("\n")
		}
		if sec.Prefix == "theses" {
			var changed []string
			for _, s := range r.Metrics {
				if strings.HasPrefix(s.Key, "thesis.score.") && s.Delta != nil && math.Abs(*s.Delta) > 1e-9 {
					changed = append(changed, fmt.Sprintf("%s %s → %s", strings.TrimPrefix(s.Key, "thesis.score."), FormatNumber(s.Key, *s.Prev), FormatNumber(s.Key, s.Cur)))
				}
			}
			if len(changed) > 0 {
				fmt.Fprintf(&b, "\n- Score changes since last week: %s\n", strings.Join(changed, "; "))
			} else if !r.First {
				b.WriteString("\n- Score changes since last week: none\n")
			}
		}
		if len(lines) > 0 {
			b.WriteString("\n")
			for _, l := range lines {
				if strings.HasPrefix(l, "  ") {
					fmt.Fprintf(&b, "  - %s\n", strings.TrimSpace(l))
				} else {
					fmt.Fprintf(&b, "- %s\n", l)
				}
			}
		}
	}
	fmt.Fprintf(&b, "\n<!-- END ft-snapshot v%d -->\n", p.Version)
	return b.String()
}

func setHeading(key string) string {
	switch key {
	case "watchlist.in_zone":
		return "In the entry zone"
	case "gaps.open":
		return "Open gaps"
	case "theses.locked":
		return "Locked theses"
	case "theses.rescore_due":
		return "Re-scores due"
	case "portfolio.top5":
		return "Top-5 holdings"
	}
	return key
}
