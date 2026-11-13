package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func timeNow() time.Time { return time.Date(2026, 11, 13, 4, 30, 0, 0, time.UTC) }

// FT-107/108/109: with demo mode on, the real cash balance, deleted-holdings cost basis and a snapshot write never
// reach the response or the history; with demo mode off the real values are served as before.
func TestDemoLeaks_CashDeletedSnapshot(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	if err := st.SetPreference(ctx, "cash_balance_usd", "13868.9"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPreference(ctx, "cash_balance_eur", "13868.9"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `INSERT INTO stock_holdings (id, user_id, name, ticker, invested_usd, avg_open_price, current_price, deleted_at, updated_at)
		VALUES (7, 1, 'XOM', 'XOM', 100, 149.12, 160.59, 1, 0)`)
	call := func(h func(http.ResponseWriter, *http.Request), method, path string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil).WithContext(context.WithValue(context.Background(), ctxUserID, int64(1)))
		h(rec, req)
		return rec.Code, rec.Body.String()
	}

	// demo off: real values served
	if _, b := call(s.handleListPreferences, "GET", "/api/preferences"); !strings.Contains(b, "13868.9") {
		t.Fatalf("demo off must serve cash: %s", b)
	}
	if _, b := call(s.handleListDeletedStocks, "GET", "/api/holdings/stocks/deleted"); !strings.Contains(b, "149.12") {
		t.Fatalf("demo off must list the deleted holding: %s", b)
	}

	if err := st.SetPreference(ctx, "demo_mode", "on"); err != nil {
		t.Fatal(err)
	}
	if _, b := call(s.handleListPreferences, "GET", "/api/preferences"); strings.Contains(b, "13868") || strings.Contains(b, "cash_balance") {
		t.Fatalf("FT-107 preferences leak cash in demo: %s", b)
	}
	if _, b := call(s.handleGetPreference, "GET", "/api/preferences/cash_balance_usd"); strings.Contains(b, "13868") {
		t.Fatalf("FT-107 single preference leaks cash in demo: %s", b)
	}
	if _, b := call(s.handleSummary, "GET", "/api/summary"); strings.Contains(b, "13868") || strings.Contains(b, "13,868") {
		t.Fatalf("FT-107 summary leaks cash in demo: %s", b)
	}
	if _, b := call(s.handleListDeletedStocks, "GET", "/api/holdings/stocks/deleted"); strings.Contains(b, "149.12") || strings.Contains(b, "XOM") {
		t.Fatalf("FT-108 deleted holdings leak in demo: %s", b)
	}
	// FT-109: the route is wrapped; the nightly check skips and writes nothing.
	if c, _ := call(s.denyInDemo(s.handleRiskSnapshot), "POST", "/api/risk/snapshot"); c != 403 {
		t.Fatalf("FT-109 snapshot must be refused in demo, got %d", c)
	}
	if err := s.RunNightlyRiskCheck(ctx, timeNow(), nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.DB.QueryRow(`SELECT COUNT(*) FROM portfolio_value_history`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("nothing may be written to the value history in demo mode: %d %v", n, err)
	}
	if v, _ := st.GetPreference(ctx, "risk_circuit_breaker_active"); v == "true" {
		t.Fatal("the breaker must not arm on a masked book")
	}
}
