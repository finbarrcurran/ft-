package server

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// FT-144: the "today" value in the drawdown is the live holdings value, never the last nightly snapshot, and the
// result says so and when the prices were last refreshed.
func TestRiskCheckTodayIsLiveHoldingsNotSnapshot(t *testing.T) {
	s, st := snapServer(t)
	ctx := context.Background()
	// the stored snapshot says 1,000 (a stale nightly row); the holdings are worth 4,000
	mustExec(t, st, `INSERT INTO portfolio_value_history (date, total_value_usd, stocks_value_usd, crypto_value_usd, computed_at) VALUES ('2026-11-13', 1000, 1000, 0, 0)`)
	mustExec(t, st, `INSERT INTO stock_holdings (id, user_id, name, ticker, invested_usd, avg_open_price, current_price, updated_at) VALUES (21, 1, 'Test Co', 'TST', 4000, 100, 100, 1)`)
	asOf := time.Date(2026, 11, 14, 8, 59, 0, 0, time.UTC)
	if err := st.SetMeta(ctx, "last_refreshed_at", strconv.FormatInt(asOf.Unix(), 10)); err != nil {
		t.Fatal(err)
	}
	chk, err := s.computeRiskCheck(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if chk.PortfolioValue != 4000 {
		t.Fatalf("today's value must be the live holdings value (4000), not the snapshot (1000): %v", chk.PortfolioValue)
	}
	if chk.ValueAsOf != "2026-11-14T08:59:00Z" || chk.ValueSource == "" {
		t.Fatalf("value source %q as of %q", chk.ValueSource, chk.ValueAsOf)
	}
	// the drawdown compares that live value with the history: peak 4000 (live) vs 1000 snapshot -> no drawdown
	if chk.DrawdownPct != 0 {
		t.Fatalf("live 4000 is the peak of [1000, 4000]: drawdown %v", chk.DrawdownPct)
	}
}
