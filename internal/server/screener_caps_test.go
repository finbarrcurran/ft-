package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"ft/internal/heatmap"
)

// FT-091: the stock Screener shows the Yahoo market cap where the daily stats pass has one (labelled with its source
// and as-of) and the seeded sample figure elsewhere, labelled approximate; in demo mode stats are not used.
func TestScreener_MarketCapSourceAndApprox(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	tiles := heatmap.AllTilesWithLive()
	if len(tiles) < 2 {
		t.Fatal("no seeded tiles")
	}
	tk := tiles[0].Ticker
	mustExec(t, st, `INSERT INTO ticker_market_stats (ticker, market_cap, cap_source, cap_as_of, updated_at) VALUES (?, 1500000000000, 'yahoo', 1794343300, 0)`, tk)
	type row struct {
		Ticker          string  `json:"ticker"`
		MarketCapB      float64 `json:"marketCapB"`
		MarketCapSource string  `json:"marketCapSource"`
		MarketCapApprox bool    `json:"marketCapApprox"`
		MarketCapAsOf   *int64  `json:"marketCapAsOf"`
	}
	get := func(path string) map[string]row {
		rec := httptest.NewRecorder()
		s.handleScreener(rec, httptest.NewRequest("GET", path, nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
		var out struct {
			Rows []row `json:"rows"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Rows) == 0 {
			t.Fatalf("%v %s", err, rec.Body.String()[:200])
		}
		m := map[string]row{}
		for _, r := range out.Rows {
			m[r.Ticker] = r
		}
		return m
	}
	m := get("/api/screener")
	a := m[tk]
	if a.MarketCapSource != "yahoo" || a.MarketCapApprox || a.MarketCapB != 1500 || a.MarketCapAsOf == nil {
		t.Fatalf("%s must use the stats cap: %+v", tk, a)
	}
	b := m[tiles[1].Ticker]
	if b.MarketCapSource != "sample" || !b.MarketCapApprox || b.MarketCapAsOf != nil {
		t.Fatalf("%s must be labelled approximate: %+v", tiles[1].Ticker, b)
	}
	// the cap filter uses the effective cap: 1,500B is in, so mcap_min=1400 keeps it
	if _, ok := get("/api/screener?mcap_min=1400")[tk]; !ok {
		t.Fatal("mcap_min must apply to the stats cap")
	}
	if err := st.SetPreference(ctx, "demo_mode", "on"); err != nil {
		t.Fatal(err)
	}
	if r := get("/api/screener")[tk]; r.MarketCapSource != "sample" {
		t.Fatalf("demo mode must not use stats: %+v", r)
	}
}
