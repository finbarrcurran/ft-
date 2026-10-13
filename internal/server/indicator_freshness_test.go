package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ft/internal/cryptoindicators"
	"ft/internal/store"
)

type cardFresh struct {
	ID          string `json:"id"`
	DataAsOf    string `json:"dataAsOf"`
	StaleLevel  string `json:"staleLevel"`
	DataAge     *int   `json:"dataAge"`
	DataAgeUnit string `json:"dataAgeUnit"`
	FetchError  string `json:"fetchError"`
}

// SC-49b: each card carries ITS OWN freshness (the v1.82.0 miss was fields on the wrong endpoint).
func TestIndicatorCardsCarryTheirOwnFreshness(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "ft.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	svc := cryptoindicators.New(st.DB)
	ctx := context.Background()
	daysAgo := func(n int) string { return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02") }
	// CFNAI 121 days (red), DXY 15 days (amber), stablecoin today (fresh), US2Y left empty,
	// and an indicator with no rule (fear & greed) given an old date: it must carry no badge.
	for id, d := range map[string]string{"pal_cfnai": daysAgo(121), "pal_dxy": daysAgo(15), "universal_stablecoin_supply": daysAgo(0), "sentiment_fear_greed": daysAgo(90)} {
		if err := svc.SetIndicatorAsOf(ctx, id, d); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{cryptoIndicators: svc}
	rec := httptest.NewRecorder()
	s.handleListCryptoIndicators(rec, httptest.NewRequest(http.MethodGet, "/api/crypto-indicators", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var out struct {
		Indicators []cardFresh `json:"indicators"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	by := map[string]cardFresh{}
	for _, c := range out.Indicators {
		by[c.ID] = c
	}
	for id, want := range map[string]struct {
		level, unit string
		age         int
	}{"pal_cfnai": {"red", "calendar", 121}, "pal_dxy": {"amber", "calendar", 15}, "universal_stablecoin_supply": {"", "calendar", 0}} {
		c := by[id]
		if c.StaleLevel != want.level || c.DataAgeUnit != want.unit || c.DataAge == nil || *c.DataAge != want.age {
			t.Fatalf("%s: %+v (want %+v)", id, c, want)
		}
	}
	if c := by["pal_us2y"]; c.DataAsOf != "" || c.StaleLevel != "" {
		t.Fatalf("pal_us2y has no data date yet and must carry no badge: %+v", c)
	}
	if c := by["sentiment_fear_greed"]; c.StaleLevel != "" || c.DataAge != nil {
		t.Fatalf("an indicator without a rule must carry no badge: %+v", c)
	}
}
