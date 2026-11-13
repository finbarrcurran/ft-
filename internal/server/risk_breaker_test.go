package server

import (
	"context"
	"testing"
	"time"
)

// FT-079: the nightly check reads the drawdown on the cash-inclusive basis, arms the ADVISORY breaker once with one
// Telegram alert, and does not alert again while it stays armed.
func TestRunNightlyRiskCheck_CashInclusiveAndOneAlert(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 11, 4, 30, 0, 0, time.UTC)

	// Cash keeps the cash-inclusive drawdown at -9.1% (peak 1100, now 1000): no trip, no alert.
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, cash_value_usd, computed_at) VALUES ('2026-11-01', 100, 100, 0, 1000, 0)`)
	if err := st.SetPreference(ctx, "cash_balance_usd", "1000"); err != nil {
		t.Fatal(err)
	}
	n := &captureNotifier{}
	if err := s.RunNightlyRiskCheck(ctx, now, n); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetPreference(ctx, "risk_circuit_breaker_active"); v == "true" || len(n.msgs) != 0 {
		t.Fatalf("must not trip at -9.1%% incl. cash: active=%q msgs=%v", v, n.msgs)
	}
	var cash float64
	// the day's row is written by the nightly value job; with none for today the cash update is a no-op

	// Now a real breach: the cash drops, so today's value incl. cash is 40% below the peak.
	if err := st.SetPreference(ctx, "cash_balance_usd", "600"); err != nil {
		t.Fatal(err)
	}
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES ('2026-11-11', 0, 0, 0, 0)`)
	if err := s.RunNightlyRiskCheck(ctx, now, n); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.GetPreference(ctx, "risk_circuit_breaker_active"); v != "true" {
		t.Fatalf("breaker must arm on a -45%% drawdown incl. cash, got %q", v)
	}
	if len(n.msgs) != 1 {
		t.Fatalf("exactly one alert on the trip, got %d: %v", len(n.msgs), n.msgs)
	}
	if err := st.DB.QueryRow(`SELECT cash_value_usd FROM portfolio_value_history WHERE date = '2026-11-11'`).Scan(&cash); err != nil || cash != 600 {
		t.Fatalf("cash recorded with the day's row: %v %v", cash, err)
	}
	// Still armed next night: no second alert.
	if err := s.RunNightlyRiskCheck(ctx, now.AddDate(0, 0, 1), n); err != nil {
		t.Fatal(err)
	}
	if len(n.msgs) != 1 {
		t.Fatalf("no repeat alert while armed, got %d", len(n.msgs))
	}
}
