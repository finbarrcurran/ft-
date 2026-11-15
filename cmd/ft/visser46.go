package main

import (
	"context"
	"flag"
	"fmt"
	"ft/internal/config"
	"ft/internal/health"
	"ft/internal/store"
	"ft/internal/visser46"
	"os"
	"time"
)

// runVisser46Backfill is the one-off Jordi-46 Part B backfill: a year of daily closes for the index tokens
// (keyed market_chart calls, 2.5 s apart) and the index stocks (Yahoo). --dry-run prints the plan and makes no call.
// A real run also needs coingecko_partb_enabled = on and the Demo key, and stops at the monthly soft stop.
func runVisser46Backfill(args []string) {
	fs := flag.NewFlagSet("visser46-backfill", flag.ExitOnError)
	dry := fs.Bool("dry-run", false, "print what it would call and how many calls; make none")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	must("load config", err)
	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())
	health.Init(st)
	health.InitEndpoint(st)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	rep, err := visser46.New(st).Backfill(ctx, visser46.BackfillOptions{DryRun: *dry, Out: os.Stdout})
	must("backfill", err)
	fmt.Printf("backfill: dryRun=%v plannedCalls=%d tokensAttempted=%d tokensSkipped=%d stocks=%v failed=%d\n",
		rep.DryRun, rep.PlannedCalls, rep.TokensAttempted, len(rep.TokensSkipped), rep.StocksDone, len(rep.Failed))
	if len(rep.Thin) > 0 {
		fmt.Printf("THIN (< %d closes): %v\n", visser46.ThinCloses, rep.Thin)
	}
	for _, f := range rep.Failed {
		fmt.Fprintln(os.Stderr, "  failed:", f)
	}
	if rep.StoppedBy != nil {
		fmt.Fprintln(os.Stderr, "stopped:", rep.StoppedBy)
		os.Exit(1)
	}
}

// runVisser46Report prints this month's CoinGecko calls per endpoint and feature, the keyed total against the
// soft stop and the 10,000 limit, and the projected month-end keyed total. Read-only; makes no call.
func runVisser46Report(args []string) {
	cfg, err := config.Load()
	must("load config", err)
	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())
	ctx := context.Background()
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	next := first.AddDate(0, 1, 0)
	keyed, _ := st.CoinGeckoMonthKeyed(ctx, now)
	fmt.Printf("CoinGecko, %s (UTC). Part B switch: %v. Soft stop: %d. Demo key limit: 10000.\n", first.Format("2006-01"), onOffStr(st.PartBEnabled(ctx)), st.PartBSoftStop(ctx))
	fmt.Printf("keyed calls so far this month (provider_daily): %d\n", keyed)
	if since := now.Sub(first); since >= 24*time.Hour {
		fmt.Printf("projected month-end keyed total at this pace: %d (against 10000)\n", int(float64(keyed)*float64(next.Sub(first))/float64(since)+0.5))
	} else {
		fmt.Println("projection: not shown in the first 24 hours of a month")
	}
	rows, err := st.CoinGeckoEndpointUsage(ctx, first.Format("2006-01-02"), next.Format("2006-01-02"))
	must("usage", err)
	fmt.Printf("%-16s %-14s %-6s %7s %5s %5s %7s\n", "feature", "endpoint", "keyed", "calls", "ok", "fail", "429")
	for _, u := range rows {
		f := u.Feature
		if f == "" {
			f = "(existing)"
		}
		fmt.Printf("%-16s %-14s %-6v %7d %5d %5d %7d\n", f, u.Endpoint, u.Keyed, u.Calls, u.OK, u.Fail, u.Fail429)
	}
	fmt.Println("note: per-endpoint counters start when this build is deployed; the keyed total above comes from provider_daily and covers the whole month.")
}

func onOffStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
