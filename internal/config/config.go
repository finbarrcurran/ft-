// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Network
	Addr    string // listen address, e.g. ":8081"
	BaseURL string // canonical public URL, e.g. "https://ft.curranhouse.dev"

	// Storage
	DBPath string // path to sqlite file

	// Auth / sessions
	SessionDays  int    // session lifetime in days
	CookieSecure bool   // Secure flag on session cookie
	CookieDomain string // optional cookie domain

	// Background work
	RefreshInterval time.Duration // how often the market-data refresher fires

	// Optional third-party keys (graceful degradation if missing)
	FinnhubAPIKey    string
	TwelveDataAPIKey string
	NewsAPIKey       string
	CryptoPanicKey   string
}

func Load() (*Config, error) {
	cfg := &Config{
		Addr:            envStr("FT_ADDR", ":8081"),
		BaseURL:         envStr("FT_BASE_URL", ""),
		DBPath:          envStr("FT_DB_PATH", "./data/ft.db"),
		SessionDays:     envInt("FT_SESSION_DAYS", 30),
		CookieSecure:    envBool("FT_COOKIE_SECURE", true),
		CookieDomain:    envStr("FT_COOKIE_DOMAIN", ""),
		RefreshInterval: envDuration("FT_REFRESH_INTERVAL", 15*time.Minute),
		FinnhubAPIKey:    envStr("FT_FINNHUB_API_KEY", ""),
		TwelveDataAPIKey: envStr("FT_TWELVEDATA_API_KEY", ""),
		NewsAPIKey:       envStr("NEWSAPI_API_KEY", ""),
		CryptoPanicKey:   envStr("CRYPTOPANIC_API_KEY", ""),
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o750); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	return cfg, nil
}

func envStr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
