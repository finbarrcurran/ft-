package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ft/internal/videolinks"
)

// FT-149 PR A: the chips and drawer endpoints are gated by two switches, answer only for tickers FT knows, and a chip
// always agrees with its drawer.
func TestCommentaryChipsEndpoints(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	mustExec(t, st, `DELETE FROM watchlist`)
	mustExec(t, st, `DELETE FROM nexus_universe`)
	mustExec(t, st, `INSERT INTO watchlist (user_id, ticker, kind, company_name, added_at, updated_at) VALUES (1,'MU','stock','Micron',1,1)`)
	mustExec(t, st, `INSERT INTO nexus_universe (ticker, company, theme, is_nexus, active, added_at) VALUES ('NVDA','NVIDIA','ai',1,1,1)`)
	d1 := time.Now().UTC().AddDate(0, 0, -3).Format("2006-01-02")
	d2 := time.Now().UTC().AddDate(0, 0, -12).Format("2006-01-02")
	mustExec(t, st, `INSERT INTO video_digests (id, source, video_id, title, published_at, package_path, ingested_at) VALUES (1,'jordi','vidAAAAAAAA','W',?, '/x',1), (2,'cowen','vidBBBBBBBB','C',?, '/x',1)`, d1, d2)
	mustExec(t, st, `INSERT INTO video_mentions (digest_id, name, type, context, stance) VALUES (1,'Micron','stock','Micron is a memory winner.','bullish'), (2,'Micron','stock','Micron looks extended.','neutral'), (1,'Nvidia','stock','Nvidia leads the AI build.','bullish')`)
	if _, err := videolinks.Rebuild(ctx, st.DB, time.Now()); err != nil {
		t.Fatal(err)
	}
	call := func(h http.HandlerFunc, url string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, url, nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1))))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}
	// both switches off, then only the panel switch on: still off
	for _, setup := range []func(){func() {}, func() { _ = st.SetPreference(ctx, "video_links_panel", "on") }} {
		setup()
		if c, m := call(s.handleCommentarySummary, "/api/commentary/summary?set=watchlist"); c != 200 || m["enabled"] != false {
			t.Fatalf("chips off: %d %v", c, m)
		}
		if c, m := call(s.handleCommentaryByTicker, "/api/commentary?ticker=MU"); c != 200 || m["enabled"] != false {
			t.Fatalf("drawer off: %d %v", c, m)
		}
	}
	_ = st.SetPreference(ctx, "video_links_chips", "on")
	// summary: only the tickers of the set that have commentary
	c, m := call(s.handleCommentarySummary, "/api/commentary/summary?set=watchlist")
	tk, _ := m["tickers"].(map[string]any)
	if c != 200 || m["enabled"] != true || m["set"] != "watchlist" || len(tk) != 1 || tk["MU"] == nil {
		t.Fatalf("watchlist summary: %d %v", c, m)
	}
	if _, um := call(s.handleCommentarySummary, "/api/commentary/summary?set=universe"); um["tickers"].(map[string]any)["NVDA"] == nil {
		t.Fatalf("universe summary: %v", um)
	}
	if c, _ := call(s.handleCommentarySummary, "/api/commentary/summary?set=bogus"); c != http.StatusBadRequest {
		t.Fatalf("bad set: %d", c)
	}
	// the chip agrees with the drawer: the same "last" for each commentator
	_, dm := call(s.handleCommentaryByTicker, "/api/commentary?ticker=mu&days=90")
	com, _ := dm["commentary"].(map[string]any)
	if dm["enabled"] != true || com == nil || com["ticker"] != "MU" {
		t.Fatalf("drawer: %v", dm)
	}
	chipJ := tk["MU"].(map[string]any)["jordi"].(map[string]any)
	drawJ := com["last"].(map[string]any)["jordi"].(map[string]any)
	chipC := tk["MU"].(map[string]any)["cowen"].(map[string]any)
	drawC := com["last"].(map[string]any)["cowen"].(map[string]any)
	if chipJ["date"] != drawJ["date"] || chipJ["daysAgo"] != drawJ["daysAgo"] || chipC["date"] != drawC["date"] || chipC["daysAgo"] != drawC["daysAgo"] || chipJ["daysAgo"].(float64) != 3 || chipC["daysAgo"].(float64) != 12 {
		t.Fatalf("chip and drawer disagree: %v vs %v", tk["MU"], com["last"])
	}
	// an unknown ticker gets no commentary; a missing ticker is a bad request
	if _, um := call(s.handleCommentaryByTicker, "/api/commentary?ticker=ZZZZ"); um["commentary"] != nil || um["enabled"] != true {
		t.Fatalf("unknown ticker: %v", um)
	}
	if c, _ := call(s.handleCommentaryByTicker, "/api/commentary"); c != http.StatusBadRequest {
		t.Fatalf("missing ticker: %d", c)
	}
	// demo mode never shows chips
	_ = st.SetPreference(ctx, "demo_mode", "on")
	if _, dm := call(s.handleCommentarySummary, "/api/commentary/summary?set=watchlist"); dm["enabled"] != false || strings.Contains(toJSON(dm), "MU") {
		t.Fatalf("demo: %v", dm)
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestVideoLinksChipsPreferenceValidation(t *testing.T) {
	for v, ok := range map[string]bool{"on": true, "off": true, "yes": false, "": false} {
		if got := validPreferenceValue("video_links_chips", v); got != ok {
			t.Errorf("video_links_chips=%q: valid=%v, want %v", v, got, ok)
		}
	}
}
