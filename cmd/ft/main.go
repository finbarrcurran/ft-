// FT — Finance Tracker.
//
// Usage:
//
//	ft                run server (default)
//	ft serve          run server (explicit)
//	ft help           print this usage
//
// More subcommands (user, token) will be added as the port progresses.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"ft/internal/auth"
	"ft/internal/config"
	"ft/internal/frameworks"
	"ft/internal/macro"
	"ft/internal/marketdata"
	"ft/internal/refresh"
	"ft/internal/server"
	"ft/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cmd := "serve"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		runServe()
	case "seed":
		runSeed(os.Args[2:])
	case "daily":
		runDaily(os.Args[2:])
	case "token":
		runToken(os.Args[2:])
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

// runSeed loads Fin's real 23 stocks + 13 crypto holdings into the DB,
// replacing anything currently owned by the target user (default user id 1).
// This is a one-shot demo command; once the xlsx import lands, it's the
// canonical way to get data in.
func runSeed(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	userID := fs.Int64("user-id", 1, "user id who owns the seeded holdings")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	must("load config", err)
	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())

	nStocks, nCrypto, err := st.SeedHoldings(context.Background(), *userID)
	must("seed", err)
	fmt.Printf("seeded: %d stock holdings, %d crypto holdings (user_id=%d)\n", nStocks, nCrypto, *userID)
}

// runDaily executes the Spec 3 daily background job once: price_history
// backfill + earnings/ex-div + beta auto-resolve. Intended as a one-shot for
// first deploy (so sparklines render before the next 04:00 UTC cron) and as a
// manual diagnostic ("why did NVDA's sparkline disappear?").
func runDaily(args []string) {
	fs := flag.NewFlagSet("daily", flag.ExitOnError)
	userID := fs.Int64("user-id", 1, "user id whose holdings to refresh")
	days := fs.Int("days", 30, "history depth in days")
	_ = fs.Parse(args)

	cfg, err := config.Load()
	must("load config", err)
	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res := refresh.New(st).RunDailyJob(ctx, *userID, *days)
	fmt.Printf("daily: stocks_history=%d/%d crypto_history=%d/%d calendar=%d beta=%d pruned=%d errs=%d took=%s\n",
		res.StocksHistoryOK, res.StocksProcessed,
		res.CryptoHistoryOK, res.CryptoProcessed,
		res.CalendarOK, res.BetaOK, res.PrunedRows, len(res.Errors),
		res.FinishedAt.Sub(res.StartedAt).Round(time.Millisecond),
	)
	for _, e := range res.Errors {
		fmt.Fprintln(os.Stderr, "  err:", e)
	}
}

func printUsage(w *os.File) {
	fmt.Fprint(w, `FT — Finance Tracker

USAGE
  ft                       run server (default)
  ft serve                 run server (explicit)
  ft seed [--user-id N]    load Fin's real 23 stocks + 13 crypto into the DB
  ft daily [--user-id N]   run the Spec 3 D8/D10/D11 daily job once (sparklines + calendar + beta)
  ft token create --user-id N --name NAME    mint a new ft_st_… service token
  ft token list [--user-id N]                list service tokens (no plaintext)
  ft help                  print this usage

ENVIRONMENT
  FT_ADDR              listen address                   (default :8081)
  FT_DB_PATH           path to sqlite file              (default ./data/ft.db)
  FT_BASE_URL          canonical public URL (no trailing slash)
  FT_COOKIE_SECURE     Secure flag on session cookie    (default true)
  FT_COOKIE_DOMAIN     cookie domain (optional)
  FT_SESSION_DAYS      session lifetime in days         (default 30)
  FT_REFRESH_INTERVAL  background market refresh        (default 15m)
  NEWSAPI_API_KEY      optional, for stock news adapter
  CRYPTOPANIC_API_KEY  optional, for crypto news adapter
`)
}

