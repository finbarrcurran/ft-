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
	"ft/internal/config"
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

func printUsage(w *os.File) {
	fmt.Fprint(w, `FT — Finance Tracker

USAGE
  ft                       run server (default)
  ft serve                 run server (explicit)
  ft seed [--user-id N]    load Fin's real 23 stocks + 13 crypto into the DB
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

func must(what string, err error) {
	if err != nil {
		slog.Error(what, "err", err)
		os.Exit(1)
	}
}
