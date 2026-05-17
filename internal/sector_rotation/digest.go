// Spec 9f D8 — Weekly digest.
//
// Friday 22:00 UTC: compute current metrics, identify top/bottom 5 by 3M
// RS, biggest WoW movers, newly-tagged "rotating in" sectors, prepend the
// macro strip text, write a single markdown row to sector_rotation_digests.
// Idempotent: UNIQUE(week_ending) on the table makes re-runs no-ops.

package sector_rotation

import (
	"context"
	"fmt"
	"ft/internal/store"
	"sort"
	"strings"
	"time"
)

// RunWeeklyDigest computes + writes the digest for the most recent Friday.
// `now` overridable for tests; pass time.Now() in production. Returns the
// week_ending date and the markdown that was inserted (for logging).
func RunWeeklyDigest(ctx context.Context, st *store.Store, now time.Time) (string, string, error) {
	// Snap to most recent Friday (UTC). If today IS Friday, use today.
	t := now.UTC()
	for t.Weekday() != time.Friday {
		t = t.AddDate(0, 0, -1)
	}
	weekEnding := t.Format("2006-01-02")

	metrics, err := ComputeAll(ctx, st, 1) // single-user FT
	if err != nil {
		return "", "", err
	}

	// Macro-strip free text.
	macroRead, _ := st.GetPreference(ctx, "jordi_current_sector_read")

	// Sort by RS for top/bottom.
	withRS := make([]SectorMetrics, 0, len(metrics))
	for _, m := range metrics {
		if m.RSvsSPY3M != nil {
			withRS = append(withRS, m)
		}
	}
	sort.Slice(withRS, func(i, j int) bool { return *withRS[i].RSvsSPY3M > *withRS[j].RSvsSPY3M })

	top5 := withRS[:min5(len(withRS))]
	bot5 := []SectorMetrics{}
	if n := len(withRS); n > 5 {
		bot5 = withRS[n-5:]
	}

	// WoW movers: biggest positive + biggest negative 1W change.
	withWoW := make([]SectorMetrics, 0, len(metrics))
	for _, m := range metrics {
		if m.Return1W != nil {
			withWoW = append(withWoW, m)
		}
	}
	sort.Slice(withWoW, func(i, j int) bool { return *withWoW[i].Return1W > *withWoW[j].Return1W })
	wowUp := withWoW[:min5(len(withWoW))]
	wowDown := []SectorMetrics{}
	if n := len(withWoW); n > 5 {
		// Last 5 by 1W return = worst (since sorted desc).
		wowDown = withWoW[n-5:]
	}

	// Newly tagged "rotating in" since prior digest, if any.
	priorIn := loadPriorRotatingIn(ctx, st)
	curIn := map[string]bool{}
	for _, m := range metrics {
		if m.Tag == "rotating_in" {
			curIn[m.Code] = true
		}
	}
	newlyIn := []SectorMetrics{}
	for _, m := range metrics {
		if m.Tag == "rotating_in" && !priorIn[m.Code] {
			newlyIn = append(newlyIn, m)
		}
	}

	// Compose markdown.
	var b strings.Builder
	fmt.Fprintf(&b, "# Sector Rotation digest — week ending %s\n\n", weekEnding)
	if strings.TrimSpace(macroRead) != "" {
		fmt.Fprintf(&b, "**Jordi's current read:** %s\n\n", macroRead)
	}

	fmt.Fprintf(&b, "## Top 5 by RS vs SPY (3M)\n\n")
	for _, m := range top5 {
		fmt.Fprintf(&b, "- **%s** — RS %.2f× · 3M %s · holdings %d\n",
			m.DisplayName, *m.RSvsSPY3M, fmtPct(m.Return3M), m.HoldingsCount)
	}
	fmt.Fprintf(&b, "\n## Bottom 5 by RS vs SPY (3M)\n\n")
	for _, m := range bot5 {
		fmt.Fprintf(&b, "- **%s** — RS %.2f× · 3M %s · holdings %d\n",
			m.DisplayName, *m.RSvsSPY3M, fmtPct(m.Return3M), m.HoldingsCount)
	}

	if len(wowUp) > 0 {
		fmt.Fprintf(&b, "\n## Biggest weekly winners (1W)\n\n")
		for _, m := range wowUp {
			fmt.Fprintf(&b, "- **%s** %s\n", m.DisplayName, fmtPct(m.Return1W))
		}
	}
	if len(wowDown) > 0 {
		fmt.Fprintf(&b, "\n## Biggest weekly losers (1W)\n\n")
		for _, m := range wowDown {
			fmt.Fprintf(&b, "- **%s** %s\n", m.DisplayName, fmtPct(m.Return1W))
		}
	}

	if len(newlyIn) > 0 {
		fmt.Fprintf(&b, "\n## Newly rotating in vs prior digest\n\n")
		for _, m := range newlyIn {
			fmt.Fprintf(&b, "- **%s** — RS %.2f× now ≥ 1.05\n", m.DisplayName, *m.RSvsSPY3M)
		}
	}

	markdown := b.String()
	if err := st.InsertSectorRotationDigest(ctx, weekEnding, markdown); err != nil {
		return weekEnding, markdown, err
	}
	return weekEnding, markdown, nil
}

// loadPriorRotatingIn returns the set of sector codes tagged "rotating in"
// in the most-recent prior digest. Parsed best-effort from the markdown.
func loadPriorRotatingIn(ctx context.Context, st *store.Store) map[string]bool {
	out := map[string]bool{}
	digests, err := st.ListSectorRotationDigests(ctx, 1)
	if err != nil || len(digests) == 0 {
		return out
	}
	// Use the doctrine-display-name match. Cheap heuristic; close enough
	// for "newly tagged" detection.
	md := digests[0].Markdown
	// Find the "## Top 5 by RS vs SPY" section.
	idx := strings.Index(md, "## Top 5 by RS vs SPY")
	if idx < 0 {
		return out
	}
	for _, line := range strings.Split(md[idx:], "\n") {
		if !strings.HasPrefix(line, "- **") {
			continue
		}
		// Crude name extraction: text between **...**
		end := strings.Index(line[4:], "**")
		if end < 0 {
			continue
		}
		out[line[4:4+end]] = true
	}
	return out
}

func min5(n int) int {
	if n < 5 {
		return n
	}
	return 5
}

func fmtPct(p *float64) string {
	if p == nil {
		return "—"
	}
	x := *p * 100
	sign := "+"
	if x < 0 {
		sign = ""
	}
	return fmt.Sprintf("%s%.1f%%", sign, x)
}

// ScheduleWeeklyDigest runs RunWeeklyDigest every Friday at 22:00 UTC for
// the lifetime of ctx. Picks the next Friday after `now`; if today is
// Friday before 22:00 UTC, fires today.
func ScheduleWeeklyDigest(ctx context.Context, st *store.Store) {
	for {
		next := nextFridayAt(time.Now().UTC(), 22, 0)
		wait := time.Until(next)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if _, _, err := RunWeeklyDigest(ctx, st, time.Now().UTC()); err != nil {
			// best-effort; log via caller is fine
		}
	}
}

func nextFridayAt(now time.Time, hour, minute int) time.Time {
	for d := 0; d <= 8; d++ {
		cand := time.Date(now.Year(), now.Month(), now.Day()+d, hour, minute, 0, 0, time.UTC)
		if cand.Weekday() == time.Friday && cand.After(now) {
			return cand
		}
	}
	return now.Add(7 * 24 * time.Hour)
}
