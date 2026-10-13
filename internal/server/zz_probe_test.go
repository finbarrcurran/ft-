package server

import (
	"net/http"
	"sort"
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"

	"ft/internal/config"
	"ft/internal/store"
)

func TestZZProbe(t *testing.T) {
	path := os.Getenv("FT_PROBE_DB")
	if path == "" {
		t.Skip()
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(&config.Config{}, st, nil)
	ctx := context.Background()
	var shape func(v any, depth int) string
	shape = func(v any, depth int) string {
		switch x := v.(type) {
		case map[string]any:
			if depth == 0 {
				return "{...}"
			}
			out := "{"
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if k == "donuts" || k == "svg" {
					continue
				}
				out += k + ":" + shape(x[k], depth-1) + ", "
			}
			return out + "}"
		case []any:
			if len(x) == 0 {
				return "[]"
			}
			return fmt.Sprintf("[%d x %s]", len(x), shape(x[0], depth))
		case string:
			if len(x) > 30 {
				x = x[:30]
			}
			return fmt.Sprintf("%q", x)
		default:
			return fmt.Sprint(x)
		}
	}
	show := func(name, p string, q url.Values, h func(w http.ResponseWriter, r *http.Request)) {
		out, err := s.callHandlerJSON(ctx, 1, p, q, h)
		if err != nil {
			fmt.Println("###", name, "ERR", err)
			return
		}
		fmt.Println("###", name, shape(out, 3))
	}
	show("summary", "/api/summary", nil, s.handleSummary)
	show("theses", "/api/theses", nil, s.handleListTheses)
	show("gaps", "/api/theses/gaps", nil, s.handleThesesGaps)
	show("watchlist", "/api/watchlist", nil, s.handleListWatchlist)
	show("alerts", "/api/bot/alerts", url.Values{"only_unnotified": {"0"}}, s.handleBotAlerts)
	show("regime", "/api/regime", nil, s.handleGetRegime)
	show("macroregime", "/api/macro/regime", nil, s.handleMacroRegime)
	show("perfOverview", "/api/performance/overview", nil, s.handlePerformanceOverview)
	show("cryptoInd", "/api/crypto-indicators", nil, s.handleListCryptoIndicators)
	show("video", "/api/video-digest", nil, s.handleVideoDigestList)
	show("etoroPerf", "/api/etoro/performance", nil, s.handleEtoroPerformance)
	show("tradeStats", "/api/etoro/trade-stats", url.Values{"scope": {"discretionary"}}, s.handleEtoroTradeStats)
}
