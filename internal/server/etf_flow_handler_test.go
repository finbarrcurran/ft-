package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ft/internal/cryptoindicators"
)

// Regression (2026-10-13): the ETF chart's freshness fields were attached to the BTC-history
// handler by mistake, so the ETF chart never got its badge. The ETF endpoint must return the
// effective as-of date (a trailing zero placeholder ignored), the age and the level; the BTC
// endpoint must stay a plain history response.
func TestETFFlowHistoryEndpointCarriesFreshness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "etf-flow.json")
	// Newest row is a 0 placeholder dated today; the real data is 10 days old.
	old := time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")
	today := time.Now().UTC().Format("2006-01-02")
	body := `{"fetchedAt":"` + time.Now().UTC().Format(time.RFC3339) + `","rolling7dM":1,"rows":[{"date":"` + today + `","totalM":0},{"date":"` + old + `","totalM":-12.5}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := cryptoindicators.New(nil)
	svc.ETFFlowPath = path
	s := &Server{cryptoIndicators: svc}

	rec := httptest.NewRecorder()
	s.handleCryptoIndicatorsETFFlowHistory(rec, httptest.NewRequest(http.MethodGet, "/api/crypto-indicators/etf-flow/history?days=30", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["asOf"] != old {
		t.Fatalf("as-of must ignore the zero placeholder and be %s, got %v", old, out["asOf"])
	}
	if lvl, _ := out["staleLevel"].(string); lvl != "red" {
		t.Fatalf("10 calendar days old (>= 5 trading days) must be red, got %q (age %v)", lvl, out["ageTradingDays"])
	}
	if _, ok := out["history"]; !ok {
		t.Fatal("history must still be returned")
	}
}