func runServe() {
	cfg, err := config.Load()
	must("load config", err)

	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())

	// Spec 4: load Jordi/Cowen framework definitions. Malformed JSON disables
	// that framework with a warning but doesn't crash the server.
	must("frameworks", frameworks.Load())

	// Spec 5: load multi-exchange holiday calendars from embedded JSON. Bad
	// files log a warning and are skipped; we never crash for missing data
	// (worst-case the affected exchange treats every day as a trading day).
	must("holidays", marketdata.LoadHolidays())

	// Spec 9b D11: macro economic calendar (hand-curated, embedded JSON).
	must("macro", macro.Load())

	srv := server.New(cfg, st)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	// Periodic session GC. Runs forever in background; cancellation comes via
	// process shutdown.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()
	go func() {
		t := time.NewTicker(1 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-t.C:
				if err := st.PurgeExpiredSessions(bgCtx); err != nil {
					slog.Warn("session gc", "err", err)
				}
			}
		}
	}()

	// Background market-data refresh. Skipped when FT_REFRESH_INTERVAL=0.
	if cfg.RefreshInterval > 0 {
		refreshSvc := refresh.New(st)
		go func() {
			slog.Info("auto-refresh enabled", "interval", cfg.RefreshInterval)
			// Brief delay so the HTTP server is up before we hit external APIs.
			select {
			case <-bgCtx.Done():
				return
			case <-time.After(5 * time.Second):
			}

			runOnce := func() {
				rctx, cancel := context.WithTimeout(bgCtx, 90*time.Second)
				defer cancel()
				// user_id = 1 — single-user app for now. Multi-user variant
				// would iterate over all known user ids.
				refreshSvc.RefreshAll(rctx, 1)
			}

			runOnce() // initial refresh on startup
			t := time.NewTicker(cfg.RefreshInterval)
			defer t.Stop()
			for {
				select {
				case <-bgCtx.Done():
					return
				case <-t.C:
					runOnce()
				}
			}
		}()
	} else {
		slog.Info("auto-refresh disabled (FT_REFRESH_INTERVAL=0)")
	}

	// Daily background job at 04:00 UTC: sparkline history + calendar + beta.
	// Best-effort, decoupled from the 15-min refresh so a transient Yahoo
	// rate-limit can't take down both.
	go func() {
		dailySvc := refresh.New(st)
		refresh.ScheduleDailyJob(bgCtx, func() {
			ctx, cancel := context.WithTimeout(bgCtx, 5*time.Minute)
			defer cancel()
			dailySvc.RunDailyJob(ctx, 1, 30)
		})
	}()

	go func() {
		slog.Info("listening", "addr", cfg.Addr, "cookie_secure", cfg.CookieSecure)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	slog.Info("shutdown initiated")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	slog.Info("shutdown complete")
}

// runToken handles `ft token create` and `ft token list`.
//
// Plaintext token is printed ONCE on creation. Only the sha256 hash is
// persisted, so there's no way to recover the plaintext later — if Fin loses
// it he must mint a new one and update the consuming skill's config.
func runToken(args []string) {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	cfg, err := config.Load()
	must("load config", err)
	st, err := store.Open(cfg.DBPath)
	must("open store", err)
	defer st.Close()
	must("migrate", st.Migrate())

	switch sub {
	case "create":
		fs := flag.NewFlagSet("token create", flag.ExitOnError)
		userID := fs.Int64("user-id", 1, "user id who owns the token")
		name := fs.String("name", "", "human-readable label, e.g. openclaw")
		_ = fs.Parse(args)
		if *name == "" {
			fmt.Fprintln(os.Stderr, "--name is required (e.g. --name openclaw)")
			os.Exit(2)
		}
		plain, hash, err := auth.NewServiceToken()
		must("generate token", err)
		id, err := st.CreateServiceToken(context.Background(), *userID, *name, nil, hash)
		must("persist token", err)
		fmt.Printf("token id   : %d\n", id)
		fmt.Printf("token name : %s\n", *name)
		fmt.Printf("user_id    : %d\n", *userID)
		fmt.Println()
		fmt.Println("Save this — it is shown ONCE and only the hash is stored:")
		fmt.Println()
		fmt.Println("  " + plain)
		fmt.Println()
	case "list":
		fs := flag.NewFlagSet("token list", flag.ExitOnError)
		userID := fs.Int64("user-id", 1, "user id")
		_ = fs.Parse(args)
		tokens, err := st.ListServiceTokens(context.Background(), *userID)
		must("list tokens", err)
		if len(tokens) == 0 {
			fmt.Println("no service tokens for user", *userID)
			return
		}
		fmt.Printf("%-4s  %-24s  %-20s  %s\n", "ID", "NAME", "CREATED", "STATUS")
		for _, t := range tokens {
			status := "active"
			if t.RevokedAt != nil {
				status = "revoked " + t.RevokedAt.Format(time.RFC3339)
			}
			fmt.Printf("%-4d  %-24s  %-20s  %s\n",
				t.ID, t.Name, t.CreatedAt.Format(time.RFC3339), status)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown token subcommand: %s\n", sub)
		fmt.Fprintln(os.Stderr, "  ft token create --user-id N --name NAME")
		fmt.Fprintln(os.Stderr, "  ft token list [--user-id N]")
		os.Exit(2)
	}
}

func must(what string, err error) {
	if err != nil {
		slog.Error(what, "err", err)
		os.Exit(1)
	}
}
