package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ft/internal/store"
)

// Jordi-46 Part B: the breadth endpoint and the Diagnostics block read stored data only (no CoinGecko call), and
// the preference validators accept only sane values.
func TestVisser46BreadthEndpointAndDiagnosticsBlock(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	end := time.Now().UTC().AddDate(0, 0, -1)
	var pts []store.PricePoint
	for i := 0; i < 210; i++ {
		pts = append(pts, store.PricePoint{Date: end.AddDate(0, 0, -(209 - i)).Format("2006-01-02"), Close: 100 + float64(i)})
	}
	if err := st.InsertPriceHistoryBatch(ctx, "BTC", "crypto", pts); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/crypto-index/visser46/breadth", nil)
	req.SetPathValue("key", "visser46")
	rec := httptest.NewRecorder()
	s.handleCryptoIndexBreadth(rec, req.WithContext(context.WithValue(ctx, ctxUserID, int64(1))))
	if rec.Code != 200 {
		t.Fatalf("code %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Breadth struct {
			Total struct {
				Members  int `json:"members"`
				Above200 struct{ Yes, N int }
			} `json:"total"`
			Missing []map[string]any `json:"missing"`
		} `json:"breadth"`
		PartB map[string]any `json:"partB"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Breadth.Total.Members != 46 || out.Breadth.Total.Above200.N != 1 || out.Breadth.Total.Above200.Yes != 1 || len(out.Breadth.Missing) != 45 || out.PartB["enabled"] != false {
		t.Fatalf("breadth payload: %s", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	req.SetPathValue("key", "other")
	rec = httptest.NewRecorder()
	s.handleCryptoIndexBreadth(rec, req)
	if rec.Code != 404 {
		t.Fatalf("unknown index: %d", rec.Code)
	}

	u := s.coinGeckoUsage(ctx)
	if u["partBEnabled"] != false || u["softStop"] != 7000 || u["monthlyLimit"] != 10000 || u["monthKeyed"] != 0 {
		t.Fatalf("diagnostics block: %v", u)
	}
}

func TestPartBPreferenceValidation(t *testing.T) {
	for _, c := range []struct {
		k, v string
		ok   bool
	}{
		{"coingecko_partb_enabled", "on", true}, {"coingecko_partb_enabled", "off", true}, {"coingecko_partb_enabled", "yes", false},
		{"coingecko_partb_soft_stop", "7000", true}, {"coingecko_partb_soft_stop", "10000", true}, {"coingecko_partb_soft_stop", "20000", false},
		{"coingecko_partb_soft_stop", "-5", false}, {"coingecko_partb_soft_stop", "abc", false}, {"coingecko_partb_soft_stop", "50", false},
	} {
		if got := validPreferenceValue(c.k, c.v); got != c.ok {
			t.Errorf("%s=%q: %v, want %v", c.k, c.v, got, c.ok)
		}
	}
}
