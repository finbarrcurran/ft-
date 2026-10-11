package monitor

import (
	"context"
	"os"
	"sort"
	"strings"
)

// RequiredProviderKeys are the environment variables FT cannot do its job without. A
// host move once dropped configuration silently (SC-48 found the Telegram variables
// never existed on either host); CheckKeys makes that loud at startup.
func RequiredProviderKeys(newsAPIActive bool) []string {
	keys := []string{
		"FT_FINNHUB_API_KEY", "FT_COINGECKO_API_KEY", "FRED_API_KEY",
		"FT_ETORO_API_KEY", "FT_ETORO_USER_KEY", "FT_ANTHROPIC_API_KEY", "FT_GITHUB_TOKEN",
		"FT_TELEGRAM_BOT_TOKEN", "FT_TELEGRAM_CHAT_ID",
	}
	if newsAPIActive {
		keys = append(keys, "NEWSAPI_API_KEY")
	}
	return keys
}

// MissingEnv returns the names (never values) of required variables that are empty.
func MissingEnv(required []string) []string {
	var out []string
	for _, k := range required {
		if strings.TrimSpace(os.Getenv(k)) == "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// CheckKeys sends ONE alert listing the missing variable names, if any. It returns the
// names it found missing (empty when all are set).
func CheckKeys(ctx context.Context, n Notifier, required []string) []string {
	missing := MissingEnv(required)
	if len(missing) == 0 || n == nil {
		return missing
	}
	_ = n.Send(ctx, "🟠 FT started with provider settings missing from its environment: "+
		strings.Join(missing, ", ")+". Those features will not work until they are added to /etc/ft/env and FT is restarted.")
	return missing
}
